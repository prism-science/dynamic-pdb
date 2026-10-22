package validation

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

const reservedKeyPrefix = "__"

// ObjectKeys checks the keys of a decoded JSON value against the Go struct it maps
// to. A key the struct does not declare is unknown, and open data belongs under the
// struct's "extensions" map. Keys starting with "__" are reserved at every depth,
// including inside open objects.
func ObjectKeys(at Pointer, raw any, shape any) []FieldError {
	return objectKeys(at, raw, reflect.TypeOf(shape))
}

func objectKeys(at Pointer, raw any, shape reflect.Type) []FieldError {
	for shape != nil && shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	switch value := raw.(type) {
	case map[string]any:
		return objectKeysOfMap(at, value, shape)
	case []any:
		var elementShape reflect.Type
		if shape != nil && (shape.Kind() == reflect.Slice || shape.Kind() == reflect.Array) {
			elementShape = shape.Elem()
		}
		var fieldErrors []FieldError
		for index, element := range value {
			fieldErrors = append(fieldErrors, objectKeys(at.Index(index), element, elementShape)...)
		}
		return fieldErrors
	default:
		return nil
	}
}

func objectKeysOfMap(at Pointer, object map[string]any, shape reflect.Type) []FieldError {
	var declared map[string]reflect.Type
	var valueShape reflect.Type
	if shape != nil {
		switch shape.Kind() {
		case reflect.Struct:
			declared = jsonFields(shape)
		case reflect.Map:
			valueShape = shape.Elem()
		case reflect.Interface:
			// An open object: only the reserved-key rule applies below it.
		default:
			// A wrong JSON type for this field is reported where the field is decoded.
		}
	}

	var fieldErrors []FieldError
	for _, key := range slices.Sorted(maps.Keys(object)) {
		keyAt := at.Field(key)
		if strings.HasPrefix(key, reservedKeyPrefix) {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: keyAt,
				Code:    CodeReservedKey,
				Detail:  fmt.Sprintf("keys starting with %q are reserved", reservedKeyPrefix),
			})
			continue
		}
		childShape := valueShape
		if declared != nil {
			fieldShape, known := declared[key]
			if !known {
				fieldErrors = append(fieldErrors, FieldError{
					Pointer: keyAt,
					Code:    CodeUnknownField,
					Detail:  `is not a field of this object; put open data under "extensions"`,
				})
				continue
			}
			childShape = fieldShape
		}
		fieldErrors = append(fieldErrors, objectKeys(keyAt, object[key], childShape)...)
	}
	return fieldErrors
}

// jsonFields maps the JSON names a struct accepts to the types they decode into.
func jsonFields(structType reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, structType.NumField())
	for index := range structType.NumField() {
		field := structType.Field(index)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}
