// Package archive maps RCSB responses to Dynamic PDB values. The mapping is pure:
// it returns the proposed values together with where each came from, plus the
// conflicts and warnings found while choosing them. Callers decide what to store.
package archive

import (
	"fmt"
	"strings"
	"time"
)

// Annotations explains a mapping. Provenance maps an output field name to the RCSB
// path it was read from, Conflicts lists fields where RCSB reported several
// disagreeing values, and Warnings lists values that were dropped or kept as-is.
type Annotations struct {
	Provenance map[string]string
	Conflicts  []Conflict
	Warnings   []Warning
}

// Conflict records that RCSB reported several values for Field. Chosen is the
// value the mapping kept and Others are the values it ignored.
type Conflict struct {
	Field  string
	Chosen string
	Others []string
}

type Warning struct {
	Field  string
	Detail string
}

func newAnnotations() Annotations {
	return Annotations{Provenance: make(map[string]string)}
}

func (annotations *Annotations) from(field string, path string) {
	annotations.Provenance[field] = path
}

func (annotations *Annotations) warn(field string, format string, arguments ...any) {
	annotations.Warnings = append(annotations.Warnings, Warning{
		Field:  field,
		Detail: fmt.Sprintf(format, arguments...),
	})
}

// conflictIfDifferent records a conflict when any of the values differ from the
// first one. Values are compared by their text form.
func (annotations *Annotations) conflictIfDifferent(field string, values []string) {
	if len(values) < 2 {
		return
	}
	var others []string
	for _, value := range values[1:] {
		if value == values[0] || containsString(others, value) {
			continue
		}
		others = append(others, value)
	}
	if len(others) > 0 {
		annotations.Conflicts = append(annotations.Conflicts, Conflict{
			Field:  field,
			Chosen: values[0],
			Others: others,
		})
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalCIFString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "?" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

// parseRCSBTime parses the RFC 3339 timestamps RCSB uses, for example
// 2026-08-12T00:00:00.000+00:00.
func parseRCSBTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse RCSB timestamp %q: %w", value, err)
	}
	return parsed, nil
}
