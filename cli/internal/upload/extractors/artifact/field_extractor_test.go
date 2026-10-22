package artifact

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_field_from_artifact_payload(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {
			Filename: "model.cif",
			Contents: []byte("_refine.ls_R_factor_R_free 0.243\n"),
		},
	})

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}
