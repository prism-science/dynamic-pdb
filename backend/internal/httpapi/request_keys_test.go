package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/validation"
)

// keyError is the pointer and code of one request key error, which is all these tests compare.
type keyError struct {
	Pointer validation.Pointer
	Code    validation.Code
}

func keyErrorsOf(fieldErrors []validation.FieldError) []keyError {
	result := make([]keyError, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		result = append(result, keyError{Pointer: fieldError.Pointer, Code: fieldError.Code})
	}
	return result
}

func rawJSON(t *testing.T, body string) any {
	t.Helper()
	var raw any
	require.NoError(t, json.Unmarshal([]byte(body), &raw))
	return raw
}

const validCreateEntryBody = `{
	"entry": {
		"title": "Lysozyme",
		"thumbnail_image_url": null,
		"method": "X-ray crystallography",
		"details": "room temperature",
		"resolution": 1.5,
		"space_group": "P 43 21 2",
		"external_refs": {"pdb": "1ABC"},
		"crystallography": {"crystals": [{
			"id": "1",
			"growth": {"ph": 7.0, "temperature_kelvin": 293},
			"diffractions": [{"id": "1", "temperature_kelvin": 100}]
		}]},
		"ligands": ["GOL"],
		"cofactors": [],
		"related_datasets": [{"doi": "10.1000/x", "data_type": "diffraction images", "repository": "SBGrid", "details": null}],
		"polymer_entities": [{
			"label_entity_id": "1",
			"construct": "full length",
			"mutations": ["C54T"],
			"source_organisms": [{"scientific_name": "Gallus gallus", "ncbi_taxonomy_id": 9031}]
		}],
		"artifacts": [{
			"id": "00000000-0000-0000-0000-000000000001",
			"name": "model.cif", "level": "L0", "type": "model",
			"uri": "https://example.org/model.cif", "sha256": null, "format": "mmcif", "size_bytes": 10,
			"metadata": {"extensions": {"note": "kept"}}
		}]
	},
	"model_operations": [{
		"op": "add",
		"data": {
			"title": "Model 1",
			"thumbnail_image_url": null,
			"idempotency_key": "key-1",
			"primary_artifact_id": "00000000-0000-0000-0000-000000000002",
			"metadata": {"model_type": "Deposited"},
			"artifacts": [{"id": "00000000-0000-0000-0000-000000000002", "name": "m.cif", "level": "L1", "type": "model"}],
			"runs": [{
				"id": "00000000-0000-0000-0000-000000000003",
				"name": "refine", "software_name": "phenix", "software_version": "1.21",
				"command": "phenix.refine", "parameters": {"cycles": 5},
				"started_at": "2026-11-04T10:00:00Z", "finished_at": "2026-11-04T11:00:00Z",
				"metadata": {"extensions": {}},
				"artifacts": [{"artifact_id": "00000000-0000-0000-0000-000000000002", "direction": "output", "position": 0}]
			}],
			"metrics": [{
				"id": "00000000-0000-0000-0000-000000000004", "key": "r_work", "value": 0.2,
				"run_id": "00000000-0000-0000-0000-000000000003", "software_name": "phenix", "software_version": "1.21"
			}]
		}
	}]
}`

