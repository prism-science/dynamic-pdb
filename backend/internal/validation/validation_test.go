package validation

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_escape_tilde_and_slash_when_pointer_field_is_added(t *testing.T) {
	// given
	base := Root.Field("model").Field("metadata")

	// when
	pointer := base.Field("a/b~c").Index(2)

	// then
	assert.Equal(t, Pointer("/model/metadata/a~1b~0c/2"), pointer)
}

func Test_should_report_required_when_text_is_missing_or_blank(t *testing.T) {
	// given
	blank := "   "
	title := "Lysozyme"

	// when
	missingErrors := RequiredText(Root.Field("title"), nil)
	blankErrors := RequiredText(Root.Field("title"), &blank)
	presentErrors := RequiredText(Root.Field("title"), &title)

	// then
	assert.Equal(t, []FieldError{{Pointer: "/title", Code: CodeRequired, Detail: "is required"}}, missingErrors)
	assert.Equal(t, []FieldError{{Pointer: "/title", Code: CodeRequired, Detail: "must not be blank"}}, blankErrors)
	assert.Empty(t, presentErrors)
}

func Test_should_accept_artifact_when_it_follows_schema(t *testing.T) {
	// given
	artifact := validArtifact()

	// when
	fieldErrors := Artifact(Root, artifact)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_every_artifact_error_when_several_fields_are_invalid(t *testing.T) {
	// given
	artifact := models.Artifact{
		Name:      " ",
		Level:     "L9",
		Type:      "assembly",
		SHA256:    new("ABC"),
		Format:    new("MMCIF"),
		SizeBytes: new(int64(-1)),
	}

	// when
	fieldErrors := Artifact(Root.Field("artifacts").Index(1), artifact)

	// then
	assert.Equal(t, []Pointer{
		"/artifacts/1/name",
		"/artifacts/1/level",
		"/artifacts/1/type",
		"/artifacts/1/sha256",
		"/artifacts/1/format",
		"/artifacts/1/size_bytes",
	}, pointersOf(fieldErrors))
	assert.Equal(t, []Code{
		CodeRequired, CodeInvalidValue, CodeInvalidValue, CodeInvalidFormat, CodeInvalidFormat, CodeOutOfRange,
	}, codesOf(fieldErrors))
}

func Test_should_report_required_type_when_artifact_type_is_empty(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.Type = ""

	// when
	fieldErrors := Artifact(Root, artifact)

	// then
	assert.Equal(t, []FieldError{{Pointer: "/type", Code: CodeRequired, Detail: "is required"}}, fieldErrors)
}

func Test_should_reject_format_when_it_has_uppercase_letters(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.Format = new("MMCIF")

	// when
	fieldErrors := Artifact(Root, artifact)

	// then
	assert.Equal(t, []FieldError{{Pointer: "/format", Code: CodeInvalidFormat, Detail: "must be lowercase"}}, fieldErrors)
}

func Test_should_accept_format_when_lowercase_even_if_not_a_listed_example(t *testing.T) {
	// given
	listed := validArtifact()
	listed.Format = new("mmcif")
	unlisted := validArtifact()
	unlisted.Format = new("pdbqt_v2")

	// when
	listedErrors := Artifact(Root, listed)
	unlistedErrors := Artifact(Root, unlisted)

	// then
	assert.Empty(t, listedErrors)
	assert.Empty(t, unlistedErrors)
}

func Test_should_reject_sha256_when_hex_is_uppercase(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.SHA256 = new(strings.ToUpper(*artifact.SHA256))

	// when
	fieldErrors := Artifact(Root, artifact)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/sha256",
		Code:    CodeInvalidFormat,
		Detail:  "must be 64 lowercase hexadecimal characters",
	}}, fieldErrors)
}

func Test_should_report_required_level_when_artifact_level_is_empty(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.Level = ""

	// when
	fieldErrors := Artifact(Root, artifact)

	// then
	assert.Equal(t, []FieldError{{Pointer: "/level", Code: CodeRequired, Detail: "is required"}}, fieldErrors)
}

