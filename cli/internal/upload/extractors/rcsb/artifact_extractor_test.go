package rcsb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rcsbclient "dynamic-pdb/cli/internal/rcsb"
	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_rcsb_file_artifact(t *testing.T) {
	// given
	client := &fakeClient{
		files: map[string]rcsbclient.Artifact{
			"5amf.cif": {
				Filename: "5amf.cif",
				Format:   "cif",
				URI:      "https://files.rcsb.test/download/5AMF.cif",
			},
		},
	}
	extractor := NewArtifactExtractor(client)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", File: "{{ pdb_id }}.cif"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf.cif", artifact.Filename)
	assert.Equal(t, "cif", artifact.Format)
	assert.Equal(t, "https://files.rcsb.test/download/5AMF.cif", artifact.URI)
	assert.Zero(t, artifact.Size)
	assert.Empty(t, artifact.SHA256)
	assert.Empty(t, artifact.Contents)
	assert.Empty(t, artifact.Metadata)
	assert.Equal(t, []string{"5amf.cif"}, client.fileNames)
}

func Test_should_download_rcsb_coordinates_artifact_for_field_extraction(t *testing.T) {
	// given
	contents := []byte("data_5amf\n")
	client := &fakeClient{
		downloadedFiles: map[string]rcsbclient.Artifact{
			"5amf.cif": {
				Filename: "5amf.cif",
				Format:   "cif",
				URI:      "https://files.rcsb.test/download/5AMF.cif",
				Contents: contents,
			},
		},
	}
	extractor := NewArtifactExtractor(client)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		ID:     "coordinates",
		Source: manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", File: "{{ pdb_id }}.cif"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "https://files.rcsb.test/download/5AMF.cif", artifact.URI)
	assert.Equal(t, int64(len(contents)), artifact.Size)
	assert.NotEmpty(t, artifact.SHA256)
	assert.Equal(t, contents, artifact.Contents)
	assert.Equal(t, []string{"5amf.cif"}, client.downloadFileNames)
	assert.Empty(t, client.fileNames)
}

func Test_should_extract_rcsb_fasta_artifact_with_parsed_records(t *testing.T) {
	// given
	contents := []byte(">5amf A\nAC\nDE\n>5amf B\nFG\n")
	client := &fakeClient{
		fasta: rcsbclient.Artifact{
			Filename: "5amf.fasta",
			Format:   "fasta",
			URI:      "https://www.rcsb.test/fasta/entry/5AMF",
			Contents: contents,
		},
	}
	extractor := NewArtifactExtractor(client)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "fasta"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf.fasta", artifact.Filename)
	assert.Equal(t, "fasta", artifact.Format)
	assert.Equal(t, "https://www.rcsb.test/fasta/entry/5AMF", artifact.URI)
	assert.Equal(t, []map[string]string{
		{"header": "5amf A", "sequence": "ACDE"},
		{"header": "5amf B", "sequence": "FG"},
	}, artifact.Metadata["records"])
	assert.Equal(t, []string{"5AMF"}, client.fastaPDBIDs)
}

func Test_should_return_error_when_rcsb_artifact_source_is_unsupported(t *testing.T) {
	// given
	extractor := NewArtifactExtractor(&fakeClient{})

	// when
	_, _, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "unsupported"}},
	})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported RCSB artifact source")
}

func Test_should_return_no_artifact_when_rcsb_file_is_not_found(t *testing.T) {
	// given
	extractor := NewArtifactExtractor(&fakeClient{fileError: rcsbclient.ErrNotFound})

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "1JT1", manifest.Artifact{
		Source: manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", File: "{{ pdb_id }}.cif"}},
	})

	// then
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, artifact)
}
