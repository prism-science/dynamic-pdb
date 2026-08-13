package rcsb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rcsbclient "dynamic-pdb/cli/internal/rcsb"
	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_json_field_from_rcsb_entry(t *testing.T) {
	// given
	client := &fakeClient{
		entry: map[string]any{
			"rcsb_entry_info": map[string]any{
				"resolution_combined": []any{1.8},
			},
		},
	}
	extractor := NewFieldExtractor(client)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "entry"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "rcsb_entry_info.resolution_combined[0]"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1.8, value)
	assert.Equal(t, []string{"5AMF"}, client.entryPDBIDs)
}

func Test_should_extract_json_fields_from_array_objects(t *testing.T) {
	// given
	client := &fakeClient{
		entry: map[string]any{
			"audit_author": []any{
				map[string]any{"name": "Nelson, R."},
				map[string]any{"name": "Sawaya, M.R."},
			},
		},
	}
	extractor := NewFieldExtractor(client)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "entry"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "audit_author.name"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []any{"Nelson, R.", "Sawaya, M.R."}, value)
}

func Test_should_extract_polymer_entity_values_from_all_entry_entity_ids(t *testing.T) {
	// given
	client := &fakeClient{
		entry: map[string]any{
			"rcsb_entry_container_identifiers": map[string]any{
				"polymer_entity_ids": []any{"1", "2", "3"},
			},
		},
		polymerEntities: map[string]map[string]any{
			"1": {"rcsb_entity_source_organism": []any{map[string]any{"ncbi_scientific_name": "Homo sapiens"}}},
			"2": {"rcsb_entity_source_organism": []any{map[string]any{"ncbi_scientific_name": "Homo sapiens"}}},
			"3": {"rcsb_entity_source_organism": []any{map[string]any{"ncbi_scientific_name": "Mus musculus"}}},
		},
	}
	extractor := NewFieldExtractor(client)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "polymer_entity"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "rcsb_entity_source_organism.ncbi_scientific_name"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Homo sapiens; Mus musculus", value)
	assert.Equal(t, []string{"1", "2", "3"}, client.polymerEntityIDs)
}

func Test_should_return_error_when_rcsb_field_resource_is_unsupported(t *testing.T) {
	// given
	extractor := NewFieldExtractor(&fakeClient{})

	// when
	_, _, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "unsupported"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "struct.title"}},
	)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported RCSB field resource")
}

func Test_should_return_no_field_when_rcsb_entry_is_not_found(t *testing.T) {
	// given
	extractor := NewFieldExtractor(&fakeClient{entryError: rcsbclient.ErrNotFound})

	// when
	value, ok, err := extractor.Extract(context.Background(), "1JT1",
		manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "entry"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "struct.title"}},
	)

	// then
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, value)
}

type fakeClient struct {
	entry             map[string]any
	entryError        error
	polymerEntities   map[string]map[string]any
	files             map[string]rcsbclient.Artifact
	downloadedFiles   map[string]rcsbclient.Artifact
	fileError         error
	images            map[string]rcsbclient.Artifact
	imageError        error
	fasta             rcsbclient.Artifact
	fastaError        error
	entryPDBIDs       []string
	polymerEntityIDs  []string
	fileNames         []string
	downloadFileNames []string
	imageNames        []string
	fastaPDBIDs       []string
}

func (c *fakeClient) GetEntry(_ context.Context, pdbID string) (map[string]any, error) {
	c.entryPDBIDs = append(c.entryPDBIDs, pdbID)
	if c.entryError != nil {
		return nil, c.entryError
	}
	return c.entry, nil
}

func (c *fakeClient) GetPolymerEntity(_ context.Context, _ string, entityID string) (map[string]any, error) {
	c.polymerEntityIDs = append(c.polymerEntityIDs, entityID)
	return c.polymerEntities[entityID], nil
}

func (c *fakeClient) GetFile(_ context.Context, _ string, file string) (rcsbclient.Artifact, error) {
	c.fileNames = append(c.fileNames, file)
	if c.fileError != nil {
		return rcsbclient.Artifact{}, c.fileError
	}
	return c.files[file], nil
}

func (c *fakeClient) DownloadFile(_ context.Context, _ string, file string) (rcsbclient.Artifact, error) {
	c.downloadFileNames = append(c.downloadFileNames, file)
	if c.fileError != nil {
		return rcsbclient.Artifact{}, c.fileError
	}
	return c.downloadedFiles[file], nil
}

func (c *fakeClient) GetImage(_ context.Context, _ string, file string) (rcsbclient.Artifact, error) {
	c.imageNames = append(c.imageNames, file)
	if c.imageError != nil {
		return rcsbclient.Artifact{}, c.imageError
	}
	return c.images[file], nil
}

func (c *fakeClient) GetFASTA(_ context.Context, pdbID string) (rcsbclient.Artifact, error) {
	c.fastaPDBIDs = append(c.fastaPDBIDs, pdbID)
	if c.fastaError != nil {
		return rcsbclient.Artifact{}, c.fastaError
	}
	return c.fasta, nil
}