func Test_should_accept_metrics_when_every_schema_key_is_used_once(t *testing.T) {
	// given
	metrics := make([]models.Metric, 0, len(models.MetricKeys))
	for _, key := range models.MetricKeys {
		metrics = append(metrics, models.Metric{Key: key, Value: 1})
	}

	// when
	fieldErrors := Metrics(Root.Field("metrics"), metrics)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_metric_when_key_not_in_schema(t *testing.T) {
	// given
	metrics := []models.Metric{{Key: "resolution", Value: 2.1}}

	// when
	fieldErrors := Metrics(Root.Field("metrics"), metrics)

	// then
	require.Len(t, fieldErrors, 1)
	assert.Equal(t, Pointer("/metrics/0/key"), fieldErrors[0].Pointer)
	assert.Equal(t, CodeInvalidValue, fieldErrors[0].Code)
	assert.Contains(t, fieldErrors[0].Detail, `metric key "resolution" is not one of`)
}

func Test_should_reject_metric_when_key_repeats_in_one_revision(t *testing.T) {
	// given
	metrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.21},
		{Key: models.MetricKeyRWork, Value: 0.18},
		{Key: models.MetricKeyRFree, Value: 0.22},
	}

	// when
	fieldErrors := Metrics(Root.Field("metrics"), metrics)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/metrics/2/key",
		Code:    CodeDuplicate,
		Detail:  `metric key "r_free" is already used at /metrics/0`,
	}}, fieldErrors)
}

func Test_should_report_required_when_run_name_is_blank(t *testing.T) {
	// given
	run := models.Run{Name: ""}

	// when
	fieldErrors := Run(Root.Field("runs").Index(0), run)

	// then
	assert.Equal(t, []FieldError{{Pointer: "/runs/0/name", Code: CodeRequired, Detail: "must not be blank"}},
		fieldErrors)
}

func Test_should_reject_run_artifact_when_direction_is_unknown_or_missing(t *testing.T) {
	// given
	at := Root.Field("runs").Index(0).Field("artifacts")

	// when
	unknownErrors := RunArtifact(at.Index(0), "both")
	missingErrors := RunArtifact(at.Index(1), "")
	validErrors := RunArtifact(at.Index(2), models.RunArtifactDirectionOutput)

	// then
	assert.Equal(t, []Pointer{"/runs/0/artifacts/0/direction"}, pointersOf(unknownErrors))
	assert.Equal(t, []Code{CodeInvalidValue}, codesOf(unknownErrors))
	assert.Equal(t, []FieldError{{Pointer: "/runs/0/artifacts/1/direction", Code: CodeRequired, Detail: "is required"}},
		missingErrors)
	assert.Empty(t, validErrors)
}

