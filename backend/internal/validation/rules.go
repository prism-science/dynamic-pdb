package validation

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// mutationPattern matches a one-letter substitution such as C54T: the original
// residue, its position, and the new residue.
var mutationPattern = regexp.MustCompile(`^[A-Z][1-9][0-9]*[A-Z]$`)

func RequiredText(at Pointer, value *string) []FieldError {
	if value == nil {
		return []FieldError{required(at)}
	}
	if strings.TrimSpace(*value) == "" {
		return []FieldError{{Pointer: at, Code: CodeRequired, Detail: "must not be blank"}}
	}
	return nil
}

func RequiredID(at Pointer, id uuid.UUID) []FieldError {
	if id == uuid.Nil {
		return []FieldError{required(at)}
	}
	return nil
}

func RequiredNumber(at Pointer, value *float64) []FieldError {
	if value == nil {
		return []FieldError{required(at)}
	}
	return nil
}

// MustNotBeNull reports a field that a request explicitly set to null.
func MustNotBeNull(at Pointer) []FieldError {
	return []FieldError{{Pointer: at, Code: CodeRequired, Detail: "must not be null"}}
}

func Artifact(at Pointer, artifact models.Artifact) []FieldError {
	var fieldErrors []FieldError
	fieldErrors = append(fieldErrors, RequiredText(at.Field("name"), &artifact.Name)...)
	fieldErrors = append(fieldErrors, requiredVocabularyValue(at.Field("level"), artifact.Level)...)
	fieldErrors = append(fieldErrors, requiredVocabularyValue(at.Field("type"), artifact.Type)...)
	if artifact.SHA256 != nil && !sha256Pattern.MatchString(*artifact.SHA256) {
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at.Field("sha256"),
			Code:    CodeInvalidFormat,
			Detail:  "must be 64 lowercase hexadecimal characters",
		})
	}
	// Format is an open vocabulary, so only its case is checked.
	if artifact.Format != nil && *artifact.Format != strings.ToLower(*artifact.Format) {
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at.Field("format"),
			Code:    CodeInvalidFormat,
			Detail:  "must be lowercase",
		})
	}
	if artifact.SizeBytes != nil && *artifact.SizeBytes < 0 {
		fieldErrors = append(fieldErrors, notNegative(at.Field("size_bytes")))
	}
	return fieldErrors
}

func Metric(at Pointer, metric models.Metric) []FieldError {
	return requiredVocabularyValue(at.Field("key"), metric.Key)
}

// Metrics checks the metric set of one model revision, where each key may appear once.
func Metrics(at Pointer, metrics []models.Metric) []FieldError {
	var fieldErrors []FieldError
	firstIndexByKey := make(map[models.MetricKey]int, len(metrics))
	for index, metric := range metrics {
		fieldErrors = append(fieldErrors, Metric(at.Index(index), metric)...)
		if metric.Key == "" {
			continue
		}
		if firstIndex, seen := firstIndexByKey[metric.Key]; seen {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: at.Index(index).Field("key"),
				Code:    CodeDuplicate,
				Detail:  fmt.Sprintf("metric key %q is already used at %s", metric.Key, at.Index(firstIndex)),
			})
			continue
		}
		firstIndexByKey[metric.Key] = index
	}
	return fieldErrors
}

func Run(at Pointer, run models.Run) []FieldError {
	return RequiredText(at.Field("name"), &run.Name)
}

func RunArtifact(at Pointer, direction models.RunArtifactDirection) []FieldError {
	return requiredVocabularyValue(at.Field("direction"), direction)
}

func EntryMetadata(at Pointer, metadata models.EntryMetadata) []FieldError {
	fieldErrors := externalRefs(at.Field("external_refs"), metadata.ExternalRefs)
	if metadata.Resolution != nil && *metadata.Resolution <= 0 {
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at.Field("resolution"),
			Code:    CodeOutOfRange,
			Detail:  "must be greater than 0",
		})
	}
	return fieldErrors
}

func ModelMetadata(at Pointer, metadata models.ModelMetadata) []FieldError {
	fieldErrors := externalRefs(at.Field("external_refs"), metadata.ExternalRefs)
	if metadata.Purpose != nil {
		fieldErrors = append(fieldErrors, vocabularyValue(at.Field("purpose"), *metadata.Purpose)...)
	}
	if metadata.ModelType != nil {
		fieldErrors = append(fieldErrors, vocabularyValue(at.Field("model_type"), *metadata.ModelType)...)
	}
	counts := []struct {
		name  string
		value *int
	}{
		{"atom_count", metadata.AtomCount},
		{"modeled_residues", metadata.ModeledResidues},
		{"unique_protein_chains", metadata.UniqueProteinChains},
	}
	for _, count := range counts {
		if count.value != nil && *count.value < 0 {
			fieldErrors = append(fieldErrors, notNegative(at.Field(count.name)))
		}
	}
	fractions := []struct {
		name  string
		value *float64
	}{
		{"altloc_fraction", metadata.AltLocFraction},
		{"unmodeled_fraction", metadata.UnmodeledFraction},
	}
	for _, fraction := range fractions {
		if fraction.value != nil && (*fraction.value < 0 || *fraction.value > 1) {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: at.Field(fraction.name),
				Code:    CodeOutOfRange,
				Detail:  "must be between 0 and 1",
			})
		}
	}
	return fieldErrors
}

