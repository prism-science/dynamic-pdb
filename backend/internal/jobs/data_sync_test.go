package jobs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

func Test_should_create_data_sync_job_when_database_passed(t *testing.T) {
	// given
	database := &db.DB{}
	rcsbClient := rcsb.NewClient()
	siftsClient := sifts.NewClient()

	// when
	job, err := NewDataSyncJob(database, rcsbClient, siftsClient, nil, validation.ModeLogOnly)

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, database, job.database)
	assert.NotNil(t, job.logger)
	require.Len(t, job.strategies, 2)
	entryStrategy, ok := job.strategies[0].(*entrySyncStrategy)
	require.True(t, ok)
	assert.Equal(t, database, entryStrategy.database)
	assert.Equal(t, rcsbClient, entryStrategy.rcsbClient)
	assert.Equal(t, siftsClient, entryStrategy.siftsClient)
	modelStrategy, ok := job.strategies[1].(*modelSyncStrategy)
	require.True(t, ok)
	assert.Equal(t, database, modelStrategy.database)
	assert.Equal(t, rcsbClient, modelStrategy.rcsbClient)
}

func Test_should_reject_data_sync_job_when_database_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(nil, rcsb.NewClient(), sifts.NewClient(), nil, validation.ModeLogOnly)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "database is nil")
	assert.Nil(t, job)
}

func Test_should_reject_data_sync_job_when_rcsb_client_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, nil, sifts.NewClient(), nil, validation.ModeLogOnly)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "RCSB client is nil")
	assert.Nil(t, job)
}

func Test_should_reject_data_sync_job_when_sifts_client_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, rcsb.NewClient(), nil, nil, validation.ModeLogOnly)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "SIFTS client is nil")
	assert.Nil(t, job)
}

func Test_should_schedule_next_data_sync_between_seven_and_fourteen_days(t *testing.T) {
	// given
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)

	// when
	scheduledAt := nextDataSyncScheduledAt(now)

	// then
	assert.GreaterOrEqual(t, scheduledAt, now.Add(dataSyncMinimumInterval))
	assert.Less(t, scheduledAt, now.Add(dataSyncMinimumInterval+dataSyncScheduleJitter))
}

func Test_should_apply_rcsb_details_to_entry_revision_copy(t *testing.T) {
	// given
	originalTitle := "old title"
	original := models.EntryRevision{
		Title: &originalTitle,
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{models.EntrySourcePDB: "5AMF"},
		},
	}
	updated := original
	details := rcsb.EntryDetails{
		Structure:   rcsb.EntryStructure{Title: "Example structure", Details: "Entry details"},
		Experiments: []rcsb.EntryExperiment{{Method: "X-RAY DIFFRACTION"}},
		Crystals:    []rcsb.EntryCrystal{{ID: "1"}},
		CrystalGrowth: []rcsb.EntryCrystalGrowth{{
			CrystalID:         "1",
			PH:                new(7.5),
			TemperatureKelvin: new(293.0),
		}},
		Diffractions: []rcsb.EntryDiffraction{{
			ID:                "1",
			CrystalID:         "1",
			TemperatureKelvin: new(100.0),
		}},
		Info:     rcsb.EntryInfo{CombinedResolution: []float64{1.5}},
		Symmetry: rcsb.EntrySymmetry{SpaceGroup: "P 21 21 21"},
	}

	// when
	applyRCSBEntryDetails(&updated, details)

	// then
	require.NotNil(t, updated.Title)
	assert.Equal(t, "Example structure", *updated.Title)
	assert.Equal(t, optionalString("Entry details"), updated.Metadata.Details)
	require.NotNil(t, updated.Metadata.Resolution)
	assert.Equal(t, 1.5, *updated.Metadata.Resolution)
	require.NotNil(t, updated.Metadata.Method)
	assert.Equal(t, models.StructureMethodXRayCrystallography, *updated.Metadata.Method)
	require.NotNil(t, updated.Metadata.SpaceGroup)
	assert.Equal(t, "P 21 21 21", *updated.Metadata.SpaceGroup)
	assert.Equal(t, &models.EntryCrystallography{Crystals: []models.EntryCrystal{{
		ID: "1",
		Growth: &models.EntryCrystalGrowth{
			PH:                new(7.5),
			TemperatureKelvin: new(293.0),
		},
		Diffractions: []models.EntryDiffraction{{
			ID:                "1",
			TemperatureKelvin: new(100.0),
		}},
	}}}, updated.Metadata.Crystallography)
	assert.Equal(t, "old title", *original.Title)
	assert.Equal(t, "5AMF", updated.Metadata.ExternalRefs[models.EntrySourcePDB])
	assert.False(t, original.HasSameData(updated))
}

