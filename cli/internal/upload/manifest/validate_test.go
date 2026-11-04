package manifest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_accept_manifest_when_values_are_in_api_vocabularies(t *testing.T) {
	// given
	manifest := validManifest()

	// when
	err := Validate(manifest)

	// then
	assert.NoError(t, err)
}

func Test_should_accept_manifest_built_from_data_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.pdb", "MODEL\n")
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.log", "ok\n")
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.mtz", "mtz\n")
	built, _, err := Build(dataRoot, BuildOptions{IncludeRCSBModel: true})
	require.NoError(t, err)

	// when
	err = Validate(built)

	// then
	assert.NoError(t, err)
}

func Test_should_list_every_problem_when_manifest_breaks_api_vocabularies(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(manifest *Manifest)
		problem string
	}{
		{
			name: "unknown metric key",
			mutate: func(manifest *Manifest) {
				manifest.Entries[0].Models[0].Metrics["resolution"] = manifest.Entries[0].Models[0].Metrics["r_free"]
			},
			problem: `entries[0].models[0].metrics.resolution: unknown metric key "resolution"; use one of r_work, r_free, cc, rscc, clashscore, ramachandran_outliers, side_chain_outliers, molprobity_score, wilson_b, rmsf`,
		},
		{
			name:    "unknown model type",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].ModelType = "Multi-conformer" },
			problem: `entries[0].models[0].model_type: unknown model type "Multi-conformer"; use one of Single Conformer, Multiconformer, Ensemble, Deposited`,
		},
		{
			name:    "unknown purpose",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].Purpose = "Validation" },
			problem: `entries[0].models[0].purpose: unknown purpose "Validation"; use one of Model Building, Refinement, Reference`,
		},
		{
			name:    "unknown model artifact level",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].Artifacts[0].Level = "l2" },
			problem: `entries[0].models[0].artifacts[0].level: unknown artifact level "l2"; use one of L0, L1, L2, L3`,
		},
		{
			name:    "unknown entry artifact type",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Artifacts[0].Type = "sequence" },
			problem: `entries[0].artifacts[0].type: unknown artifact type "sequence"; use one of model, structure_factors, fasta, other`,
		},
		{
			name: "software without a name",
			mutate: func(manifest *Manifest) {
				manifest.Entries[0].Models[0].Metrics["clashscore"][0].Software = &Software{Version: "4.5.2"}
			},
			problem: `entries[0].models[0].metrics.clashscore[0].software.name: is required when software is set`,
		},
		{
			name: "software on model metadata",
			mutate: func(manifest *Manifest) {
				manifest.Entries[0].Models[0].Metadata["atom_count"][0].Software = &Software{Name: "PHENIX"}
			},
			problem: `entries[0].models[0].metadata.atom_count[0].software: only metric extractions may name software`,
		},
		{
			name: "software on entry metadata",
			mutate: func(manifest *Manifest) {
				manifest.Entries[0].Metadata["title"][0].Software = &Software{Name: "PHENIX"}
			},
			problem: `entries[0].metadata.title[0].software: only metric extractions may name software`,
		},
		{
			name:    "run input that is not an artifact of the model",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].Run.Inputs = []string{"mtz_9"} },
			problem: `entries[0].models[0].run.inputs[0]: the model has no artifact with id "mtz_9"`,
		},
		{
			name:    "run output that is not an artifact of the model",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].Run.Outputs = []string{"log_9"} },
			problem: `entries[0].models[0].run.outputs[0]: the model has no artifact with id "log_9"`,
		},
		{
			name:    "run artifact in both directions",
			mutate:  func(manifest *Manifest) { manifest.Entries[0].Models[0].Run.Outputs = []string{"coordinates", "mtz_1"} },
			problem: `entries[0].models[0].run: artifact "mtz_1" is listed in both inputs and outputs`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			manifest := validManifest()
			test.mutate(&manifest)

			// when
			err := Validate(manifest)

			// then
			var validationError *ValidationError
			require.True(t, errors.As(err, &validationError))
			assert.Equal(t, []string{test.problem}, validationError.Problems)
		})
	}
}

func Test_should_report_problem_count_and_paths_when_manifest_has_several_problems(t *testing.T) {
	// given
	manifest := validManifest()
	manifest.Entries[0].Models[0].ModelType = "Mixed"
	manifest.Entries[0].Models[0].Artifacts[0].Level = "L9"

	// when
	err := Validate(manifest)

	// then
	require.Error(t, err)
	assert.Equal(t, "manifest has 2 problems; nothing was uploaded:\n"+
		`  entries[0].models[0].model_type: unknown model type "Mixed"; use one of Single Conformer, Multiconformer, Ensemble, Deposited`+"\n"+
		`  entries[0].models[0].artifacts[0].level: unknown artifact level "L9"; use one of L0, L1, L2, L3`, err.Error())
}

func validManifest() Manifest {
	coordinates := Source{Artifact: "coordinates"}
	return Manifest{
		Version:  1,
		DataRoot: ".",
		Entries: []Entry{
			{
				PDBID: "{{ pdb_id }}",
				Title: "{{ pdb_id }}",
				Metadata: EntryMetadata{
					"title": newFieldExtractions(rcsbResourceSource("entry"), Extract{JSON: &ExtractRule{Field: "struct.title"}}),
				},
				Artifacts: []Artifact{{ID: "fasta", Source: rcsbResourceSource("fasta"), Level: "L0", Type: "fasta"}},
				Models: []ModelPattern{
					{
						ID:        "model_1",
						Title:     "qFit model",
						ModelType: "Multiconformer",
						Purpose:   "Refinement",
						Metadata: ModelMetadata{
							"atom_count": newFieldExtractions(coordinates, Extract{PDB: &ExtractRule{Field: "atom_count"}}),
						},
						Artifacts: []Artifact{
							{ID: "coordinates", Source: Source{Files: []string{"{{ pdb_id }}.pdb"}}, Level: "L2", Type: "model"},
							{ID: "mtz_1", Source: Source{Files: []string{"{{ pdb_id }}.mtz"}}, Level: "L1"},
						},
						Metrics: Metrics{
							"r_free": newFieldExtractions(coordinates, Extract{PDB: &ExtractRule{Field: "REMARK 3 FREE R VALUE"}}),
							"clashscore": {{
								Source:   Source{Files: []string{"scores.csv"}},
								Extract:  Extract{CSV: &ExtractRule{Column: "clashscore"}},
								Software: &Software{Name: "MolProbity", Version: "4.5.2"},
							}},
						},
						Run: &Run{SoftwareName: "qFit", SoftwareVersion: "2024.1", Inputs: []string{"mtz_1"}, Outputs: []string{"coordinates"}},
					},
				},
			},
		},
	}
}
