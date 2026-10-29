// Package cif reads mmCIF and PDB structure files and derives the values the
// registry stores: counts, ligands, cofactors, mutations and connections.
package cif

import (
	"context"
	"strings"
)

// Document is the first data block of an mmCIF file.
type Document struct {
	scalarTags []string
	scalars    map[string]string
	tables     map[string]*Table
	// atomSite is set when the _atom_site rows were streamed, not stored.
	atomSite *atomSiteReader
	// metalLinks is set when the _struct_conn rows were streamed, not stored.
	metalLinks *metalLinkReader
	truncated  bool
	// timedOut is set when the time budget ended the read before the input did.
	timedOut bool
	notes    limitNotes
}

// Table is one category: a loop, or the items of a category written as pairs.
type Table struct {
	Columns []string
	Rows    [][]string
	// Complete is false when the read may have ended inside the category: a
	// truncated input or the time budget ended it while the loop, or the
	// category's pair items, were still being read, so more rows or items may
	// exist beyond.
	Complete bool
}

type token struct {
	text   string
	quoted bool
}

// Parse reads a complete mmCIF text.
func Parse(text string) *Document {
	return parse(context.Background(), text, false, parseOptions{})
}

// ParseTruncated reads the head of a larger file. The final token is dropped
// when it may be cut off, and loops still open at the end are marked incomplete.
func ParseTruncated(text string) *Document {
	return parse(context.Background(), text, true, parseOptions{})
}

// parseOptions lets extraction read a file in one pass without keeping every
// row. Parse and ParseTruncated keep all tables.
type parseOptions struct {
	// streamAtomSite feeds _atom_site rows to an accumulator instead of
	// storing them; the table keeps its columns and completeness only.
	streamAtomSite bool
	// streamStructConn feeds _struct_conn rows to a reader that keeps only the
	// metal coordination partners; Result.Connections stays empty.
	streamStructConn bool
	// onlyTables, when set, lists the categories whose rows are kept; the
	// rows of every other loop are skipped.
	onlyTables map[string]struct{}
}

func parse(ctx context.Context, text string, truncated bool, options parseOptions) *Document {
	reader := &tokenReader{tokens: newTokenizer(ctx, text, truncated)}
	document := &Document{
		scalars:   map[string]string{},
		tables:    map[string]*Table{},
		truncated: truncated,
	}
	scalarItems := 0
	pairColumns := map[string]*Table{}
	// openPairCategory is the category of the last pair item read, until a
	// loop starts: if the read ends there, more of its items may follow.
	openPairCategory := ""
	seenDataHeader := false
	for {
		current, ok := reader.next()
		if !ok {
			break
		}
		switch {
		case current.quoted:
		case isDataHeader(current.text):
			if seenDataHeader {
				return document.finish(pairColumns, reader, "")
			}
			seenDataHeader = true
		case strings.EqualFold(current.text, "loop_"):
			openPairCategory = ""
			document.readLoop(reader, truncated, options)
		case strings.HasPrefix(current.text, "_"):
			openPairCategory, _, _ = strings.Cut(strings.ToLower(current.text), ".")
			value, ok := reader.next()
			if !ok {
				continue
			}
			if scalarItems >= maxScalarItems {
				document.notes.add("more than %d non-looped items: the rest were ignored", maxScalarItems)
				continue
			}
			scalarItems++
			document.addScalar(current.text, value.text, pairColumns)
		}
	}
	return document.finish(pairColumns, reader, openPairCategory)
}

// tokenReader adds one token of pushback to the tokenizer.
type tokenReader struct {
	tokens  *tokenizer
	pending token
	hasHeld bool
}

func (r *tokenReader) next() (token, bool) {
	if r.hasHeld {
		r.hasHeld = false
		return r.pending, true
	}
	return r.tokens.next()
}

func (r *tokenReader) unread(held token) {
	r.pending = held
	r.hasHeld = true
}

// finish stores the pair-form categories. openCategory is the one whose items
// were being read when the input ended ("" when a data block header ended the
// read): it is incomplete when the read was cut off before a "#" line closed it,
// or when the time budget ended it.
func (d *Document) finish(pairColumns map[string]*Table, reader *tokenReader, openCategory string) *Document {
	if reader.tokens.check.err != nil {
		d.timedOut = true
		d.notes.add(timeBudgetNote)
	}
	cutOff := d.timedOut || (d.truncated && !reader.tokens.endsWithSeparator)
	for category, table := range pairColumns {
		if _, exists := d.tables[category]; !exists {
			table.Rows = [][]string{rowFromPairs(table)}
			table.Complete = !cutOff || category != openCategory
			d.tables[category] = table
		}
	}
	return d
}