func Test_should_clear_rcsb_fields_when_details_are_empty(t *testing.T) {
	// given
	title := "old title"
	resolution := 1.5
	method := models.StructureMethodCryoEM
	spaceGroup := "P 21 21 21"
	details := "old details"
	revision := models.EntryRevision{
		Title: &title,
		Metadata: models.EntryMetadata{
			Details:    &details,
			Resolution: &resolution,
			Method:     &method,
			SpaceGroup: &spaceGroup,
			Crystallography: &models.EntryCrystallography{Crystals: []models.EntryCrystal{{
				ID: "1",
			}}},
		},
	}

	// when
	applyRCSBEntryDetails(&revision, rcsb.EntryDetails{})

	// then
	assert.Nil(t, revision.Title)
	assert.Nil(t, revision.Metadata.Details)
	assert.Nil(t, revision.Metadata.Resolution)
	assert.Nil(t, revision.Metadata.Method)
	assert.Nil(t, revision.Metadata.SpaceGroup)
	assert.Nil(t, revision.Metadata.Crystallography)
}

func Test_should_apply_rcsb_details_to_model_revision_copy(t *testing.T) {
	// given
	purpose := models.ModelPurposeRefinement
	modelType := models.StructureModelTypeSingleConformer
	original := models.ModelRevision{Metadata: models.ModelMetadata{
		ExternalRefs: map[models.ModelSource]string{models.ModelSourcePDB: "5AMF"},
		Purpose:      &purpose,
		ModelType:    &modelType,
	}}
	updated := original
	details := rcsb.EntryDetails{
		Structure: rcsb.EntryStructure{ModelDetails: " Deposited model details "},
		Authors:   []rcsb.EntryAuthor{{Name: " Nelson, R. "}, {Name: "Sawaya, M.R."}},
		Publication: rcsb.EntryPublication{
			Affiliations: []string{" Howard Hughes Medical Institute ", "UCLA"},
		},
		Info: rcsb.EntryInfo{
			DepositedAtomCount:                    new(1383),
			DepositedPolymerMonomerCount:          new(200),
			DepositedModeledPolymerMonomerCount:   new(164),
			DepositedUnmodeledPolymerMonomerCount: new(36),
			DepositedPolymerEntityInstanceCount:   new(1),
			NonpolymerBoundComponents:             []string{"ATP", " HOH ", "ZN"},
		},
	}

	// when
	applyRCSBModelDetails(&updated, details)

	// then
	assert.Equal(t, []string{"Nelson, R.", "Sawaya, M.R."}, updated.Metadata.Authors)
	assert.Equal(t, optionalString("Deposited model details"), updated.Metadata.Details)
	assert.Equal(t, optionalString("Howard Hughes Medical Institute; UCLA"), updated.Metadata.Affiliation)
	assert.Equal(t, new(1383), updated.Metadata.AtomCount)
	assert.Equal(t, new(164), updated.Metadata.ModeledResidues)
	assert.Equal(t, new(1), updated.Metadata.UniqueProteinChains)
	assert.Equal(t, new(0.18), updated.Metadata.UnmodeledFraction)
	assert.Equal(t, []string{"ATP", "ZN"}, updated.Metadata.Ligands)
	assert.Equal(t, "5AMF", updated.Metadata.ExternalRefs[models.ModelSourcePDB])
	assert.Equal(t, &purpose, updated.Metadata.Purpose)
	assert.Equal(t, &modelType, updated.Metadata.ModelType)
	assert.Empty(t, original.Metadata.Authors)
}