func Test_should_accept_model_when_type_deposited_and_purpose_reference(t *testing.T) {
	// given
	metadata := models.ModelMetadata{
		Purpose:   new(models.ModelPurposeReference),
		ModelType: new(models.StructureModelTypeDeposited),
		ExternalRefs: map[models.ModelSource]string{
			models.ModelSourcePDB: "1ABC",
		},
		AtomCount:         new(0),
		AltLocFraction:    new(0.0),
		UnmodeledFraction: new(1.0),
	}

	// when
	fieldErrors := ModelMetadata(Root.Field("metadata"), metadata)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_every_model_metadata_error_when_several_fields_are_invalid(t *testing.T) {
	// given
	metadata := models.ModelMetadata{
		Purpose:             new(models.ModelPurpose("Docking")),
		ModelType:           new(models.StructureModelType("Trajectory")),
		ExternalRefs:        map[models.ModelSource]string{"emdb": "EMD-1"},
		AtomCount:           new(-1),
		ModeledResidues:     new(-2),
		UniqueProteinChains: new(-3),
		AltLocFraction:      new(1.5),
		UnmodeledFraction:   new(-0.1),
	}

	// when
	fieldErrors := ModelMetadata(Root.Field("metadata"), metadata)

	// then
	assert.Equal(t, []Pointer{
		"/metadata/external_refs/emdb",
		"/metadata/purpose",
		"/metadata/model_type",
		"/metadata/atom_count",
		"/metadata/modeled_residues",
		"/metadata/unique_protein_chains",
		"/metadata/altloc_fraction",
		"/metadata/unmodeled_fraction",
	}, pointersOf(fieldErrors))
	assert.Equal(t, []Code{
		CodeInvalidValue, CodeInvalidValue, CodeInvalidValue,
		CodeOutOfRange, CodeOutOfRange, CodeOutOfRange, CodeOutOfRange, CodeOutOfRange,
	}, codesOf(fieldErrors))
}

func Test_should_reject_entry_metadata_when_resolution_is_not_positive_or_ref_is_unknown(t *testing.T) {
	// given
	metadata := models.EntryMetadata{
		ExternalRefs: map[models.EntrySource]string{"emdb": "EMD-1", models.EntrySourcePDB: "1ABC"},
		Resolution:   new(0.0),
	}

	// when
	fieldErrors := EntryMetadata(Root.Field("entry"), metadata)

	// then
	assert.Equal(t, []Pointer{"/entry/external_refs/emdb", "/entry/resolution"}, pointersOf(fieldErrors))
	assert.Equal(t, []Code{CodeInvalidValue, CodeOutOfRange}, codesOf(fieldErrors))
}

func Test_should_require_unp_release_when_uniprot_source_is_sifts_or_alignment(t *testing.T) {
	// given
	metadata := models.PolymerEntityMetadata{
		UniProtMappings: []models.PolymerEntityUniProtReference{
			{Accession: "P00720", Source: models.UniProtReferenceSourceSIFTS},
			{Accession: "P00720", Source: models.UniProtReferenceSourceAlignment},
			{Accession: "P00720", Source: models.UniProtReferenceSourceStructRef},
			{Accession: "P00720", Source: models.UniProtReferenceSourceAlignment, UniProtRelease: new("2026_04")},
		},
	}

	// when
	fieldErrors := PolymerEntity(Root.Field("polymer_entities").Index(0), metadata)

	// then
	assert.Equal(t, []FieldError{
		{
			Pointer: "/polymer_entities/0/uniprot_mappings/0/unp_release",
			Code:    CodeRequired,
			Detail:  `is required for UniProt source "sifts"`,
		},
		{
			Pointer: "/polymer_entities/0/uniprot_mappings/1/unp_release",
			Code:    CodeRequired,
			Detail:  `is required for UniProt source "alignment"`,
		},
	}, fieldErrors)
}

func Test_should_reject_uniprot_mapping_when_source_unknown_or_accession_blank(t *testing.T) {
	// given
	metadata := models.PolymerEntityMetadata{
		UniProtMappings: []models.PolymerEntityUniProtReference{
			{Accession: " ", Source: "uniprot"},
		},
	}

	// when
	fieldErrors := PolymerEntity(Root, metadata)

	// then
	assert.Equal(t, []Pointer{"/uniprot_mappings/0/accession", "/uniprot_mappings/0/source"}, pointersOf(fieldErrors))
	assert.Equal(t, []Code{CodeRequired, CodeInvalidValue}, codesOf(fieldErrors))
}

func Test_should_return_error_with_every_field_when_mode_is_enforce(t *testing.T) {
	// given
	fieldErrors := []FieldError{
		{Pointer: "/model/metrics/0/key", Code: CodeInvalidValue, Detail: "bad key"},
		{Pointer: "/model/title", Code: CodeRequired, Detail: "is required"},
	}

	// when
	err := ModeEnforce.Check(context.Background(), slog.Default(), "create model", fieldErrors)

	// then
	var validationError *Error
	require.ErrorAs(t, err, &validationError)
	assert.Equal(t, fieldErrors, validationError.Fields)
	assert.Equal(t,
		"schema validation failed: /model/metrics/0/key INVALID_VALUE (bad key); /model/title REQUIRED (is required)",
		err.Error(),
	)
}

func Test_should_log_and_allow_write_when_mode_is_log_only(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	fieldErrors := []FieldError{{Pointer: "/model/metrics/0/key", Code: CodeInvalidValue, Detail: "bad key"}}

	// when
	err := ModeLogOnly.Check(context.Background(), logger, "POST /v1/entries/{entry_id}/models", fieldErrors)

	// then
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), `msg="schema validation failed"`)
	assert.Contains(t, logs.String(), `subject="POST /v1/entries/{entry_id}/models"`)
	assert.Contains(t, logs.String(), "/model/metrics/0/key INVALID_VALUE (bad key)")
}

