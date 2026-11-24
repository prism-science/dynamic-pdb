package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

// residueCount is one conformer-count row of a model file: residues of a type with a number of
// conformers.
type residueCount struct {
	residueType string
	conformers  int
	residues    int
}

// indexResidues gives a model's coordinate file a set read with conformer counts and the rows, as
// the coordinate-index job stores a counted file.
func (f *modelSearchTestDB) indexResidues(t *testing.T, model searchModel, counts ...residueCount) {
	t.Helper()
	residues, multi := 0, 0
	for _, count := range counts {
		residues += count.residues
		if count.conformers >= 2 {
			multi += count.residues
		}
	}
	var key any = "label"
	if residues == 0 {
		key = nil
	}
	f.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
	               conformers, residue_key, residues, multi_conformer_residues, read_as_ensemble_type)
	           values ($1, 'done', '1.1.0', now(), '1.0.0', 'counted', $2, $3, $4, false)`,
		model.coordinateFile, key, residues, multi)
	for _, count := range counts {
		f.exec(t, `insert into coordinate_index_residue_counts (artifact_id, residue_type, conformer_count, residues)
		           values ($1, $2, $3, $4)`, model.coordinateFile, count.residueType, count.conformers, count.residues)
	}
}

func residueSearch(types []string, minConformers int, minResidues int) db.ModelSearch {
	return db.ModelSearch{Residues: &db.ResidueFilter{Types: types, MinConformers: minConformers, MinResidues: minResidues}}
}

func Test_should_match_models_by_the_conformers_of_their_own_residues_when_models_are_searched_by_residues(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	xray := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	nmr := fixture.entry(t, map[string]any{"method": "SOLUTION NMR"})
	// Example 1: a lysine with 4 conformers (Deposited), with 5 (Multiconformer).
	depositedFour := fixture.model(t, xray, map[string]any{"model_type": "Deposited"})
	fixture.indexResidues(t, depositedFour, residueCount{"LYS", 1, 30}, residueCount{"LYS", 4, 1})
	multiconformerFive := fixture.model(t, xray, map[string]any{"model_type": "Multiconformer"})
	fixture.indexResidues(t, multiconformerFive, residueCount{"LYS", 2, 6}, residueCount{"LYS", 5, 1}, residueCount{"ARG", 4, 2})
	// Lysines with 3 conformers only; an arginine with 4.
	threeOnly := fixture.model(t, xray, map[string]any{"model_type": "Multiconformer"})
	fixture.indexResidues(t, threeOnly, residueCount{"LYS", 3, 9}, residueCount{"ARG", 4, 1})
	// Ensembles never match, even with stored counts (a model retyped by a review decision).
	ensembleRows := fixture.model(t, xray, map[string]any{"model_type": "Ensemble"})
	fixture.indexResidues(t, ensembleRows, residueCount{"LYS", 6, 40})
	memberCountRows := fixture.model(t, nmr, map[string]any{"model_type": "Multiconformer", "member_count": 6})
	fixture.indexResidues(t, memberCountRows, residueCount{"LYS", 6, 40})
	// A file not read with counts yet, and one the job stored as an ensemble (no rows).
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`,
		fixture.model(t, xray, map[string]any{"model_type": "Deposited"}).coordinateFile)
	framesFile := fixture.model(t, nmr, map[string]any{"model_type": "Ensemble"})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
	                     conformers, read_as_ensemble_type)
	                 values ($1, 'done', '1.1.0', now(), '1.0.0', 'ensemble_model_frames', true)`, framesFile.coordinateFile)
	// Same-model rule: one entry, its first model has a 4-conformer lysine, its second does not.
	sharedEntry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	withLysine := fixture.model(t, sharedEntry, map[string]any{"model_type": "Deposited"})
	fixture.indexResidues(t, withLysine, residueCount{"LYS", 4, 1})
	withoutLysine := fixture.model(t, sharedEntry, map[string]any{"model_type": "Multiconformer"})
	fixture.indexResidues(t, withoutLysine, residueCount{"LYS", 1, 12})

	multiconformerType := residueSearch([]string{"LYS"}, 4, 1)
	multiconformerType.ModelTypes = []models.StructureModelType{models.StructureModelTypeMulticonformer}
	xrayOnly := residueSearch([]string{"lys"}, 4, 1)
	xrayOnly.Methods = []string{"X-ray crystallography"}
	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "example 1: a lysine with more than three conformations", search: residueSearch([]string{"LYS"}, 4, 1),
			want: []string{depositedFour.id, multiconformerFive.id, withLysine.id}},
		{name: "the conformer bound is inclusive", search: residueSearch([]string{"LYS"}, 5, 1),
			want: []string{multiconformerFive.id}},
		{name: "rows of different counts add up: 6 with 2 and 1 with 5, or 9 with 3", search: residueSearch([]string{"LYS"}, 2, 7),
			want: []string{multiconformerFive.id, threeOnly.id}},
		{name: "several types: a residue of any counts", search: residueSearch([]string{"LYS", "ARG"}, 4, 3),
			want: []string{multiconformerFive.id}},
		{name: "every residue of a type", search: residueSearch([]string{"LYS"}, 1, 10),
			want: []string{depositedFour.id, withoutLysine.id}},
		{name: "case does not matter", search: residueSearch([]string{"arg"}, 4, 1),
			want: []string{multiconformerFive.id, threeOnly.id}},
		{name: "same model only: the model type is the matched model's", search: multiconformerType,
			want: []string{multiconformerFive.id}},
		{name: "with the entry's method", search: xrayOnly,
			want: []string{depositedFour.id, multiconformerFive.id, withLysine.id}},
		{name: "no residue has that many", search: residueSearch([]string{"LYS"}, 16, 1),
			want: nil},
	})
}

func Test_should_refuse_a_residue_filter_when_its_values_are_out_of_range(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	cases := map[string]db.ResidueFilter{
		"no type":       {MinConformers: 1, MinResidues: 1},
		"no conformers": {Types: []string{"LYS"}, MinConformers: 0, MinResidues: 1},
		"more than 16":  {Types: []string{"LYS"}, MinConformers: 17, MinResidues: 1},
		"no residues":   {Types: []string{"LYS"}, MinConformers: 1, MinResidues: 0},
	}
	for name, filter := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := fixture.database.Models.List(context.Background(), activeSearchFilters(db.ModelSearch{Residues: &filter}))

			// then
			require.ErrorIs(t, err, db.ErrInvalidModelSearch)
		})
	}
}