func Test_should_clear_rcsb_model_fields_when_details_are_empty(t *testing.T) {
	// given
	revision := models.ModelRevision{Metadata: models.ModelMetadata{
		Details:           optionalString("old details"),
		UnmodeledFraction: new(0.25),
	}}

	// when
	applyRCSBModelDetails(&revision, rcsb.EntryDetails{})

	// then
	assert.Nil(t, revision.Metadata.Details)
	assert.Nil(t, revision.Metadata.UnmodeledFraction)
}

func Test_should_map_rcsb_model_metrics(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		Refinements: []rcsb.EntryRefinement{{
			RFree: new(0.21),
			RWork: new(0.18),
		}},
		ValidationGeometry: []rcsb.EntryValidationGeometry{{
			Clashscore:                  new(4.8),
			RamachandranOutliersPercent: new(0.13),
		}},
	}

	// when
	metrics := modelMetricsFromRCSB(details)

	// then
	pipeline := new(archive.ValidationPipelineSoftware)
	assert.Equal(t, []models.Metric{
		{Key: models.MetricKeyClashscore, Value: 4.8, SoftwareName: pipeline},
		{Key: models.MetricKeyRFree, Value: 0.21},
		{Key: models.MetricKeyRWork, Value: 0.18},
		{Key: models.MetricKeyRamachandranOutliers, Value: 0.13, SoftwareName: pipeline},
	}, metrics)
}

func Test_should_fill_software_only_where_unchanged_rcsb_metric_has_none_when_sync_compares_metrics(t *testing.T) {
	// given
	unnamedRFree, namedRWork, unnamedClashscore := uuid.New(), uuid.New(), uuid.New()
	currentMetrics := []models.Metric{
		{ID: namedRWork, Key: models.MetricKeyRWork, Value: 0.18, SoftwareName: new("REFMAC")},
		{ID: unnamedRFree, Key: models.MetricKeyRFree, Value: 0.21},
		{ID: uuid.New(), Key: "custom_score", Value: 12},
		{ID: unnamedClashscore, Key: models.MetricKeyClashscore, Value: 3.1},
	}
	desiredMetrics := []models.Metric{
		{Key: models.MetricKeyClashscore, Value: 3.1, SoftwareName: new(archive.ValidationPipelineSoftware)},
		{Key: models.MetricKeyRFree, Value: 0.21, SoftwareName: new("PHENIX"), SoftwareVersion: new("1.21")},
		{Key: models.MetricKeyRWork, Value: 0.18, SoftwareName: new("PHENIX"), SoftwareVersion: new("1.21")},
	}

	// when
	fills := metricSoftwareFills(currentMetrics, desiredMetrics)

	// then
	assert.ElementsMatch(t, []metricSoftwareFill{
		{metricID: unnamedClashscore, softwareName: archive.ValidationPipelineSoftware},
		{metricID: unnamedRFree, softwareName: "PHENIX", softwareVersion: new("1.21")},
	}, fills)
}

func Test_should_accept_archive_metric_without_software_when_rcsb_names_no_program(t *testing.T) {
	// given: 4HHB lists no refinement program, so its r_work names no software.
	metrics := []models.Metric{{Key: models.MetricKeyRWork, Value: 0.135}}

	// when
	fieldErrors := modelUpdateErrors(models.ModelRevision{}, metrics)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_detect_model_update_when_rcsb_fields_or_metrics_changed(t *testing.T) {
	// given
	currentRevision := models.ModelRevision{Metadata: models.ModelMetadata{AtomCount: new(100)}}
	desiredRevision := models.ModelRevision{Metadata: models.ModelMetadata{AtomCount: new(101)}}
	currentMetrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.22},
		{Key: "custom_score", Value: 12},
	}
	desiredMetrics := []models.Metric{{Key: models.MetricKeyRFree, Value: 0.21}}

	// when
	modelChanged, metricsChanged := compareModelUpdate(
		currentRevision,
		desiredRevision,
		currentMetrics,
		desiredMetrics,
	)

	// then
	assert.True(t, modelChanged)
	assert.True(t, metricsChanged)
}