func Test_should_allow_write_without_logging_when_no_field_errors(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	for _, mode := range []Mode{ModeEnforce, ModeLogOnly} {
		// when
		err := mode.Check(context.Background(), logger, "create model", nil)

		// then
		require.NoError(t, err)
	}
	assert.Empty(t, logs.String())
}

func Test_should_fail_loudly_when_mode_is_unknown(t *testing.T) {
	// given
	mode := Mode("warn")
	fieldErrors := []FieldError{{Pointer: "/title", Code: CodeRequired, Detail: "is required"}}

	// when
	checkErr := mode.Check(context.Background(), slog.Default(), "create entry", fieldErrors)
	validateErr := mode.Validate()

	// then
	require.EqualError(t, checkErr, "unknown validation mode: warn")
	require.EqualError(t, validateErr, "unknown validation mode: warn")
	assert.NoError(t, ModeEnforce.Validate())
	assert.NoError(t, ModeLogOnly.Validate())
}

func validArtifact() models.Artifact {
	return models.Artifact{
		Name:      "model.cif",
		Level:     models.ArtifactLevelL2,
		Type:      models.ArtifactTypeModel,
		SHA256:    new("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"),
		SizeBytes: new(int64(0)),
	}
}

func pointersOf(fieldErrors []FieldError) []Pointer {
	pointers := make([]Pointer, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		pointers = append(pointers, fieldError.Pointer)
	}
	return pointers
}

func codesOf(fieldErrors []FieldError) []Code {
	codes := make([]Code, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		codes = append(codes, fieldError.Code)
	}
	return codes
}

type objectKeysShape struct {
	Name       string                      `json:"name"`
	Tags       []string                    `json:"tags"`
	Nested     *objectKeysNested           `json:"nested"`
	Items      []objectKeysNested          `json:"items"`
	Extensions map[string]any              `json:"extensions"`
	Lookup     map[string]objectKeysNested `json:"lookup"`
	Skipped    string                      `json:"-"`
}

type objectKeysNested struct {
	Value int `json:"value"`
}

func Test_should_report_unknown_field_when_object_has_key_outside_shape(t *testing.T) {
	// given
	raw := map[string]any{
		"name":   "ok",
		"colour": "blue",
		"nested": map[string]any{"value": 1, "extra": true},
		"items":  []any{map[string]any{"value": 1}, map[string]any{"other": 2}},
	}

	// when
	fieldErrors := ObjectKeys(Root.Field("metadata"), raw, objectKeysShape{})

	// then
	assert.Equal(t, []Pointer{
		"/metadata/colour",
		"/metadata/items/1/other",
		"/metadata/nested/extra",
	}, pointersOf(fieldErrors))
	for _, fieldError := range fieldErrors {
		assert.Equal(t, CodeUnknownField, fieldError.Code)
	}
}

