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
		case "/fasta/entry/5AMF":
			writeText(t, w, ">5amf\nACDE\n")
		case "/images/structures/am/5amf/5amf_assembly-1.jpeg":
			writeText(t, w, "image\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	coordinates, err := client.GetFile(context.Background(), "5amf", "5amf.cif")
	require.NoError(t, err)
	structureFactors, err := client.GetFile(context.Background(), "5amf", "5amf-sf.cif")
	require.NoError(t, err)
	previewImage, err := client.GetImage(context.Background(), "5amf", "5amf_assembly-1.jpeg")
	require.NoError(t, err)
	fasta, err := client.GetFASTA(context.Background(), "5amf")

	// then
	require.NoError(t, err)
	assert.Equal(t, "5amf.cif", coordinates.Filename)
	assert.Equal(t, "cif", coordinates.Format)
	assert.Equal(t, server.URL+"/download/5AMF.cif", coordinates.URI)
	assert.Empty(t, coordinates.Contents)
	assert.Equal(t, "5amf-sf.cif", structureFactors.Filename)
	assert.Equal(t, "structure_factors_cif", structureFactors.Format)
	assert.Equal(t, server.URL+"/download/5AMF-sf.cif", structureFactors.URI)
	assert.Empty(t, structureFactors.Contents)
	assert.Equal(t, "5amf_assembly-1.jpeg", previewImage.Filename)
	assert.Equal(t, "image", previewImage.Format)
	assert.Equal(t, server.URL+"/images/structures/am/5amf/5amf_assembly-1.jpeg", previewImage.URI)
	assert.Equal(t, "image\n", string(previewImage.Contents))
	assert.Equal(t, "5amf.fasta", fasta.Filename)
	assert.Equal(t, "fasta", fasta.Format)
	assert.Equal(t, server.URL+"/fasta/entry/5AMF", fasta.URI)
	assert.Equal(t, ">5amf\nACDE\n", string(fasta.Contents))
}