func Test_should_ignore_non_rcsb_metrics_when_comparing_model_update(t *testing.T) {
	// given
	revision := models.ModelRevision{}
	currentMetrics := []models.Metric{{Key: "custom_score", Value: 12}}

	// when
	modelChanged, metricsChanged := compareModelUpdate(
		revision,
		revision,
		currentMetrics,
		nil,
	)

	// then
	assert.False(t, modelChanged)
	assert.False(t, metricsChanged)
}

func Test_should_detect_model_update_when_residue_data_changed(t *testing.T) {
	// given
	currentRevision := models.ModelRevision{Metadata: models.ModelMetadata{ResidueData: []models.ResidueData{{
		LabelAsymID: "A", LabelSeqID: 1, BIso: new(18.4),
	}}}}
	desiredRevision := models.ModelRevision{Metadata: models.ModelMetadata{ResidueData: []models.ResidueData{{
		LabelAsymID: "A", LabelSeqID: 1, BIso: new(20.1),
	}}}}

	// when
	modelChanged, metricsChanged := compareModelUpdate(currentRevision, desiredRevision, nil, nil)

	// then
	assert.True(t, modelChanged)
	assert.False(t, metricsChanged)
}

func Test_should_group_rcsb_crystallography_by_crystal(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		Crystals: []rcsb.EntryCrystal{{ID: " 2 "}, {ID: "1"}},
		CrystalGrowth: []rcsb.EntryCrystalGrowth{
			{CrystalID: "1", PH: new(6.5)},
			{CrystalID: "2", TemperatureKelvin: new(293.0)},
		},
		Diffractions: []rcsb.EntryDiffraction{
			{ID: "2", CrystalID: "1", TemperatureKelvin: new(120.0)},
			{ID: "1", CrystalID: "1", TemperatureKelvin: new(100.0)},
		},
	}

	// when
	var revision models.EntryRevision
	applyRCSBEntryDetails(&revision, details)

	// then
	assert.Equal(t, &models.EntryCrystallography{Crystals: []models.EntryCrystal{
		{
			ID:     "1",
			Growth: &models.EntryCrystalGrowth{PH: new(6.5)},
			Diffractions: []models.EntryDiffraction{
				{ID: "1", TemperatureKelvin: new(100.0)},
				{ID: "2", TemperatureKelvin: new(120.0)},
			},
		},
		{
			ID:     "2",
			Growth: &models.EntryCrystalGrowth{TemperatureKelvin: new(293.0)},
		},
	}}, revision.Metadata.Crystallography)
}

func Test_should_detect_entry_update_when_crystallography_changed(t *testing.T) {
	// given
	current := models.EntryRevision{Metadata: models.EntryMetadata{
		Crystallography: &models.EntryCrystallography{Crystals: []models.EntryCrystal{{
			ID: "1",
			Diffractions: []models.EntryDiffraction{{
				ID:                "1",
				TemperatureKelvin: new(100.0),
			}},
		}}},
	}}
	desired := current
	desired.Metadata.Crystallography = &models.EntryCrystallography{Crystals: []models.EntryCrystal{{
		ID: "1",
		Diffractions: []models.EntryDiffraction{{
			ID:                "1",
			TemperatureKelvin: new(120.0),
		}},
	}}}

	// when
	entryChanged, polymerEntitiesChanged := compareEntryUpdate(current, desired, nil, nil)

	// then
	assert.True(t, entryChanged)
	assert.False(t, polymerEntitiesChanged)
}

