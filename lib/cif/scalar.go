package cif

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ScalarValue returns one non-looped mmCIF item without building a document.
// It gives the same answer as Parse(text).Scalar(tag).
func ScalarValue(text string, tag string) (string, bool) {
	value, ok, err := ScalarValueFromReader(strings.NewReader(text), tag)
	if err != nil {
		return "", false
	}
	return value, ok
}

// ScalarValueFromReader scans an mmCIF stream for one non-looped item. Memory
// use does not depend on the size of the stream: lines are read through a fixed
// buffer, loops are skipped without storing their rows, and only the wanted
// value is kept. Missing, "." and "?" report false.
func ScalarValueFromReader(source io.Reader, tag string) (string, bool, error) {
	scanner := &scalarScanner{
		reader: bufio.NewReaderSize(source, 64*1024),
		wanted: []byte(tag),
	}
	if err := scanner.run(); err != nil {
		return "", false, fmt.Errorf("scan mmCIF for %s: %w", tag, err)
	}
	value := cleanValue(scanner.value)
	return value, value != "", nil
}

type scalarMode int

const (
	scalarModeNormal scalarMode = iota
	scalarModeLoopHeader
	scalarModeLoopBody
)

type scalarScanner struct {
	reader *bufio.Reader
	wanted []byte
	// longLine holds lines that do not fit the read buffer.
	longLine []byte

	mode        scalarMode
	loopColumns int
	seenData    bool
	finished    bool
	// awaitingValue is set after a tag outside a loop; the next token is its value.
	awaitingValue  bool
	awaitingWanted bool
	value          string
}

func (s *scalarScanner) run() error {
	inTextBlock := false
	var block []byte
	capture := false
	for !s.finished {
		line, err := s.readLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read line: %w", err)
		}
		atEnd := errors.Is(err, io.EOF)
		hadNewline := len(line) > 0 && line[len(line)-1] == '\n'
		if hadNewline {
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		switch {
		case inTextBlock && len(line) > 0 && line[0] == ';':
			inTextBlock = false
			s.handle(block, true)
			block = block[:0]
			s.scanLine(line[1:])
		case inTextBlock:
			if capture {
				block = append(block, '\n')
				block = append(block, line...)
			}
		case len(line) > 0 && line[0] == ';':
			inTextBlock = true
			capture = s.awaitingValue && s.awaitingWanted
			block = block[:0]
			if capture {
				block = append(block, line[1:]...)
			}
		default:
			s.scanLine(line)
		}
		if atEnd {
			if inTextBlock && !s.finished {
				s.handle(block, true)
			}
			return nil
		}
	}
	return nil
}

// readLine returns the next line including its newline; io.EOF comes with the
// last, possibly empty, line.
func (s *scalarScanner) readLine() ([]byte, error) {
	line, err := s.reader.ReadSlice('\n')
	if !errors.Is(err, bufio.ErrBufferFull) {
		return line, err
	}
	s.longLine = append(s.longLine[:0], line...)
	for errors.Is(err, bufio.ErrBufferFull) {
		if len(s.longLine) >= maxScanLineBytes {
			return nil, fmt.Errorf("line is longer than %d bytes", maxScanLineBytes)
		}
		line, err = s.reader.ReadSlice('\n')
		s.longLine = append(s.longLine, line...)
	}
	return s.longLine, err
}

// scanLine splits one line (without its newline, outside a text block) into
// tokens the way the document tokenizer does and feeds them to the state machine.
func (s *scalarScanner) scanLine(line []byte) {
	position := 0
	for position < len(line) && !s.finished {
		character := line[position]
		switch character {
		case ' ', '\t':
			position++
		case '#':
			return
		case '\'', '"':
			valueEnd, next, _ := quotedExtent(line, position)
			s.handle(line[position+1:valueEnd], true)
			position = next
		default:
			end := position
			for end < len(line) && line[end] != ' ' && line[end] != '\t' {
				end++
			}
			s.handle(line[position:end], false)
			position = end
		}
	}
}

func (s *scalarScanner) handle(text []byte, quoted bool) {
	if s.awaitingValue {
		s.awaitingValue = false
		if s.awaitingWanted {
			s.value = string(text)
		}
		return
	}
	for {
		switch s.mode {
		case scalarModeLoopHeader:
			if !quoted && len(text) > 0 && text[0] == '_' {
				s.loopColumns++
				return
			}
			if s.loopColumns == 0 {
				s.mode = scalarModeNormal
			} else {
				s.mode = scalarModeLoopBody
			}
		case scalarModeLoopBody:
			if !isLoopTerminatorBytes(text, quoted) {
				return
			}
			s.mode = scalarModeNormal
		case scalarModeNormal:
			s.handleNormal(text, quoted)
			return
		default:
			panic(fmt.Sprintf("unknown scalar scanner mode %d", s.mode))
		}
	}
}

func (s *scalarScanner) handleNormal(text []byte, quoted bool) {
	switch {
	case quoted:
	case isDataHeaderBytes(text):
		if s.seenData {
			s.finished = true
			return
		}
		s.seenData = true
	case bytes.EqualFold(text, []byte("loop_")):
		s.mode = scalarModeLoopHeader
		s.loopColumns = 0
	case len(text) > 0 && text[0] == '_':
		s.awaitingValue = true
		s.awaitingWanted = bytes.EqualFold(text, s.wanted)
	}
}

func isDataHeaderBytes(text []byte) bool {
	return len(text) >= 5 && bytes.EqualFold(text[:5], []byte("data_"))
}

func hasFoldPrefix(text []byte, prefix string) bool {
	return len(text) >= len(prefix) && bytes.EqualFold(text[:len(prefix)], []byte(prefix))
}

func isLoopTerminatorBytes(text []byte, quoted bool) bool {
	if quoted {
		return false
	}
	return (len(text) > 0 && text[0] == '_') ||
		isDataHeaderBytes(text) ||
		bytes.EqualFold(text, []byte("loop_")) ||
		hasFoldPrefix(text, "save_") ||
		bytes.EqualFold(text, []byte("global_")) ||
		bytes.EqualFold(text, []byte("stop_"))
}
