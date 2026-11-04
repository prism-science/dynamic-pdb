package validation

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

const reservedKeyPrefix = "__"

// extensionsField is the open map in which an object with one takes data outside its fields.
const extensionsField = "extensions"

// ObjectKeys checks the keys of a decoded JSON value against the Go struct it maps
// to. A key the struct does not declare is unknown; when the struct has an
// "extensions" map, open data belongs there. Keys starting with "__" are reserved at
// every depth, including inside open objects.
func ObjectKeys(at Pointer, raw any, shape any) []FieldError {
	return keyWalk{}.keys(at, raw, reflect.TypeOf(shape))
}

// RequestKeys checks the keys of a decoded request body against the request type it
// decodes into, with the rules of ObjectKeys. It does not enter a declared field whose
// JSON name is in fieldsWithOwnRule: a rule of its own checks that field's keys, so
// every key gets one error.
func RequestKeys(at Pointer, raw any, shape any, fieldsWithOwnRule ...string) []FieldError {
	return keyWalk{fieldsWithOwnRule: fieldsWithOwnRule}.keys(at, raw, reflect.TypeOf(shape))
}

type keyWalk struct {
	fieldsWithOwnRule []string
}

func (walk keyWalk) keys(at Pointer, raw any, shape reflect.Type) []FieldError {
	for shape != nil && shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	switch value := raw.(type) {
	case map[string]any:
		return walk.keysOfMap(at, value, shape)
	case []any:
		var elementShape reflect.Type
		if shape != nil && (shape.Kind() == reflect.Slice || shape.Kind() == reflect.Array) {
			elementShape = shape.Elem()
		}
		var fieldErrors []FieldError
		for index, element := range value {
			fieldErrors = append(fieldErrors, walk.keys(at.Index(index), element, elementShape)...)
		}
		return fieldErrors
	default:
		return nil
	}
}

func (walk keyWalk) keysOfMap(at Pointer, object map[string]any, shape reflect.Type) []FieldError {
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
				fieldErrors = append(fieldErrors, unknownField(keyAt, declared))
				continue
			}
			if slices.Contains(walk.fieldsWithOwnRule, key) {
				continue
			}
			childShape = fieldShape
		}
		fieldErrors = append(fieldErrors, walk.keys(keyAt, object[key], childShape)...)
	}
	return fieldErrors
}

// unknownField reports a key the object does not declare. The error points to
// "extensions" only when the object has that open map.
func unknownField(at Pointer, declared map[string]reflect.Type) FieldError {
	detail := "is not a field of this object"
	if _, hasExtensions := declared[extensionsField]; hasExtensions {
		detail += fmt.Sprintf("; put open data under %q", extensionsField)
	}
	return FieldError{Pointer: at, Code: CodeUnknownField, Detail: detail}
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
