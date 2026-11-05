package manifest

const DefaultFilename = "dynamic-pdb.manifest.yaml"

type Manifest struct {
	Version  int     `yaml:"version"`
	DataRoot string  `yaml:"data_root"`
	Filter   Filter  `yaml:"filter"`
	Entries  []Entry `yaml:"entries"`
}

type Entry struct {
	PDBID        string             `yaml:"pdb_id"`
	Title        string             `yaml:"title"`
	Metadata     EntryMetadata      `yaml:"metadata,omitempty"`
	PreviewImage *EntryPreviewImage `yaml:"preview_image,omitempty"`
	Artifacts    []Artifact         `yaml:"artifacts,omitempty"`
	Models       []ModelPattern     `yaml:"models"`
}

type EntryMetadata map[string][]FieldExtraction

type EntryPreviewImage struct {
	Source Source `yaml:"source"`
}

type Artifact struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name,omitempty"`
	Source Source `yaml:"source"`
	Level  string `yaml:"level,omitempty"`
	Type   string `yaml:"type,omitempty"`
	Format string `yaml:"format,omitempty"`
}

type ModelPattern struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	ModelType string `yaml:"model_type"`
	// Origin is "archive" for an unchanged archive deposition whose ModelType describes its
	// representation.
	Origin    string        `yaml:"origin,omitempty"`
	Purpose   string        `yaml:"purpose"`
	Metadata  ModelMetadata `yaml:"metadata,omitempty"`
	Artifacts []Artifact    `yaml:"artifacts"`
	Metrics   Metrics       `yaml:"metrics"`
	// Run describes the program that produced the model. Each field it sets
	// overrides what the coordinates file header says.
	Run *Run `yaml:"run,omitempty"`
}

// Run names the program that produced a model, and which of the model's artifacts
// the program read (inputs) and wrote (outputs). Inputs and Outputs hold artifact
// ids of the same model. A list that is set replaces the default rule for its
// direction (coordinates and log_* are outputs; mtz_*, structure_factors_* and
// density files are inputs).
type Run struct {
	SoftwareName    string   `yaml:"software_name,omitempty"`
	SoftwareVersion string   `yaml:"software_version,omitempty"`
	Inputs          []string `yaml:"inputs,omitempty"`
	Outputs         []string `yaml:"outputs,omitempty"`
}

type ModelMetadata map[string][]FieldExtraction

type Metrics map[string][]FieldExtraction

type FieldExtraction struct {
	Source  Source  `yaml:"source"`
	Extract Extract `yaml:"extract"`
	// Software names the program that produced a metric value read by this
	// extraction. Only metric extractions may set it.
	Software *Software `yaml:"software,omitempty"`
}

// Software is a program and, where known, its version.
type Software struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"`
}

type Extract struct {
	JSON  *ExtractRule `yaml:"json,omitempty"`
	CSV   *ExtractRule `yaml:"csv,omitempty"`
	TSV   *ExtractRule `yaml:"tsv,omitempty"`
	PDB   *ExtractRule `yaml:"pdb,omitempty"`
	MMCIF *ExtractRule `yaml:"mmcif,omitempty"`
}

type ExtractRule struct {
	Field  string       `yaml:"field,omitempty"`
	Column string       `yaml:"column,omitempty"`
	Path   string       `yaml:"path,omitempty"`
	Value  string       `yaml:"value,omitempty"`
	Equals string       `yaml:"equals,omitempty"`
	Where  *ExtractRule `yaml:"where,omitempty"`
}

type Source struct {
	Files    []string    `yaml:"files,omitempty"`
	RCSB     *RCSBSource `yaml:"rcsb,omitempty"`
	Artifact string      `yaml:"artifact,omitempty"`
}

type RCSBSource struct {
	PDBID    string `yaml:"pdb_id"`
	Resource string `yaml:"resource,omitempty"`
	File     string `yaml:"file,omitempty"`
}

type Filter struct {
	Include []string `yaml:"include"`
	Skip    []string `yaml:"skip"`
}
