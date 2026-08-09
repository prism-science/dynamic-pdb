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
		case "/fasta/entry/5AMF":
			writeText(t, w, ">5amf\nACDE\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL), WithCDNBaseURL(server.URL))

	// when
	coordinates, err := client.GetCoordinates(context.Background(), "5amf")
	require.NoError(t, err)
	structureFactors, err := client.GetStructureFactors(context.Background(), "5amf")
	require.NoError(t, err)
	previewImage, err := client.GetPreviewImage(context.Background(), "5amf")
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
	assert.Empty(t, structureFactors.Contents)
	assert.Equal(t, "5amf_assembly-1.jpeg", previewImage.Filename)
	assert.Equal(t, "image", previewImage.Format)
	assert.Equal(t, server.URL+"/images/structures/am/5amf/5amf_assembly-1.jpeg", previewImage.URI)
	assert.Empty(t, previewImage.Contents)
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
	require.NotNil(t, entry.Struct)
	assert.Equal(t, "example structure", entry.Struct.Title)
	require.Len(t, entry.Experiments, 1)
	assert.Equal(t, "X-RAY DIFFRACTION", entry.Experiments[0].Method)
	require.NotNil(t, entry.EntryInfo)
	assert.Equal(t, []float64{1.5}, entry.EntryInfo.ResolutionCombined)
	require.NotNil(t, entry.EntryInfo.DepositedAtomCount)
	assert.Equal(t, 1383, *entry.EntryInfo.DepositedAtomCount)
	require.NotNil(t, entry.EntryInfo.DepositedModeledPolymerMonomerCount)
	assert.Equal(t, 164, *entry.EntryInfo.DepositedModeledPolymerMonomerCount)
	require.NotNil(t, entry.EntryInfo.DepositedPolymerEntityInstanceCount)
	assert.Equal(t, 1, *entry.EntryInfo.DepositedPolymerEntityInstanceCount)
	assert.Equal(t, []string{"ATP", "HOH", "ZN", "ACY"}, entry.EntryInfo.NonpolymerBoundComponents)
	require.NotNil(t, entry.Symmetry)
	assert.Equal(t, "P 21 21 21", entry.Symmetry.SpaceGroupNameHM)
	require.NotNil(t, entry.EntryContainerIdentifiers)
	assert.Equal(t, []string{"1", "2"}, entry.EntryContainerIdentifiers.PolymerEntityIDs)
	assert.Equal(t, []EntitySourceOrganism{
		{NCBIScientificName: "Homo sapiens"},
		{NCBIScientificName: "Escherichia coli"},
	}, polymerEntity.SourceOrganisms)
	assert.Equal(t, []AuditAuthor{{Name: "Nelson, R."}, {Name: "Sawaya, M.R."}}, entry.AuditAuthors)
	require.NotNil(t, entry.PrimaryCitation)
	assert.Equal(t, []string{"Citation, A."}, entry.PrimaryCitation.Authors)
	require.NotNil(t, entry.PubMed)
	assert.Equal(t, []string{"Howard Hughes Medical Institute, UCLA, USA."}, entry.PubMed.Affiliations)
	require.Len(t, entry.Refinements, 1)
	require.NotNil(t, entry.Refinements[0].LSRFactorRFree)
	assert.Equal(t, 0.21, *entry.Refinements[0].LSRFactorRFree)
	require.NotNil(t, entry.Refinements[0].LSRFactorRWork)
	assert.Equal(t, 0.18, *entry.Refinements[0].LSRFactorRWork)
}

func writeText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	_, err := fmt.Fprint(w, text)
	require.NoError(t, err)
}
