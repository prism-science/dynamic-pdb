package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_accept_related_entry_when_it_uses_the_dictionary_values(t *testing.T) {
	// given
	entries := []models.RelatedEntry{{DBName: "PDB", DBID: "7KR0", ContentType: "re-refinement"}}

	// when
	fieldErrors := RelatedEntries(Root.Field("related_entries"), entries)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_each_field_when_related_entry_breaks_the_dictionary(t *testing.T) {
	// given
	entries := []models.RelatedEntry{{DBName: "UniProt", ContentType: "rerefinement"}}

	// when
	fieldErrors := RelatedEntries(Root.Field("related_entries"), entries)

	// then
	pointers := make([]Pointer, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		pointers = append(pointers, fieldError.Pointer)
	}
	at := Root.Field("related_entries").Index(0)
	assert.ElementsMatch(t, []Pointer{at.Field("db_name"), at.Field("db_id"), at.Field("content_type")}, pointers)
}

func Test_should_report_duplicate_when_related_entry_is_listed_twice(t *testing.T) {
	// given
	entries := []models.RelatedEntry{
		{DBName: "PDB", DBID: "7KR0", ContentType: "re-refinement"},
		{DBName: "PDB", DBID: "7kr0", ContentType: "re-refinement"},
	}

	// when
	fieldErrors := RelatedEntries(Root.Field("related_entries"), entries)

	// then
	assert.Len(t, fieldErrors, 1)
	assert.Equal(t, CodeDuplicate, fieldErrors[0].Code)
}

func Test_should_require_pdb_id_when_model_names_the_archive_as_its_origin(t *testing.T) {
	// given
	origin := models.ModelOriginArchive

	// when
	fieldErrors := ModelMetadata(Root, models.ModelMetadata{Origin: &origin})

	// then
	assert.Len(t, fieldErrors, 1)
	assert.Equal(t, Root.Field("external_refs").Field("pdb"), fieldErrors[0].Pointer)
}

func Test_should_refuse_unknown_origin_when_model_names_one(t *testing.T) {
	// given
	origin := models.ModelOrigin("uploaded")

	// when
	fieldErrors := ModelMetadata(Root, models.ModelMetadata{Origin: &origin})

	// then
	assert.Len(t, fieldErrors, 1)
	assert.Equal(t, Root.Field("origin"), fieldErrors[0].Pointer)
}

func Test_should_refuse_member_count_when_model_is_not_an_ensemble(t *testing.T) {
	// given
	multiconformer := models.StructureModelTypeMulticonformer
	ensemble := models.StructureModelTypeEnsemble
	six, one := 6, 1

	// when
	onMulticonformer := ModelMetadata(Root, models.ModelMetadata{ModelType: &multiconformer, MemberCount: &six})
	tooFew := ModelMetadata(Root, models.ModelMetadata{ModelType: &ensemble, MemberCount: &one})
	onEnsemble := ModelMetadata(Root, models.ModelMetadata{ModelType: &ensemble, MemberCount: &six})

	// then
	assert.Len(t, onMulticonformer, 1)
	assert.Len(t, tooFew, 1)
	assert.Empty(t, onEnsemble)
}

func Test_should_not_count_as_computed_when_model_names_the_archive_as_its_origin(t *testing.T) {
	// given
	origin := models.ModelOriginArchive
	ensemble := models.StructureModelTypeEnsemble

	// when
	computed := ModelRevision{ModelType: &ensemble, Origin: &origin}.IsComputed()
	withoutOrigin := ModelRevision{ModelType: &ensemble}.IsComputed()

	// then
	assert.False(t, computed)
	assert.True(t, withoutOrigin)
}
