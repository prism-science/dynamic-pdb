// Package cif reads mmCIF and PDB structure files and derives the values the
// registry stores: counts, ligands, cofactors, mutations and connections.
package cif

import (
	"strings"
)

// Document is the first data block of an mmCIF file.
type Document struct {
	scalarTags []string
	scalars    map[string]string
	tables     map[string]*Table
}

// Table is one category: a loop, or the items of a category written as pairs.
type Table struct {
	Columns []string
	Rows    [][]string
	// Complete is false when the input ended while the loop was still open,
	// which means more rows may exist beyond a truncated read.
	Complete bool
}

type token struct {
	text   string
	quoted bool
}

// Parse reads a complete mmCIF text.
func Parse(text string) *Document {
	return parse(text, false)
}

// ParseTruncated reads the head of a larger file. The final token is dropped
// when it may be cut off, and loops still open at the end are marked incomplete.
func ParseTruncated(text string) *Document {
	return parse(text, true)
}

func parse(text string, truncated bool) *Document {
	tokens, endsWithSeparator := tokenize(text, truncated)
	document := &Document{
		scalars: map[string]string{},
		tables:  map[string]*Table{},
	}
	pairColumns := map[string]*Table{}
	seenDataHeader := false
	for index := 0; index < len(tokens); {
		current := tokens[index]
		switch {
		case current.quoted:
			index++
		case isDataHeader(current.text):
			if seenDataHeader {
				return document.finish(pairColumns)
			}
			seenDataHeader = true
			index++
		case strings.EqualFold(current.text, "loop_"):
			index = document.readLoop(tokens, index+1, truncated && !endsWithSeparator)
		case strings.HasPrefix(current.text, "_"):
			if index+1 >= len(tokens) {
				index++
				continue
			}
			value := tokens[index+1].text
			document.addScalar(current.text, value, pairColumns)
			index += 2
		default:
			index++
		}
	}
	return document.finish(pairColumns)
}

func (d *Document) finish(pairColumns map[string]*Table) *Document {
	for category, table := range pairColumns {
		if _, exists := d.tables[category]; !exists {
			table.Rows = [][]string{rowFromPairs(table)}
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

func (d *Document) readLoop(tokens []token, start int, openAtEnd bool) int {
	index := start
	var category string
	var columns []string
	for index < len(tokens) && !tokens[index].quoted && strings.HasPrefix(tokens[index].text, "_") {
		tag := strings.ToLower(tokens[index].text)
		tagCategory, column, ok := strings.Cut(tag, ".")
		if !ok {
			tagCategory, column = tag, ""
		}
		if category == "" {
			category = tagCategory
		}
		columns = append(columns, column)
		index++
	}
	if len(columns) == 0 {
		return index
	}
	width := len(columns)
	table := &Table{Columns: columns}
	var values []string
	for index < len(tokens) && !isLoopTerminator(tokens[index]) {
		values = append(values, tokens[index].text)
		index++
	}
	for offset := 0; offset+width <= len(values); offset += width {
		table.Rows = append(table.Rows, values[offset:offset+width])
	}
	table.Complete = (!openAtEnd || index < len(tokens)) && len(values)%width == 0
	if _, exists := d.tables[category]; !exists {
		d.tables[category] = table
	}
	return index
}

func isLoopTerminator(candidate token) bool {
	if candidate.quoted {
		return false
	}
	return strings.HasPrefix(candidate.text, "_") ||
		isDataHeader(candidate.text) ||
		strings.EqualFold(candidate.text, "loop_") ||
		strings.HasPrefix(strings.ToLower(candidate.text), "save_") ||
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

// tokenize splits text into tokens. endsWithSeparator reports that a line
// starting with "#" followed the last token: archive files put one after every
// category, so a loop that is followed by one has ended even if the text stops there.
func tokenize(text string, truncated bool) (tokens []token, endsWithSeparator bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	position := 0
	for position < len(text) {
		character := text[position]
		switch {
		case character == ' ' || character == '\t' || character == '\n':
			position++
		case character == '#':
			if position == 0 || text[position-1] == '\n' {
				endsWithSeparator = true
			}
			end := strings.IndexByte(text[position:], '\n')
			if end < 0 {
				return tokens, endsWithSeparator
			}
			position += end
		case character == ';' && (position == 0 || text[position-1] == '\n'):
			end := strings.Index(text[position+1:], "\n;")
			if end < 0 {
				if truncated {
					return tokens, endsWithSeparator
				}
				tokens = append(tokens, token{text: text[position+1:], quoted: true})
				endsWithSeparator = false
				return tokens, endsWithSeparator
			}
			tokens = append(tokens, token{text: text[position+1 : position+1+end], quoted: true})
			endsWithSeparator = false
			position += end + 3
		case character == '\'' || character == '"':
			value, next, closed := readQuoted(text, position)
			if !closed && truncated && next >= len(text) {
				return tokens, endsWithSeparator
			}
			tokens = append(tokens, token{text: value, quoted: true})
			endsWithSeparator = false
			position = next
		default:
			end := position
			for end < len(text) && text[end] != ' ' && text[end] != '\t' && text[end] != '\n' {
				end++
			}
			if truncated && end >= len(text) {
				return tokens, endsWithSeparator
			}
			tokens = append(tokens, token{text: text[position:end]})
			endsWithSeparator = false
			position = end
		}
	}
	return tokens, endsWithSeparator
}

// readQuoted reads a quoted value on one line. The closing quote is the first
// matching quote followed by whitespace or the end of the line; an unterminated
// value takes the rest of the line.
func readQuoted(text string, start int) (value string, next int, closed bool) {
	quote := text[start]
	lineEnd := strings.IndexByte(text[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text)
	} else {
		lineEnd += start
	}
	for index := start + 1; index < lineEnd; index++ {
		if text[index] != quote {
			continue
		}
		if index+1 == lineEnd || text[index+1] == ' ' || text[index+1] == '\t' {
			return text[start+1 : index], index + 1, true
		}
	}
	return text[start+1 : lineEnd], lineEnd, false
}
