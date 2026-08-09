package rcsb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_get_remote_artifacts_from_compact_rcsb_source(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/5AMF.cif":
			writeText(t, w, `data_5amf
loop_
_software.name
_software.classification
_software.version
_software.citation_id
_software.pdbx_ordinal
REFMAC refinement 5.2.0005 ? 1
`)
		case "/download/5AMF-sf.cif":
			writeText(t, w, "structure factors\n")
		case "/download/5AMF_assembly-1.jpeg":
			writeText(t, w, "image\n")
		case "/fasta/entry/5AMF":
			writeText(t, w, ">5amf\nACDE\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL))

	// when
	coordinates, err := client.GetFile(context.Background(), "5amf", "5amf.cif")
	require.NoError(t, err)
	structureFactors, err := client.GetFile(context.Background(), "5amf", "5amf-sf.cif")
	require.NoError(t, err)
	previewImage, err := client.GetFile(context.Background(), "5amf", "5amf_assembly-1.jpeg")
	require.NoError(t, err)
	fasta, err := client.GetFASTA(context.Background(), "5amf")

	// then
	require.NoError(t, err)
	assert.Equal(t, "5amf.cif", coordinates.Filename)
	assert.Equal(t, "cif", coordinates.Format)
	assert.Equal(t, server.URL+"/download/5AMF.cif", coordinates.URI)
	assert.Contains(t, string(coordinates.Contents), "REFMAC refinement 5.2.0005")
	assert.Equal(t, "5amf-sf.cif", structureFactors.Filename)
	assert.Equal(t, "structure_factors_cif", structureFactors.Format)
	assert.Equal(t, server.URL+"/download/5AMF-sf.cif", structureFactors.URI)
	assert.Equal(t, "structure factors\n", string(structureFactors.Contents))
	assert.Equal(t, "5amf_assembly-1.jpeg", previewImage.Filename)
	assert.Equal(t, "image", previewImage.Format)
	assert.Equal(t, server.URL+"/download/5AMF_assembly-1.jpeg", previewImage.URI)
	assert.Equal(t, "image\n", string(previewImage.Contents))
	assert.Equal(t, "5amf.fasta", fasta.Filename)
	assert.Equal(t, "fasta", fasta.Format)
	assert.Equal(t, server.URL+"/fasta/entry/5AMF", fasta.URI)
}

func Test_should_get_metadata_and_metrics_from_configured_data_base_url(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/core/entry/5AMF":
			writeText(t, w, `{
				"struct": {"title": "example structure"},
				"exptl": [{"method": "X-RAY DIFFRACTION"}],
				"rcsb_entry_info": {
					"resolution_combined": [1.5],
					"deposited_atom_count": 1383,
					"deposited_modeled_polymer_monomer_count": 164,
					"deposited_polymer_entity_instance_count": 1,
					"nonpolymer_bound_components": ["ATP", "HOH", "ZN", "ACY"]
				},
				"symmetry": {"space_group_name_H_M": "P 21 21 21"},
				"rcsb_entry_container_identifiers": {"polymer_entity_ids": ["1", "2"]},
				"refine": [{"ls_R_factor_R_free": 0.21, "ls_R_factor_R_work": 0.18}],
				"audit_author": [{"name": "Nelson, R."}, {"name": "Sawaya, M.R."}],
				"rcsb_primary_citation": {"rcsb_authors": ["Citation, A."]},
				"pubmed": {"rcsb_pubmed_affiliation_info": ["Howard Hughes Medical Institute, UCLA, USA."]}
			}`)
		case "/rest/v1/core/polymer_entity/5AMF/1":
			writeText(t, w, `{
				"rcsb_entity_source_organism": [{"ncbi_scientific_name": "Homo sapiens"}]
			}`)
		case "/rest/v1/core/polymer_entity/5AMF/2":
			writeText(t, w, `{
				"rcsb_entity_source_organism": [{"ncbi_scientific_name": "Homo sapiens"}, {"ncbi_scientific_name": "Escherichia coli"}]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL))

	// when
	entry, err := client.GetEntry(context.Background(), "5amf")
	require.NoError(t, err)
	polymerEntity, err := client.GetPolymerEntity(context.Background(), "5amf", "2")
	require.NoError(t, err)

	// then
	assert.Equal(t, "example structure", entry["struct"].(map[string]any)["title"])
	assert.Equal(t, "X-RAY DIFFRACTION", entry["exptl"].([]any)[0].(map[string]any)["method"])
	entryInfo := entry["rcsb_entry_info"].(map[string]any)
	assert.Equal(t, []any{1.5}, entryInfo["resolution_combined"])
	assert.Equal(t, float64(1383), entryInfo["deposited_atom_count"])
	assert.Equal(t, float64(164), entryInfo["deposited_modeled_polymer_monomer_count"])
	assert.Equal(t, float64(1), entryInfo["deposited_polymer_entity_instance_count"])
	assert.Equal(t, []any{"ATP", "HOH", "ZN", "ACY"}, entryInfo["nonpolymer_bound_components"])
	assert.Equal(t, "P 21 21 21", entry["symmetry"].(map[string]any)["space_group_name_H_M"])
	identifiers := entry["rcsb_entry_container_identifiers"].(map[string]any)
	assert.Equal(t, []any{"1", "2"}, identifiers["polymer_entity_ids"])
	organisms := polymerEntity["rcsb_entity_source_organism"].([]any)
	assert.Equal(t, "Homo sapiens", organisms[0].(map[string]any)["ncbi_scientific_name"])
	assert.Equal(t, "Escherichia coli", organisms[1].(map[string]any)["ncbi_scientific_name"])
	assert.Equal(t, "Nelson, R.", entry["audit_author"].([]any)[0].(map[string]any)["name"])
	assert.Equal(t, []any{"Citation, A."}, entry["rcsb_primary_citation"].(map[string]any)["rcsb_authors"])
	assert.Equal(t, []any{"Howard Hughes Medical Institute, UCLA, USA."}, entry["pubmed"].(map[string]any)["rcsb_pubmed_affiliation_info"])
	refinement := entry["refine"].([]any)[0].(map[string]any)
	assert.Equal(t, 0.21, refinement["ls_R_factor_R_free"])
	assert.Equal(t, 0.18, refinement["ls_R_factor_R_work"])
}

func writeText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	_, err := fmt.Fprint(w, text)
	require.NoError(t, err)
}
