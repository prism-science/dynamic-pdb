package rcsb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/manifest"
	rcsbclient "dynamic-pdb/lib/rcsb"
)

func Test_should_extract_rcsb_image(t *testing.T) {
	// given
	contents := []byte("image\n")
	client := &fakeClient{
		images: map[string]rcsbclient.Artifact{
			"5amf_assembly-1.jpeg": {
				Filename: "5amf_assembly-1.jpeg",
				Contents: contents,
			},
		},
	}
	extractor := NewImageExtractor(client)

	// when
	image, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", File: "{{ pdb_id }}_assembly-1.jpeg"},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf_assembly-1.jpeg", image.Filename)
	assert.Equal(t, int64(len(contents)), image.Size)
	assert.Equal(t, contents, image.Contents)
	assert.Equal(t, []string{"5amf_assembly-1.jpeg"}, client.imageNames)
}

func Test_should_return_no_rcsb_image_when_file_is_missing(t *testing.T) {
	// given
	extractor := NewImageExtractor(&fakeClient{})

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}"},
	})

	// then
	require.NoError(t, err)
	assert.False(t, ok)
}