func rowFromPairs(table *Table) []string {
	row := make([]string, len(table.Columns))
	copy(row, table.Rows[0])
	return row
}

func isDataHeader(text string) bool {
	return len(text) >= 5 && strings.EqualFold(text[:5], "data_")
}

func (d *Document) addScalar(tag string, value string, pairColumns map[string]*Table) {
	lowerTag := strings.ToLower(tag)
	if _, exists := d.scalars[lowerTag]; !exists {
		d.scalarTags = append(d.scalarTags, tag)
	}
	d.scalars[lowerTag] = value
	category, column, ok := strings.Cut(lowerTag, ".")
	if !ok {
		return
	}
	table := pairColumns[category]
	if table == nil {
		table = &Table{Complete: true, Rows: [][]string{{}}}
		pairColumns[category] = table
	}
	table.Columns = append(table.Columns, column)
	table.Rows[0] = append(table.Rows[0], value)
}

func (d *Document) readLoop(reader *tokenReader, truncated bool, options parseOptions) {
	var category string
	var columns []string
	columnCount := 0
	for {
		current, ok := reader.next()
		if !ok {
			break
		}
		if current.quoted || !strings.HasPrefix(current.text, "_") {
			reader.unread(current)
			break
		}
		columnCount++
		if columnCount > maxLoopColumns {
			continue
		}
		tag := strings.ToLower(current.text)
		tagCategory, column, ok := strings.Cut(tag, ".")
		if !ok {
			tagCategory, column = tag, ""
		}
		if category == "" {
			category = tagCategory
		}
		columns = append(columns, column)
	}
	if columnCount == 0 {
		return
	}
	tooWide := columnCount > maxLoopColumns
	if tooWide {
		d.notes.add("loop %s has more than %d columns: its rows were skipped", category, maxLoopColumns)
	}
	width := len(columns)
	_, exists := d.tables[category]
	var sink *atomSiteReader
	var linkSink *metalLinkReader
	keepRows := !exists && !tooWide
	if keepRows && options.onlyTables != nil {
		_, keepRows = options.onlyTables[category]
	}
	if !exists && !tooWide && options.streamAtomSite && category == "_atom_site" {
		sink = newAtomSiteReader(columns)
		d.atomSite = sink
		keepRows = false
	}
	if !exists && !tooWide && options.streamStructConn && category == "_struct_conn" {
		linkSink = newMetalLinkReader(columns)
		d.metalLinks = linkSink
		keepRows = false
	}
	cellLimit := min(maxTableCells, maxTableRows*max(width, 1))
	cellLimit -= cellLimit % max(width, 1)
	var values []string
	var row []string
	if sink != nil || linkSink != nil {
		row = make([]string, 0, width)
	}
	count := 0
	capped := false
	endedByTerminator := false
	for {
		current, ok := reader.next()
		if !ok {
			break
		}
		if isLoopTerminator(current) {
			reader.unread(current)
			endedByTerminator = true
			break
		}
		count++
		switch {
		case sink != nil:
			row = append(row, current.text)
			if len(row) == width {
				sink.add(row)
				row = row[:0]
			}
		case linkSink != nil:
			row = append(row, current.text)
			if len(row) == width {
				linkSink.add(row)
				row = row[:0]
			}
		case keepRows:
			if len(values) < cellLimit {
				values = append(values, current.text)
			} else if !capped {
				capped = true
				d.notes.add("table %s has more than %d rows or %d values: the rest were ignored", category, maxTableRows, maxTableCells)
			}
		}
	}
	if exists || tooWide || (!keepRows && sink == nil && linkSink == nil) {
		return
	}
	table := &Table{Columns: columns}
	for offset := 0; offset+width <= len(values); offset += width {
		table.Rows = append(table.Rows, values[offset:offset+width])
	}
	// The loop is open at the end when the input stopped before a "#" line
	// closed it, or the time budget stopped the read inside it (even at a row
	// boundary).
	openAtEnd := (truncated && !reader.tokens.endsWithSeparator) || reader.tokens.check.err != nil
	table.Complete = (!openAtEnd || endedByTerminator) && count%width == 0 && !capped
	d.tables[category] = table
}

func isLoopTerminator(candidate token) bool {
	if candidate.quoted {
		return false
	}
	return strings.HasPrefix(candidate.text, "_") ||
		isDataHeader(candidate.text) ||
		strings.EqualFold(candidate.text, "loop_") ||
		(len(candidate.text) >= 5 && strings.EqualFold(candidate.text[:5], "save_")) ||
		strings.EqualFold(candidate.text, "global_") ||
		strings.EqualFold(candidate.text, "stop_")
}