func Test_should_map_supported_rcsb_polymer_entity_fields(t *testing.T) {
	// given
	release := "2026_03"
	humanTaxonomyID := 9606
	eColiTaxonomyID := 562
	details := rcsb.PolymerEntityDetails{
		Entity: rcsb.PolymerEntityData{
			Description: " Spike glycoprotein ",
			Fragment:    " receptor-binding domain ",
			Mutation:    " N501Y ",
		},
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{
			EntityID: " 1 ",
			ReferenceSequenceIdentifiers: []rcsb.ReferenceSequenceIdentifier{
				{DatabaseAccession: " P0DTC2 ", DatabaseName: "UniProt", ProvenanceSource: "SIFTS"},
				{DatabaseAccession: " Q9BYF1 ", DatabaseName: "UniProt", ProvenanceSource: "PDB"},
				{DatabaseAccession: "ignored", DatabaseName: "UniProt", ProvenanceSource: "RCSB"},
				{DatabaseAccession: "ignored", DatabaseName: "GenBank", ProvenanceSource: "PDB"},
			},
		},
		SourceOrganisms: []rcsb.SourceOrganism{
			{ScientificName: "Homo sapiens", NCBITaxonomyID: &humanTaxonomyID},
			{ScientificName: "Escherichia coli", NCBITaxonomyID: &eColiTaxonomyID},
		},
	}

	// when
	metadata := polymerEntityMetadataFromRCSB(details, "fallback", &release)

	// then
	assert.Equal(t, optionalString("1"), metadata.LabelEntityID)
	assert.Equal(t, optionalString("Spike glycoprotein"), metadata.Description)
	assert.Equal(t, optionalString("receptor-binding domain"), metadata.Construct)
	assert.Equal(t, []string{"N501Y"}, metadata.Mutations)
	assert.Equal(t, []models.PolymerEntityOrganism{
		{ScientificName: "Escherichia coli", NCBITaxonomyID: &eColiTaxonomyID},
		{ScientificName: "Homo sapiens", NCBITaxonomyID: &humanTaxonomyID},
	}, metadata.SourceOrganisms)
	assert.Equal(t, []models.PolymerEntityUniProtReference{
		{Accession: "P0DTC2", Source: models.UniProtReferenceSourceSIFTS, UniProtRelease: &release},
		{Accession: "Q9BYF1", Source: models.UniProtReferenceSourceStructRef},
	}, metadata.UniProtMappings)
}

func Test_should_map_rcsb_instance_metrics_to_residue_data(t *testing.T) {
	// given
	entity := rcsb.PolymerEntityDetails{
		Polymer: rcsb.PolymerData{Sequence: "AIL", Type: "polypeptide(L)"},
		Alignments: []rcsb.PolymerEntityAlignment{
			{
				ProvenanceSource:           "PDB",
				ReferenceDatabaseName:      "UniProt",
				ReferenceDatabaseAccession: "IGNORED",
				AlignedRegions: []rcsb.PolymerEntityAlignmentRegion{{
					EntityBeginSequenceID: 2, ReferenceBeginSequenceID: 500, Length: 2,
				}},
			},
			{
				ProvenanceSource:           "SIFTS",
				ReferenceDatabaseName:      "UniProt",
				ReferenceDatabaseAccession: "P69441",
				AlignedRegions: []rcsb.PolymerEntityAlignmentRegion{{
					EntityBeginSequenceID: 2, ReferenceBeginSequenceID: 52, Length: 2,
				}},
			},
		},
	}
	instance := rcsb.PolymerEntityInstanceDetails{
		Identifiers: rcsb.PolymerEntityInstanceContainerIdentifiers{
			AsymID: "A", AuthAsymID: "X", AuthToEntityPolySeqMapping: []string{"51", "52A", "53"},
		},
		SequenceScheme: []rcsb.PolymerSequenceScheme{
			{AsymID: "A", SequenceID: 2, MonomerID: "ILE", AuthSeqNum: new(52), PDBStrandID: "X", PDBInsCode: "A"},
		},
		Features: []rcsb.PolymerInstanceFeature{
			{Type: "RSCC", Positions: []rcsb.PolymerInstanceFeaturePosition{{
				BeginSequenceID: 2, BeginComponentID: "ILE", Values: []*float64{new(0.97), new(0.95)},
			}}},
			{Type: "OWAB", Positions: []rcsb.PolymerInstanceFeaturePosition{{
				BeginSequenceID: 2, BeginComponentID: "ILE", Values: []*float64{new(18.4), new(20.1)},
			}}},
			{Type: "AVERAGE_OCCUPANCY", Positions: []rcsb.PolymerInstanceFeaturePosition{{
				BeginSequenceID: 2, BeginComponentID: "ILE", Values: []*float64{new(0.6), new(1.0)},
			}}},
			{Type: "HELIX_P", Positions: []rcsb.PolymerInstanceFeaturePosition{{
				BeginSequenceID: 2, Values: []*float64{new(1.0)},
			}}},
		},
	}

	// when
	residues := residueDataFromRCSB(entity, instance, "fallback")

	// then
	assert.Equal(t, []models.ResidueData{
		{
			LabelAsymID: "A", LabelSeqID: 2, LabelCompID: "ILE", AuthAsymID: new("X"),
			AuthSeqID: new(52), PDBxPDBInsCode: new("A"), UniProtPosition: new("P69441:52"),
			RSCC: new(0.97), BIso: new(18.4), Occupancy: new(0.6),
		},
		{
			LabelAsymID: "A", LabelSeqID: 3, LabelCompID: "LEU", AuthAsymID: new("X"),
			AuthSeqID: new(53), UniProtPosition: new("P69441:53"),
			RSCC: new(0.95), BIso: new(20.1), Occupancy: new(1.0),
		},
	}, residues)
}