func PolymerEntity(at Pointer, metadata models.PolymerEntityMetadata) []FieldError {
	var fieldErrors []FieldError
	for index, mapping := range metadata.UniProtMappings {
		mappingAt := at.Field("uniprot_mappings").Index(index)
		fieldErrors = append(fieldErrors, RequiredText(mappingAt.Field("accession"), &mapping.Accession)...)
		fieldErrors = append(fieldErrors, uniProtSourceErrors(mappingAt, mapping)...)
	}
	return fieldErrors
}

// uniProtSourceErrors checks a mapping's source and, for sources that depend on a
// UniProt release, that the release is named.
func uniProtSourceErrors(at Pointer, mapping models.PolymerEntityUniProtReference) []FieldError {
	if sourceErrors := requiredVocabularyValue(at.Field("source"), mapping.Source); len(sourceErrors) > 0 {
		return sourceErrors
	}
	requiresRelease, err := mapping.Source.RequiresUniProtRelease()
	if err != nil {
		return []FieldError{{Pointer: at.Field("source"), Code: CodeInvalidValue, Detail: err.Error()}}
	}
	releaseAt := at.Field("unp_release")
	if requiresRelease && len(RequiredText(releaseAt, mapping.UniProtRelease)) > 0 {
		return []FieldError{{
			Pointer: releaseAt,
			Code:    CodeRequired,
			Detail:  fmt.Sprintf("is required for UniProt source %q", mapping.Source),
		}}
	}
	return nil
}

// externalRefs checks that every key of an external_refs map is a supported source.
func externalRefs[K vocabulary](at Pointer, refs map[K]string) []FieldError {
	var fieldErrors []FieldError
	for _, source := range sortedKeys(refs) {
		fieldErrors = append(fieldErrors, vocabularyValue(at.Field(string(source)), source)...)
	}
	return fieldErrors
}

type vocabulary interface {
	~string
	Validate() error
}

// vocabularyValue checks a field whose value, when given, must come from a schema list.
func vocabularyValue[T vocabulary](at Pointer, value T) []FieldError {
	if err := value.Validate(); err != nil {
		return []FieldError{{Pointer: at, Code: CodeInvalidValue, Detail: err.Error()}}
	}
	return nil
}

// requiredVocabularyValue is vocabularyValue for a field that must always be set.
func requiredVocabularyValue[T vocabulary](at Pointer, value T) []FieldError {
	if value == "" {
		return []FieldError{required(at)}
	}
	return vocabularyValue(at, value)
}

func required(at Pointer) FieldError {
	return FieldError{Pointer: at, Code: CodeRequired, Detail: "is required"}
}

func notNegative(at Pointer) FieldError {
	return FieldError{Pointer: at, Code: CodeOutOfRange, Detail: "must not be negative"}
}

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// NewEntryMethod checks that an entry being created names its experimental method.
// The method is free text, so nothing beyond presence is checked.
func NewEntryMethod(at Pointer, metadata models.EntryMetadata) []FieldError {
	var method *string
	if metadata.Method != nil {
		text := string(*metadata.Method)
		method = &text
	}
	return RequiredText(at.Field("method"), method)
}

// DepositorPolymerEntities checks polymer-entity values a depositor supplies on a
// create or revision request.
func DepositorPolymerEntities(at Pointer, entities []models.DepositorPolymerEntity) []FieldError {
	var fieldErrors []FieldError
	firstIndexByLabel := make(map[string]int, len(entities))
	for index, entity := range entities {
		entityAt := at.Index(index)
		labelAt := entityAt.Field("label_entity_id")
		labelErrors := RequiredText(labelAt, &entity.LabelEntityID)
		fieldErrors = append(fieldErrors, labelErrors...)
		if len(labelErrors) == 0 {
			if firstIndex, seen := firstIndexByLabel[entity.LabelEntityID]; seen {
				fieldErrors = append(fieldErrors, FieldError{
					Pointer: labelAt,
					Code:    CodeDuplicate,
					Detail:  fmt.Sprintf("entity %q is already described at %s", entity.LabelEntityID, at.Index(firstIndex)),
				})
			} else {
				firstIndexByLabel[entity.LabelEntityID] = index
			}
		}
		fieldErrors = append(fieldErrors, mutations(entityAt.Field("mutations"), entity.Mutations)...)
		for organismIndex, organism := range entity.SourceOrganisms {
			organismAt := entityAt.Field("source_organisms").Index(organismIndex)
			fieldErrors = append(fieldErrors, RequiredText(organismAt.Field("scientific_name"), &organism.ScientificName)...)
			if organism.NCBITaxonomyID != nil && *organism.NCBITaxonomyID <= 0 {
				fieldErrors = append(fieldErrors, FieldError{
					Pointer: organismAt.Field("ncbi_taxonomy_id"),
					Code:    CodeOutOfRange,
					Detail:  "must be greater than 0",
				})
			}
		}
	}
	return fieldErrors
}

func mutations(at Pointer, values []string) []FieldError {
	var fieldErrors []FieldError
	for index, value := range values {
		if !mutationPattern.MatchString(value) {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: at.Index(index),
				Code:    CodeInvalidFormat,
				Detail:  "must be one substitution such as C54T: original residue, position, new residue",
			})
		}
	}
	return fieldErrors
}