// Scalar returns the value of a non-looped item. Missing, "." and "?" report false.
func (d *Document) Scalar(tag string) (string, bool) {
	value, ok := d.scalars[strings.ToLower(tag)]
	if !ok {
		return "", false
	}
	value = cleanValue(value)
	return value, value != ""
}

// Scalars returns every non-looped item that has a value, keyed by the tag as written.
func (d *Document) Scalars() map[string]string {
	result := make(map[string]string, len(d.scalarTags))
	for _, tag := range d.scalarTags {
		if value := cleanValue(d.scalars[strings.ToLower(tag)]); value != "" {
			result[tag] = value
		}
	}
	return result
}

// Table returns a category by name, with or without the leading underscore.
func (d *Document) Table(category string) *Table {
	return d.tables[strings.ToLower(category)]
}

// Value returns a cleaned cell of a row by column name; "." and "?" become "".
func (t *Table) Value(row []string, column string) string {
	column = strings.ToLower(column)
	for index, candidate := range t.Columns {
		if candidate == column && index < len(row) {
			return cleanValue(row[index])
		}
	}
	return ""
}

func cleanValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "." || value == "?" {
		return ""
	}
	return value
}

// tokenizer splits text into tokens one at a time. endsWithSeparator reports
// that a line starting with "#" followed the last token: archive files put one
// after every category, so a loop that is followed by one has ended even if the
// text stops there. It is final once next has returned false.
type tokenizer struct {
	text              string
	position          int
	truncated         bool
	endsWithSeparator bool
	check             timeCheck
}

func newTokenizer(ctx context.Context, text string, truncated bool) *tokenizer {
	return &tokenizer{
		text:      strings.ReplaceAll(text, "\r\n", "\n"),
		truncated: truncated,
		check:     timeCheck{ctx: ctx},
	}
}

// next returns the next token. When the time budget runs out it behaves like the
// end of the input and check.err says why.
func (t *tokenizer) next() (token, bool) {
	text := t.text
	for t.position < len(text) {
		if t.check.expired() {
			t.position = len(text)
			return token{}, false
		}
		position := t.position
		character := text[position]
		switch {
		case character == ' ' || character == '\t' || character == '\n':
			t.position++
		case character == '#':
			if position == 0 || text[position-1] == '\n' {
				t.endsWithSeparator = true
			}
			end := strings.IndexByte(text[position:], '\n')
			if end < 0 {
				t.position = len(text)
				return token{}, false
			}
			t.position += end
		case character == ';' && (position == 0 || text[position-1] == '\n'):
			end := strings.Index(text[position+1:], "\n;")
			if end < 0 {
				t.position = len(text)
				if t.truncated {
					return token{}, false
				}
				t.endsWithSeparator = false
				return token{text: text[position+1:], quoted: true}, true
			}
			t.endsWithSeparator = false
			t.position = position + end + 3
			return token{text: text[position+1 : position+1+end], quoted: true}, true
		case character == '\'' || character == '"':
			valueEnd, next, closed := quotedExtent(text, position)
			if !closed && t.truncated && next >= len(text) {
				t.position = len(text)
				return token{}, false
			}
			t.endsWithSeparator = false
			t.position = next
			return token{text: text[position+1 : valueEnd], quoted: true}, true
		default:
			end := position
			for end < len(text) && text[end] != ' ' && text[end] != '\t' && text[end] != '\n' {
				end++
			}
			if t.truncated && end >= len(text) {
				t.position = len(text)
				return token{}, false
			}
			t.endsWithSeparator = false
			t.position = end
			return token{text: text[position:end]}, true
		}
	}
	return token{}, false
}

// quotedExtent finds a quoted value on one line. The closing quote is the first
// matching quote followed by whitespace or the end of the line; an unterminated
// value takes the rest of the line. The value is text[start+1:valueEnd]. It
// reads forward from the opening quote once and stops at the closing quote, so a
// token costs its own length and a line of many quoted tokens is read once.
func quotedExtent[T ~string | ~[]byte](text T, start int) (valueEnd int, next int, closed bool) {
	quote := text[start]
	for index := start + 1; index < len(text); index++ {
		switch text[index] {
		case '\n':
			return index, index, false
		case quote:
			if index+1 == len(text) || text[index+1] == ' ' || text[index+1] == '\t' || text[index+1] == '\n' {
				return index, index + 1, true
			}
		}
	}
	return len(text), len(text), false
}