func Test_should_match_polymer_entity_to_rcsb_fasta_record(t *testing.T) {
	// given
	expectedID := uuid.New()
	sequences := []models.ProteinSequence{
		{ID: uuid.New(), Header: "6M0J_2|Chain B", Sequence: "TTTT"},
		{ID: expectedID, Header: "6M0J_1|Chain A", Sequence: "AC DE"},
	}

	// when
	sequence, err := proteinSequenceForPolymerEntity("6m0j", "1", "acde", sequences)

	// then
	require.NoError(t, err)
	assert.Equal(t, expectedID, sequence.ID)
}

func Test_should_reject_polymer_entity_when_rcsb_sequence_changed(t *testing.T) {
	// given
	sequences := []models.ProteinSequence{
		{ID: uuid.New(), Header: "6M0J_1|Chain A", Sequence: "ACDE"},
	}

	// when
	sequence, err := proteinSequenceForPolymerEntity("6M0J", "1", "TTTT", sequences)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "does not match RCSB")
	assert.Nil(t, sequence)
}

func Test_should_detect_polymer_entity_metadata_change(t *testing.T) {
	// given
	sequenceID := uuid.New()
	entityID := "1"
	oldDescription := "old description"
	newDescription := "new description"
	stored := []models.PolymerEntity{{
		ProteinSequenceID: sequenceID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID: &entityID,
			Description:   &oldDescription,
		},
	}}
	desired := []polymerEntitySnapshot{{
		ProteinSequenceID: sequenceID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID: &entityID,
			Description:   &newDescription,
		},
	}}

	// when
	same := polymerEntitiesHaveSameData(stored, desired)

	// then
	assert.False(t, same)
}

func Test_should_prefer_depositor_construct_and_mutations_when_rcsb_extraction_has_values(t *testing.T) {
	// given
	extracted := []models.PolymerEntity{{Metadata: models.PolymerEntityMetadata{
		LabelEntityID: optionalString("1"),
		Construct:     optionalString("rcsb fragment"),
		Mutations:     []string{"A1B"},
	}}}
	depositor := []models.DepositorPolymerEntity{{
		LabelEntityID: "1",
		Construct:     optionalString("depositor construct"),
		Mutations:     []string{"C54T", "C97A"},
	}}

	// when
	merged := applyDepositorPolymerEntities(extracted, depositor)

	// then
	assert.Equal(t, optionalString("depositor construct"), merged[0].Metadata.Construct)
	assert.Equal(t, []string{"C54T", "C97A"}, merged[0].Metadata.Mutations)
	assert.Equal(t, optionalString("rcsb fragment"), extracted[0].Metadata.Construct)
}

