package mmseqs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const FormatOutput = "query,target,fident,qcov,tcov,evalue,bits,alnlen,qstart,qend,tstart,tend,qaln,taln"

type Hit struct {
	QuerySequenceID  uuid.UUID
	TargetSequenceID uuid.UUID
	Fident           float64
	Qcov             float64
	Tcov             float64
	Evalue           float64
	Bits             float64
	AlignmentLength  int
	Qstart           int
	Qend             int
	Tstart           int
	Tend             int
	Qaln             string
	Taln             string
}

func ParseOutput(path string) (hits []Hit, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mmseqs output file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close mmseqs output file: %w", closeErr)
		}
	}()

	hits = make([]Hit, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 32*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		hit, err := parseHit(line)
		if err != nil {
			return nil, fmt.Errorf("parse mmseqs output line %d: %w", lineNumber, err)
		}
		if hit.QuerySequenceID == hit.TargetSequenceID {
			continue
		}
		hits = append(hits, *hit)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan mmseqs output file: %w", err)
	}
	return hits, nil
}

func parseHit(line string) (*Hit, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 14 {
		return nil, fmt.Errorf("expected 14 fields, got %d", len(fields))
	}

	queryID, err := uuid.Parse(fields[0])
	if err != nil {
		return nil, fmt.Errorf("parse query sequence id: %w", err)
	}
	targetID, err := uuid.Parse(fields[1])
	if err != nil {
		return nil, fmt.Errorf("parse target sequence id: %w", err)
	}

	fident, err := parseFloatField(fields[2], "fident")
	if err != nil {
		return nil, err
	}
	qcov, err := parseFloatField(fields[3], "qcov")
	if err != nil {
		return nil, err
	}
	tcov, err := parseFloatField(fields[4], "tcov")
	if err != nil {
		return nil, err
	}
	evalue, err := parseFloatField(fields[5], "evalue")
	if err != nil {
		return nil, err
	}
	bits, err := parseFloatField(fields[6], "bits")
	if err != nil {
		return nil, err
	}
	alignmentLength, err := parseIntField(fields[7], "alnlen")
	if err != nil {
		return nil, err
	}
	qstart, err := parseIntField(fields[8], "qstart")
	if err != nil {
		return nil, err
	}
	qend, err := parseIntField(fields[9], "qend")
	if err != nil {
		return nil, err
	}
	tstart, err := parseIntField(fields[10], "tstart")
	if err != nil {
		return nil, err
	}
	tend, err := parseIntField(fields[11], "tend")
	if err != nil {
		return nil, err
	}

	return &Hit{
		QuerySequenceID:  queryID,
		TargetSequenceID: targetID,
		Fident:           fident,
		Qcov:             qcov,
		Tcov:             tcov,
		Evalue:           evalue,
		Bits:             bits,
		AlignmentLength:  alignmentLength,
		Qstart:           qstart,
		Qend:             qend,
		Tstart:           tstart,
		Tend:             tend,
		Qaln:             fields[12],
		Taln:             fields[13],
	}, nil
}

func parseFloatField(value string, name string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func parseIntField(value string, name string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}
