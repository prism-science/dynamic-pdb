package cif

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
)

// streamBufferBytes is the read buffer of a streamed structure file.
const streamBufferBytes = 64 * 1024

// lineReader reads a stream one line at a time through a fixed buffer, so its
// memory does not grow with the stream; only a line longer than the buffer is
// copied, up to maxScanLineBytes.
type lineReader struct {
	reader *bufio.Reader
	check  timeCheck
	// longLine holds a line that does not fit the read buffer.
	longLine []byte
	atEnd    bool
	// err is a read failure or a line over its limit.
	err error
}

func newLineReader(ctx context.Context, source io.Reader) *lineReader {
	return &lineReader{reader: bufio.NewReaderSize(source, streamBufferBytes), check: timeCheck{ctx: ctx}}
}

// next returns the next line without its "\n" (or "\r\n"), and whether the
// stream had a newline after it. The line is valid until the next call. It
// reports false at the end of the stream, after a read failure (err) and once
// the context is done (check.err).
func (r *lineReader) next() ([]byte, bool, bool) {
	if r.atEnd || r.err != nil || r.check.expired() {
		return nil, false, false
	}
	line, err := r.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		r.longLine = append(r.longLine[:0], line...)
		for errors.Is(err, bufio.ErrBufferFull) {
			if len(r.longLine) >= maxScanLineBytes {
				r.err = fmt.Errorf("line is longer than %d bytes", maxScanLineBytes)
				return nil, false, false
			}
			line, err = r.reader.ReadSlice('\n')
			r.longLine = append(r.longLine, line...)
		}
		line = r.longLine
	}
	switch {
	case err == nil:
	case errors.Is(err, io.EOF):
		r.atEnd = true
		if len(line) == 0 {
			return nil, false, false
		}
	default:
		r.err = fmt.Errorf("read structure file: %w", err)
		return nil, false, false
	}
	hadNewline := line[len(line)-1] == '\n'
	if hadNewline {
		line = line[:len(line)-1]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
	}
	return line, hadNewline, true
}

// stopErr returns what ended the read before the stream did: a read failure,
// a line over its limit, or the context.
func (r *lineReader) stopErr() error {
	if r.err != nil {
		return r.err
	}
	if r.check.err != nil {
		return fmt.Errorf("read stopped: %w", r.check.err)
	}
	return nil
}

// lineTokenizer yields the tokens of an mmCIF stream with the rules of
// tokenizer, one line at a time, so a file of any size is read in bounded
// memory. The stream is read to its end, so nothing is treated as cut off. A
// ;-delimited text field is the only token that spans lines; it is limited to
// maxScanLineBytes.
type lineTokenizer struct {
	lines *lineReader
	// queue holds the tokens of the current line not yet returned.
	queue             []token
	endsWithSeparator bool
}

func newLineTokenizer(ctx context.Context, source io.Reader) *lineTokenizer {
	return &lineTokenizer{lines: newLineReader(ctx, source)}
}

func (t *lineTokenizer) next() (token, bool) {
	for len(t.queue) == 0 {
		if !t.readLine() {
			return token{}, false
		}
	}
	current := t.queue[0]
	t.queue = t.queue[1:]
	t.endsWithSeparator = false
	return current, true
}

func (t *lineTokenizer) separatorFollows() bool {
	return t.endsWithSeparator
}

// stopErr reports the context error alone, as tokenizer does; err also
// reports a read failure.
func (t *lineTokenizer) stopErr() error {
	return t.lines.check.err
}

// err returns what ended the read before the stream did.
func (t *lineTokenizer) err() error {
	return t.lines.stopErr()
}

// readLine queues the tokens of the next line, or of a text field and the rest
// of the line that closes it. It reports false when no line is left.
func (t *lineTokenizer) readLine() bool {
	line, hadNewline, ok := t.lines.next()
	if !ok {
		return false
	}
	if len(line) == 0 || line[0] != ';' {
		t.scanLine(line, true)
		return true
	}
	field, rest, closed, ok := t.readTextField(line[1:], hadNewline)
	if !ok {
		return false
	}
	t.queue = append(t.queue, token{text: field, quoted: true})
	if closed {
		t.scanLine(rest, false)
	}
	return true
}

// readTextField reads a ;-delimited text field whose first line, after the ";",
// is first. The value runs to the line before the closing ";" line, and rest is
// what follows that ";". A field the stream ends inside takes the rest of the
// stream (closed is false), as tokenizer does for a complete text.
func (t *lineTokenizer) readTextField(first []byte, firstHadNewline bool) (string, []byte, bool, bool) {
	content := append([]byte(nil), first...)
	pendingNewline := firstHadNewline
	for {
		line, hadNewline, ok := t.lines.next()
		if !ok {
			if t.lines.stopErr() != nil {
				return "", nil, false, false
			}
			if pendingNewline {
				content = append(content, '\n')
			}
			return string(content), nil, false, true
		}
		if len(line) > 0 && line[0] == ';' {
			return string(content), line[1:], true, true
		}
		content = append(content, '\n')
		content = append(content, line...)
		pendingNewline = hadNewline
		if len(content) > maxScanLineBytes {
			t.lines.err = fmt.Errorf("text field is longer than %d bytes", maxScanLineBytes)
			return "", nil, false, false
		}
	}
}

// scanLine queues the tokens of one line outside a text field. A "#" starts a
// comment; at the start of a line it is also a category separator.
func (t *lineTokenizer) scanLine(line []byte, atLineStart bool) {
	for position := 0; position < len(line); {
		switch line[position] {
		case ' ', '\t':
			position++
		case '#':
			if position == 0 && atLineStart {
				t.endsWithSeparator = true
			}
			return
		case '\'', '"':
			valueEnd, next, _ := quotedExtent(line, position)
			t.queue = append(t.queue, token{text: string(line[position+1 : valueEnd]), quoted: true})
			position = next
		default:
			end := position
			for end < len(line) && line[end] != ' ' && line[end] != '\t' {
				end++
			}
			t.queue = append(t.queue, token{text: string(line[position:end])})
			position = end
		}
	}
}

// parseStream reads an mmCIF stream to its end (or to a second data block),
// as parse reads a complete text. It fails when the stream cannot be read, a
// line or text field is over its limit, or ctx is done first.
func parseStream(ctx context.Context, source io.Reader, options parseOptions) (*Document, error) {
	tokens := newLineTokenizer(ctx, source)
	document := parseTokens(tokens, false, options)
	if err := tokens.err(); err != nil {
		return nil, err
	}
	return document, nil
}