func Test_should_use_depositor_organism_only_when_rcsb_extraction_finds_none(t *testing.T) {
	// given
	rcsbOrganism := models.PolymerEntityOrganism{ScientificName: "Homo sapiens", NCBITaxonomyID: new(9606)}
	extracted := []models.PolymerEntity{
		{Metadata: models.PolymerEntityMetadata{LabelEntityID: optionalString("1")}},
		{Metadata: models.PolymerEntityMetadata{
			LabelEntityID:   optionalString("2"),
			SourceOrganisms: []models.PolymerEntityOrganism{rcsbOrganism},
		}},
	}
	depositorOrganism := models.PolymerEntityOrganism{ScientificName: "Escherichia virus T4"}
	depositor := []models.DepositorPolymerEntity{
		{LabelEntityID: "1", SourceOrganisms: []models.PolymerEntityOrganism{depositorOrganism}},
		{LabelEntityID: "2", SourceOrganisms: []models.PolymerEntityOrganism{depositorOrganism}},
		{LabelEntityID: "9", Construct: optionalString("matches no entity")},
	}

	// when
	merged := applyDepositorPolymerEntities(extracted, depositor)

	// then
	assert.Equal(t, []models.PolymerEntityOrganism{depositorOrganism}, merged[0].Metadata.SourceOrganisms)
	assert.Equal(t, []models.PolymerEntityOrganism{rcsbOrganism}, merged[1].Metadata.SourceOrganisms)
	assert.Len(t, merged, 2)
}

func Test_should_not_detect_polymer_entity_change_when_stored_metadata_was_structured_by_migration(t *testing.T) {
	// given
	sequenceID := uuid.New()
	cases := map[string]struct {
		stored   string
		mutation string
	}{
		"parsed string":       {`{"label_entity_id":"1","mutations":["C54T","C97A"],"mutations_raw":"C54T, C97A","mutations_status":"parsed"}`, "C54T, C97A"},
		"needs review":        {`{"label_entity_id":"1","mutations":[],"mutations_raw":"ENGINEERED MUTATION","mutations_status":"needs_review"}`, "ENGINEERED MUTATION"},
		"migrated list":       {`{"label_entity_id":"1","mutations":["C54T"],"mutations_raw":"C54T, ENGINEERED MUTATION","mutations_status":"needs_review"}`, "C54T, ENGINEERED MUTATION"},
		"other separators":    {`{"label_entity_id":"1","mutations":["C54T","K7R"],"mutations_raw":"C54T, DELETION, K7R","mutations_status":"needs_review"}`, "C54T;DELETION ,K7R"},
		"blank stored list":   {`{"label_entity_id":"1","mutations_raw":"","mutations_status":"none"}`, ""},
		"mixed":               {`{"label_entity_id":"1","mutations":["C54T","K7R"],"mutations_raw":"C54T;DELETION,A(-3)G, K7R","mutations_status":"needs_review"}`, "C54T;DELETION,A(-3)G, K7R"},
		"list without raw":    {`{"label_entity_id":"1","mutations":["C54T","C97A"],"mutations_status":"parsed"}`, "C54T, C97A"},
		"no mutations at all": {`{"label_entity_id":"1"}`, ""},
	}

	for name, testCase := range cases {
		var stored models.PolymerEntityMetadata
		require.NoError(t, json.Unmarshal([]byte(testCase.stored), &stored), name)
		desired := polymerEntityMetadataFromRCSB(rcsb.PolymerEntityDetails{
			Entity:      rcsb.PolymerEntityData{Mutation: testCase.mutation},
			Identifiers: rcsb.PolymerEntityContainerIdentifiers{EntityID: "1"},
		}, "fallback", nil)

		// when
		same := polymerEntitiesHaveSameData(
			[]models.PolymerEntity{{ProteinSequenceID: sequenceID, Metadata: stored}},
			[]polymerEntitySnapshot{{ProteinSequenceID: sequenceID, Metadata: desired}},
		)

		// then
		assert.True(t, same, name)
	}
}