func Test_should_download_rcsb_file_only_when_contents_are_requested(t *testing.T) {
	// given
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/download/5AMF.cif":
			writeText(t, w, "data_5amf\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	linked, linkErr := client.GetFile(context.Background(), "5amf", "5amf.cif")
	downloaded, downloadErr := client.DownloadFile(context.Background(), "5amf", "5amf.cif")

	// then
	require.NoError(t, linkErr)
	require.NoError(t, downloadErr)
	assert.Empty(t, linked.Contents)
	assert.Equal(t, "data_5amf\n", string(downloaded.Contents))
	assert.Equal(t, 1, requests["/download/5AMF.cif"])
}

func Test_should_get_metadata_and_metrics_from_configured_data_base_url(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/core/entry/5AMF":
			writeText(t, w, `{
				"struct": {
					"title": "example structure",
					"pdbx_details": "entry details",
					"pdbx_model_details": "deposited model details"
				},
				"exptl": [{"method": "X-RAY DIFFRACTION"}],
				"exptl_crystal": [{"id": "1"}],
				"exptl_crystal_grow": [{"crystal_id": "1", "pH": 7.5, "temp": 293}],
				"diffrn": [{"id": "1", "crystal_id": "1", "ambient_temp": 100}],
				"rcsb_entry_info": {
					"resolution_combined": [1.5],
					"deposited_atom_count": 1383,
					"deposited_polymer_monomer_count": 171,
					"deposited_modeled_polymer_monomer_count": 164,
					"deposited_unmodeled_polymer_monomer_count": 7,
					"deposited_polymer_entity_instance_count": 1,
					"nonpolymer_bound_components": ["ATP", "HOH", "ZN", "ACY"]
				},
				"symmetry": {"space_group_name_H_M": "P 21 21 21"},
				"rcsb_entry_container_identifiers": {"polymer_entity_ids": ["1", "2"]},
				"refine": [{"ls_R_factor_R_free": 0.21, "ls_R_factor_R_work": 0.18}],
				"audit_author": [{"name": "Nelson, R."}, {"name": "Sawaya, M.R."}],
				"rcsb_primary_citation": {"rcsb_authors": ["Citation, A."]},
				"pubmed": {"rcsb_pubmed_affiliation_info": ["Howard Hughes Medical Institute, UCLA, USA."]},
				"pdbx_vrpt_summary_geometry": [{"clashscore": 4.8, "percent_ramachandran_outliers": 0.13}]
			}`)
		case "/rest/v1/core/polymer_entity/5AMF/1":
			writeText(t, w, `{
				"rcsb_entity_source_organism": [{"ncbi_scientific_name": "Homo sapiens"}]
			}`)
		case "/rest/v1/core/polymer_entity/5AMF/2":
			writeText(t, w, `{
				"entity_poly": {"pdbx_seq_one_letter_code_can": "ACDE"},
				"rcsb_polymer_entity": {
					"pdbx_description": "Example protein",
					"pdbx_fragment": "Catalytic domain",
					"pdbx_mutation": "A12G"
				},
				"rcsb_polymer_entity_container_identifiers": {
					"entity_id": "2",
					"reference_sequence_identifiers": [{
						"database_accession": "P12345",
						"database_name": "UniProt",
						"provenance_source": "SIFTS"
					}]
				},
				"rcsb_entity_source_organism": [
					{"ncbi_scientific_name": "Homo sapiens", "ncbi_taxonomy_id": 9606},
					{"ncbi_scientific_name": "Escherichia coli", "ncbi_taxonomy_id": 562}
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	entry, err := client.GetEntry(context.Background(), "5amf")
	require.NoError(t, err)
	polymerEntity, err := client.GetPolymerEntity(context.Background(), "5amf", "2")
	require.NoError(t, err)
	entryDetails, err := client.GetEntryDetails(context.Background(), "5amf")
	require.NoError(t, err)
	polymerEntityDetails, err := client.GetPolymerEntityDetails(context.Background(), "5amf", "2")

	// then
	require.NoError(t, err)
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
	assert.Equal(t, "example structure", entryDetails.Structure.Title)
	assert.Equal(t, "entry details", entryDetails.Structure.Details)
	assert.Equal(t, "X-RAY DIFFRACTION", entryDetails.Experiments[0].Method)
	require.Len(t, entryDetails.Crystals, 1)
	assert.Equal(t, "1", entryDetails.Crystals[0].ID)
	require.Len(t, entryDetails.CrystalGrowth, 1)
	assert.Equal(t, "1", entryDetails.CrystalGrowth[0].CrystalID)
	assert.Equal(t, 7.5, *entryDetails.CrystalGrowth[0].PH)
	assert.Equal(t, 293.0, *entryDetails.CrystalGrowth[0].TemperatureKelvin)
	require.Len(t, entryDetails.Diffractions, 1)
	assert.Equal(t, "1", entryDetails.Diffractions[0].ID)
	assert.Equal(t, "1", entryDetails.Diffractions[0].CrystalID)
	assert.Equal(t, 100.0, *entryDetails.Diffractions[0].TemperatureKelvin)
	assert.Equal(t, []float64{1.5}, entryDetails.Info.CombinedResolution)
	assert.Equal(t, "deposited model details", entryDetails.Structure.ModelDetails)
	assert.Equal(t, 1383, *entryDetails.Info.DepositedAtomCount)
	assert.Equal(t, 171, *entryDetails.Info.DepositedPolymerMonomerCount)
	assert.Equal(t, 164, *entryDetails.Info.DepositedModeledPolymerMonomerCount)
	assert.Equal(t, 7, *entryDetails.Info.DepositedUnmodeledPolymerMonomerCount)
	assert.Equal(t, 1, *entryDetails.Info.DepositedPolymerEntityInstanceCount)
	assert.Equal(t, []string{"ATP", "HOH", "ZN", "ACY"}, entryDetails.Info.NonpolymerBoundComponents)
	assert.Equal(t, "P 21 21 21", entryDetails.Symmetry.SpaceGroup)
	assert.Equal(t, []string{"1", "2"}, entryDetails.Identifiers.PolymerEntityIDs)
	assert.Equal(t, "Nelson, R.", entryDetails.Authors[0].Name)
	assert.Equal(t, []string{"Howard Hughes Medical Institute, UCLA, USA."}, entryDetails.Publication.Affiliations)
	assert.Equal(t, 0.21, *entryDetails.Refinements[0].RFree)
	assert.Equal(t, 0.18, *entryDetails.Refinements[0].RWork)
	assert.Equal(t, 4.8, *entryDetails.ValidationGeometry[0].Clashscore)
	assert.Equal(t, 0.13, *entryDetails.ValidationGeometry[0].RamachandranOutliersPercent)
	assert.Equal(t, "Homo sapiens", polymerEntityDetails.SourceOrganisms[0].ScientificName)
	assert.Equal(t, "Escherichia coli", polymerEntityDetails.SourceOrganisms[1].ScientificName)
	assert.Equal(t, 9606, *polymerEntityDetails.SourceOrganisms[0].NCBITaxonomyID)
	assert.Equal(t, "ACDE", polymerEntityDetails.Polymer.CanonicalSequence)
	assert.Equal(t, "Example protein", polymerEntityDetails.Entity.Description)
	assert.Equal(t, "Catalytic domain", polymerEntityDetails.Entity.Fragment)
	assert.Equal(t, "A12G", polymerEntityDetails.Entity.Mutation)
	assert.Equal(t, "2", polymerEntityDetails.Identifiers.EntityID)
	require.Len(t, polymerEntityDetails.Identifiers.ReferenceSequenceIdentifiers, 1)
	assert.Equal(t, "P12345", polymerEntityDetails.Identifiers.ReferenceSequenceIdentifiers[0].DatabaseAccession)
	assert.Equal(t, "UniProt", polymerEntityDetails.Identifiers.ReferenceSequenceIdentifiers[0].DatabaseName)
	assert.Equal(t, "SIFTS", polymerEntityDetails.Identifiers.ReferenceSequenceIdentifiers[0].ProvenanceSource)
}

func Test_should_cache_successful_rcsb_responses_by_url(t *testing.T) {
	// given
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/rest/v1/core/entry/5AMF":
			writeText(t, w, `{"struct": {"title": "cached entry"}}`)
		case "/rest/v1/core/polymer_entity/5AMF/1":
			writeText(t, w, `{"id": "1"}`)
		case "/download/5AMF.cif":
			writeText(t, w, "data_5amf\n")
		case "/images/structures/am/5amf/5amf_assembly-1.jpeg":
			writeText(t, w, "image\n")
		case "/fasta/entry/5AMF":
			writeText(t, w, ">5amf\nACDE\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	for range 2 {
		_, err := client.GetEntry(context.Background(), "5amf")
		require.NoError(t, err)
		_, err = client.GetPolymerEntity(context.Background(), "5amf", "1")
		require.NoError(t, err)
		_, err = client.DownloadFile(context.Background(), "5amf", "5amf.cif")
		require.NoError(t, err)
		_, err = client.GetImage(context.Background(), "5amf", "5amf_assembly-1.jpeg")
		require.NoError(t, err)
		_, err = client.GetFASTA(context.Background(), "5amf")
		require.NoError(t, err)
	}

	// then
	assert.Equal(t, 1, requests["/rest/v1/core/entry/5AMF"])
	assert.Equal(t, 1, requests["/rest/v1/core/polymer_entity/5AMF/1"])
	assert.Equal(t, 1, requests["/download/5AMF.cif"])
	assert.Equal(t, 1, requests["/images/structures/am/5amf/5amf_assembly-1.jpeg"])
	assert.Equal(t, 1, requests["/fasta/entry/5AMF"])
}

func Test_should_allow_disabling_rcsb_response_cache(t *testing.T) {
	// given
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeText(t, w, `{"struct": {"title": "uncached entry"}}`)
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL), WithCacheEntries(0))

	// when
	_, firstErr := client.GetEntry(context.Background(), "5amf")
	_, secondErr := client.GetEntry(context.Background(), "5amf")

	// then
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, 2, requests)
}

func Test_should_retry_transient_rcsb_get_errors(t *testing.T) {
	// given
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		writeText(t, w, `{"struct": {"title": "retried entry"}}`)
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	entry, err := client.GetEntry(context.Background(), "5amf")

	// then
	require.NoError(t, err)
	assert.Equal(t, "retried entry", entry["struct"].(map[string]any)["title"])
	assert.Equal(t, 2, requests)
}

func writeText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	_, err := fmt.Fprint(w, text)
	require.NoError(t, err)
}
