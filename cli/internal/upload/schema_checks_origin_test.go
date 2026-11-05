package upload

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_not_ask_for_a_run_when_model_names_the_archive_as_its_origin(t *testing.T) {
	// given
	archiveEnsemble := manifest.ModelPattern{ModelType: "Ensemble", Origin: manifest.ModelOriginArchive}
	computedEnsemble := manifest.ModelPattern{ModelType: "Ensemble"}

	// when
	archiveProblems := computedModelProblems(archiveEnsemble, runDraft{}, nil)
	computedProblems := computedModelProblems(computedEnsemble, runDraft{}, nil)

	// then
	assert.Empty(t, archiveProblems)
	assert.NotEmpty(t, computedProblems)
}