func Test_should_accept_any_key_when_it_sits_under_an_open_object(t *testing.T) {
	// given
	raw := map[string]any{
		"extensions": map[string]any{"anything": map[string]any{"deep": 1}},
		"lookup":     map[string]any{"free-key": map[string]any{"value": 2}},
	}

	// when
	fieldErrors := ObjectKeys(Root, raw, objectKeysShape{})

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_reserved_key_when_it_starts_with_double_underscore_at_any_depth(t *testing.T) {
	// given
	raw := map[string]any{
		"__internal": 1,
		"extensions": map[string]any{"ok": 1, "__hidden": map[string]any{"__deeper": 2}},
		"name":       "kept",
		"_single":    1,
	}

	// when
	fieldErrors := ObjectKeys(Root.Field("metadata"), raw, objectKeysShape{})

	// then
	assert.Equal(t, []FieldError{
		{Pointer: "/metadata/__internal", Code: CodeReservedKey, Detail: `keys starting with "__" are reserved`},
		{Pointer: "/metadata/_single", Code: CodeUnknownField, Detail: `is not a field of this object; put open data under "extensions"`},
		{Pointer: "/metadata/extensions/__hidden", Code: CodeReservedKey, Detail: `keys starting with "__" are reserved`},
	}, fieldErrors)
}

func Test_should_report_mutation_when_it_is_not_a_substitution(t *testing.T) {
	// given
	metadata := models.PolymerEntityMetadata{Mutations: []string{"C54T", "c97a", "54T", "C54T, C97A"}}

	// when
	fieldErrors := PolymerEntity(Root.Field("entity"), metadata)

	// then
	assert.Equal(t, []Pointer{"/entity/mutations/1", "/entity/mutations/2", "/entity/mutations/3"}, pointersOf(fieldErrors))
	assert.Equal(t, CodeInvalidFormat, fieldErrors[0].Code)
}

func Test_should_accept_depositor_polymer_entities_when_fields_follow_schema(t *testing.T) {
	// given
	construct := "residues 1-164"
	entities := []models.DepositorPolymerEntity{{
		LabelEntityID:   "1",
		Construct:       &construct,
		Mutations:       []string{"C54T", "C97A"},
		SourceOrganisms: []models.PolymerEntityOrganism{{ScientificName: "Escherichia virus T4", NCBITaxonomyID: new(10665)}},
	}}

	// when
	fieldErrors := DepositorPolymerEntities(Root.Field("polymer_entities"), entities)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_every_depositor_polymer_entity_error_when_several_fields_are_wrong(t *testing.T) {
	// given
	entities := []models.DepositorPolymerEntity{
		{LabelEntityID: "1", Mutations: []string{"nope"}},
		{LabelEntityID: "1", SourceOrganisms: []models.PolymerEntityOrganism{{ScientificName: " ", NCBITaxonomyID: new(0)}}},
		{LabelEntityID: ""},
	}

	// when
	fieldErrors := DepositorPolymerEntities(Root.Field("polymer_entities"), entities)

	// then
	assert.Equal(t, []Pointer{
		"/polymer_entities/0/mutations/0",
		"/polymer_entities/1/label_entity_id",
		"/polymer_entities/1/source_organisms/0/scientific_name",
		"/polymer_entities/1/source_organisms/0/ncbi_taxonomy_id",
		"/polymer_entities/2/label_entity_id",
	}, pointersOf(fieldErrors))
}

func Test_should_require_method_when_new_entry_metadata_has_none(t *testing.T) {
	// given
	blank := models.StructureMethod("  ")
	method := models.StructureMethod("Neutron diffraction")

	// when
	missingErrors := NewEntryMethod(Root.Field("entry"), models.EntryMetadata{})
	blankErrors := NewEntryMethod(Root.Field("entry"), models.EntryMetadata{Method: &blank})
	freeTextErrors := NewEntryMethod(Root.Field("entry"), models.EntryMetadata{Method: &method})

	// then
	assert.Equal(t, []Pointer{"/entry/method"}, pointersOf(missingErrors))
	assert.Equal(t, []Pointer{"/entry/method"}, pointersOf(blankErrors))
	assert.Empty(t, freeTextErrors)
}
