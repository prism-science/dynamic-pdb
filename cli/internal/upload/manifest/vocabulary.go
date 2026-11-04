package manifest

// The closed vocabularies below are the enums of backend/api/openapi.yaml that the
// API checks in enforce mode. vocabulary_test.go fails when they drift apart.

// MetricKeys are the metric keys the API accepts (MetricKey).
var MetricKeys = []string{
	"r_work",
	"r_free",
	"cc",
	"rscc",
	"clashscore",
	"ramachandran_outliers",
	"side_chain_outliers",
	"molprobity_score",
	"wilson_b",
	"rmsf",
}

// ArtifactLevels are the artifact levels the API accepts (ArtifactLevel).
var ArtifactLevels = []string{"L0", "L1", "L2", "L3"}

// ArtifactTypes are the artifact types the API accepts (ArtifactType).
var ArtifactTypes = []string{"model", "structure_factors", "fasta", "other"}

// ModelTypes are the model types the API accepts (StructureModelType).
var ModelTypes = []string{"Single Conformer", "Multiconformer", "Ensemble", ModelTypeDeposited}

// ModelPurposes are the model purposes the API accepts (ModelPurpose).
var ModelPurposes = []string{"Model Building", "Refinement", "Reference"}

// ModelTypeDeposited is the only model type the API treats as not computed: a
// Deposited model needs no run, and its metrics need not name their software.
const ModelTypeDeposited = "Deposited"
