// Package validation checks writes against Dynamic PDB Schema v0.2.0
// (docs/schema/dynamic-pdb-schema-v0.2.0.md). Every rule reports all of its field
// errors at once, so HTTP handlers and the RCSB sync jobs share one set of rules.
package validation

import (
	"fmt"
	"strconv"
	"strings"
)

// Pointer is an RFC 6901 JSON pointer to the field an error is about.
type Pointer string

// Root points at the whole document.
const Root Pointer = ""

var pointerTokenEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func (pointer Pointer) Field(name string) Pointer {
	return pointer + "/" + Pointer(pointerTokenEscaper.Replace(name))
}

func (pointer Pointer) Index(index int) Pointer {
	return pointer + "/" + Pointer(strconv.Itoa(index))
}

type Code string

const (
	CodeRequired      Code = "REQUIRED"
	CodeInvalidValue  Code = "INVALID_VALUE"
	CodeInvalidFormat Code = "INVALID_FORMAT"
	CodeOutOfRange    Code = "OUT_OF_RANGE"
	CodeDuplicate     Code = "DUPLICATE"
	CodeUnknownField  Code = "UNKNOWN_FIELD"
	CodeReservedKey   Code = "RESERVED_KEY"
)

type FieldError struct {
	Pointer Pointer
	Code    Code
	Detail  string
}

func (fieldError FieldError) String() string {
	return fmt.Sprintf("%s %s (%s)", fieldError.Pointer, fieldError.Code, fieldError.Detail)
}

// Error is returned when a write breaks schema rules in enforce mode.
type Error struct {
	Fields []FieldError
}

func (validationError *Error) Error() string {
	return "schema validation failed: " + describe(validationError.Fields)
}

func describe(fieldErrors []FieldError) string {
	descriptions := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		descriptions = append(descriptions, fieldError.String())
	}
	return strings.Join(descriptions, "; ")
}