func Test_should_report_no_key_errors_when_create_entry_request_uses_only_defined_keys(t *testing.T) {
	// given
	raw := rawJSON(t, validCreateEntryBody)

	// when
	fieldErrors := requestKeyErrors(raw, CreateEntryRequest{})

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_unknown_and_reserved_keys_at_every_depth_when_request_has_them(t *testing.T) {
	cases := []struct {
		name    string
		request any
		body    string
		want    []keyError
	}{
		{
			name:    "create entry",
			request: CreateEntryRequest{},
			body: `{
				"note": 1,
				"entry": {
					"title": "t", "method": "X-ray crystallography",
					"colour": "blue",
					"metadata": {},
					"crystallography": {"crystals": [{"id": "1", "growth": {"ph": 7, "ph_note": "x"}}]},
					"polymer_entities": [{"label_entity_id": "1", "source_organisms": [{"scientific_name": "x", "strain": "K12"}]}],
					"related_datasets": [{"doi": "10.1/x", "data_type": "t", "__hidden": true}],
					"artifacts": [{"id": "00000000-0000-0000-0000-000000000001", "name": "a", "level": "L0", "checksum": {}}]
				},
				"model_operations": [{
					"op": "add", "reason": "x",
					"data": {
						"title": "m",
						"runs": [{"id": "00000000-0000-0000-0000-000000000003", "name": "r",
							"parameters": {"cycles": 5, "__seed": 1},
							"artifacts": [{"artifact_id": "00000000-0000-0000-0000-000000000002", "direction": "input", "role": "x"}]}],
						"metrics": [{"id": "00000000-0000-0000-0000-000000000004", "key": "r_work", "value": 0.2, "unit": "%"}]
					}
				}]
			}`,
			want: []keyError{
				{"/entry/artifacts/0/checksum", validation.CodeUnknownField},
				{"/entry/colour", validation.CodeUnknownField},
				{"/entry/crystallography/crystals/0/growth/ph_note", validation.CodeUnknownField},
				{"/entry/metadata", validation.CodeUnknownField},
				{"/entry/polymer_entities/0/source_organisms/0/strain", validation.CodeUnknownField},
				{"/entry/related_datasets/0/__hidden", validation.CodeReservedKey},
				{"/model_operations/0/data/metrics/0/unit", validation.CodeUnknownField},
				{"/model_operations/0/data/runs/0/artifacts/0/role", validation.CodeUnknownField},
				{"/model_operations/0/data/runs/0/parameters/__seed", validation.CodeReservedKey},
				{"/model_operations/0/reason", validation.CodeUnknownField},
				{"/note", validation.CodeUnknownField},
			},
		},
		{
			name:    "entry revision",
			request: CreateEntryRevisionRequest{},
			body: `{
				"__trace": 1,
				"entry": {
					"colour": "blue",
					"crystallography": {"crystals": [{"id": "1", "diffractions": [{"id": "1", "beamline": "x"}]}]},
					"polymer_entities": [{"label_entity_id": "1", "tag": "x"}]
				}
			}`,
			want: []keyError{
				{"/__trace", validation.CodeReservedKey},
				{"/entry/colour", validation.CodeUnknownField},
				{"/entry/crystallography/crystals/0/diffractions/0/beamline", validation.CodeUnknownField},
				{"/entry/polymer_entities/0/tag", validation.CodeUnknownField},
			},
		},
		{
			name:    "create model",
			request: CreateModelRequest{},
			body: `{
				"model": {
					"title": "m", "__draft": true,
					"artifacts": [{"id": "00000000-0000-0000-0000-000000000001", "name": "a", "level": "L0", "stored_uri": "x"}],
					"runs": [{"id": "00000000-0000-0000-0000-000000000003", "name": "r", "host": "x"}],
					"metrics": [{"id": "00000000-0000-0000-0000-000000000004", "key": "r_work", "value": 0.2, "created_at": "x"}]
				}
			}`,
			want: []keyError{
				{"/model/__draft", validation.CodeReservedKey},
				{"/model/artifacts/0/stored_uri", validation.CodeUnknownField},
				{"/model/metrics/0/created_at", validation.CodeUnknownField},
				{"/model/runs/0/host", validation.CodeUnknownField},
			},
		},
		{
			name:    "model revision",
			request: CreateModelRevisionRequest{},
			body: `{
				"model": {
					"colour": "blue",
					"runs": [{"id": "00000000-0000-0000-0000-000000000003", "name": "r",
						"artifacts": [{"artifact_id": "00000000-0000-0000-0000-000000000002", "direction": "input", "__x": 1}]}]
				},
				"dry_run": true
			}`,
			want: []keyError{
				{"/dry_run", validation.CodeUnknownField},
				{"/model/colour", validation.CodeUnknownField},
				{"/model/runs/0/artifacts/0/__x", validation.CodeReservedKey},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given
			raw := rawJSON(t, testCase.body)

			// when
			fieldErrors := requestKeyErrors(raw, testCase.request)

			// then
			assert.Equal(t, testCase.want, keyErrorsOf(fieldErrors))
		})
	}
}

func Test_should_not_point_to_extensions_when_request_object_has_none(t *testing.T) {
	// given
	raw := rawJSON(t, `{"entry": {"Title": "wrong case", "title": "t", "method": "X-ray crystallography"}}`)

	// when
	fieldErrors := requestKeyErrors(raw, CreateEntryRequest{})

	// then
	assert.Equal(t, []validation.FieldError{{
		Pointer: "/entry/Title",
		Code:    validation.CodeUnknownField,
		Detail:  "is not a field of this object",
	}}, fieldErrors, "JSON keys are case-sensitive, although encoding/json matches them case-insensitively")
}

func Test_should_report_each_key_once_when_field_has_its_own_key_rule(t *testing.T) {
	// given
	body := `{
		"entry": {
			"title": "t", "method": "X-ray crystallography",
			"external_refs": {"__pdb": "1ABC"},
			"artifacts": [{"id": "00000000-0000-0000-0000-000000000001", "name": "a", "level": "L0", "type": "model",
				"metadata": {"__a": 1, "colour": "blue"}}]
		},
		"model_operations": [{"op": "add", "data": {
			"title": "m",
			"metadata": {"__m": 1},
			"runs": [{"id": "00000000-0000-0000-0000-000000000003", "name": "r", "metadata": {"__r": 1}}]
		}}]
	}`
	var request CreateEntryRequest
	require.NoError(t, json.Unmarshal([]byte(body), &request))

	// when
	keyErrors := requestKeyErrors(rawJSON(t, body), CreateEntryRequest{})
	schemaErrors := createEntryRequestErrors(request, nil)

	// then
	assert.Empty(t, keyErrors, "metadata and external_refs keys are left to their own rules")
	allPointers := pointersOf(append(keyErrors, schemaErrors...))
	for _, pointer := range []validation.Pointer{
		"/entry/external_refs/__pdb",
		"/entry/artifacts/0/metadata/__a",
		"/entry/artifacts/0/metadata/colour",
		"/model_operations/0/data/metadata/__m",
		"/model_operations/0/data/runs/0/metadata/__r",
	} {
		count := 0
		for _, reported := range allPointers {
			if reported == pointer {
				count++
			}
		}
		assert.Equal(t, 1, count, "errors at %s", pointer)
	}
}

// Test_should_walk_only_plain_json_fields_when_request_types_are_generated guards
// requestKeyErrors against a change in the generated types: the walk knows only plain
// json-tagged struct fields, so an embedded struct, a oneOf union or a type with its own
// UnmarshalJSON would make it report keys the type does accept.
func Test_should_walk_only_plain_json_fields_when_request_types_are_generated(t *testing.T) {
	// given
	jsonUnmarshaler := reflect.TypeFor[json.Unmarshaler]()
	timeType := reflect.TypeFor[time.Time]()
	seen := map[reflect.Type]bool{}
	var problems []string
	var visit func(path string, shape reflect.Type)
	visit = func(path string, shape reflect.Type) {
		for shape.Kind() == reflect.Pointer || shape.Kind() == reflect.Slice ||
			shape.Kind() == reflect.Array || shape.Kind() == reflect.Map {
			shape = shape.Elem()
		}
		if shape.Kind() != reflect.Struct || seen[shape] || shape == timeType {
			return
		}
		seen[shape] = true
		if reflect.PointerTo(shape).Implements(jsonUnmarshaler) {
			problems = append(problems, path+": "+shape.Name()+" has its own UnmarshalJSON")
		}
		for index := range shape.NumField() {
			field := shape.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			switch {
			case field.Anonymous:
				problems = append(problems, path+": "+shape.Name()+" embeds "+field.Name)
			case !field.IsExported():
				problems = append(problems, path+": "+shape.Name()+" has unexported field "+field.Name)
			case name == "" || name == "-":
				problems = append(problems, path+": "+shape.Name()+"."+field.Name+" has no JSON name")
			default:
				visit(path+"/"+name, field.Type)
			}
		}
	}

	// when
	for _, request := range []any{
		CreateEntryRequest{}, CreateEntryRevisionRequest{}, CreateModelRequest{}, CreateModelRevisionRequest{},
	} {
		visit(reflect.TypeOf(request).Name(), reflect.TypeOf(request))
	}

	// then
	assert.Empty(t, problems)
}

func Test_should_apply_each_mode_to_its_own_errors_when_request_is_checked(t *testing.T) {
	keyErrors := []validation.FieldError{{Pointer: "/entry/colour", Code: validation.CodeUnknownField}}
	schemaErrors := []validation.FieldError{{Pointer: "/entry/title", Code: validation.CodeRequired}}
	cases := []struct {
		name        string
		config      validation.Config
		wantFields  []validation.FieldError
		wantLogKeys bool
	}{
		{
			name:       "both enforce",
			config:     validation.Config{Mode: validation.ModeEnforce, RequestKeys: validation.ModeEnforce},
			wantFields: append(append([]validation.FieldError{}, keyErrors...), schemaErrors...),
		},
		{
			name:        "request keys only logged, as on production during the rollout",
			config:      validation.Config{Mode: validation.ModeEnforce, RequestKeys: validation.ModeLogOnly},
			wantFields:  schemaErrors,
			wantLogKeys: true,
		},
		{
			name:       "schema rules only logged",
			config:     validation.Config{Mode: validation.ModeLogOnly, RequestKeys: validation.ModeEnforce},
			wantFields: keyErrors,
		},
		{
			name:        "both only logged",
			config:      validation.Config{Mode: validation.ModeLogOnly, RequestKeys: validation.ModeLogOnly},
			wantLogKeys: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given
			var logs bytes.Buffer
			useDefaultLoggerForTest(t, slog.New(slog.NewTextHandler(&logs, nil)))
			server := NewServer(nil, nil, auth.Config{}, nil, nil, nil, "", testCase.config)
			request := httptest.NewRequest(http.MethodPost, "/v1/entries", nil)

			// when
			err := server.checkSchema(request, keyErrors, schemaErrors)

			// then
			if testCase.wantFields == nil {
				require.NoError(t, err)
			} else {
				var validationError *validation.Error
				require.ErrorAs(t, err, &validationError)
				assert.Equal(t, testCase.wantFields, validationError.Fields)
			}
			assert.Equal(t, testCase.wantLogKeys, strings.Contains(logs.String(), "rule=request_keys"), logs.String())
		})
	}
}