func Test_should_detect_polymer_entity_change_when_rcsb_mutation_tokens_changed_but_separators_did_not(t *testing.T) {
	// given
	sequenceID := uuid.New()
	var stored models.PolymerEntityMetadata
	require.NoError(t, json.Unmarshal([]byte(
		`{"label_entity_id":"1","mutations":["C54T"],"mutations_raw":"C54T, ENGINEERED MUTATION","mutations_status":"needs_review"}`),
		&stored))
	desired := polymerEntityMetadataFromRCSB(rcsb.PolymerEntityDetails{
		Entity:      rcsb.PolymerEntityData{Mutation: "C54T, DELETION"},
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{EntityID: "1"},
	}, "fallback", nil)

	// when
	same := polymerEntitiesHaveSameData(
		[]models.PolymerEntity{{ProteinSequenceID: sequenceID, Metadata: stored}},
		[]polymerEntitySnapshot{{ProteinSequenceID: sequenceID, Metadata: desired}},
	)

	// then
	assert.False(t, same)
}

func Test_should_keep_non_substitution_out_of_mutations_when_depositor_supplies_one(t *testing.T) {
	// given
	extracted := []models.PolymerEntity{{Metadata: models.PolymerEntityMetadata{LabelEntityID: optionalString("1")}}}
	depositor := []models.DepositorPolymerEntity{{LabelEntityID: "1", Mutations: []string{"C54T", "ENGINEERED MUTATION"}}}

	// when
	merged := applyDepositorPolymerEntities(extracted, depositor)

	// then
	assert.Equal(t, []string{"C54T"}, merged[0].Metadata.Mutations)
	assert.Equal(t, optionalString("C54T, ENGINEERED MUTATION"), merged[0].Metadata.MutationsRaw)
	assert.Equal(t, models.MutationsStatusNeedsReview, *merged[0].Metadata.MutationsStatus)
}

func Test_should_detect_polymer_entity_change_when_rcsb_mutation_text_changed(t *testing.T) {
	// given
	sequenceID := uuid.New()
	var stored models.PolymerEntityMetadata
	require.NoError(t, json.Unmarshal([]byte(
		`{"label_entity_id":"1","mutations":[],"mutations_raw":"ENGINEERED MUTATION","mutations_status":"needs_review"}`),
		&stored))
	desired := polymerEntityMetadataFromRCSB(rcsb.PolymerEntityDetails{
		Entity:      rcsb.PolymerEntityData{Mutation: "DELETION"},
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{EntityID: "1"},
	}, "fallback", nil)

	// when
	same := polymerEntitiesHaveSameData(
		[]models.PolymerEntity{{ProteinSequenceID: sequenceID, Metadata: stored}},
		[]polymerEntitySnapshot{{ProteinSequenceID: sequenceID, Metadata: desired}},
	)

	// then
	assert.False(t, same)
}

func Test_should_keep_entry_ligands_and_cofactors_when_rcsb_details_applied(t *testing.T) {
	// given
	ligands := []string{"ZN", "ATP"}
	cofactors := []string{}
	current := models.EntryRevision{
		Title:    new("old title"),
		Metadata: models.EntryMetadata{Ligands: &ligands, Cofactors: &cofactors},
	}
	updated := current
	details := rcsb.EntryDetails{Info: rcsb.EntryInfo{CombinedResolution: []float64{1.5}}}

	// when
	applyRCSBEntryDetails(&updated, details)
	entryChanged, _ := compareEntryUpdate(current, updated, nil, nil)

	// then
	assert.True(t, entryChanged, "resolution changed")
	assert.Equal(t, &ligands, updated.Metadata.Ligands)
	assert.Equal(t, &cofactors, updated.Metadata.Cofactors)
}
