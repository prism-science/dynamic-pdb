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
	Name         string             `yaml:"name"`
	Metadata     EntryMetadata      `yaml:"metadata,omitempty"`
	PreviewImage *EntryPreviewImage `yaml:"preview_image,omitempty"`
	Artifacts    []Artifact         `yaml:"artifacts,omitempty"`
	Models       []ModelPattern     `yaml:"models"`
}

type EntryMetadata map[string]FieldExtraction

type EntryPreviewImage struct {
	Source Source `yaml:"source"`
}

type Artifact struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name,omitempty"`
	Source Source `yaml:"source"`
	Level  string `yaml:"level,omitempty"`
}

type ModelPattern struct {
	ID        string     `yaml:"id"`
	Name      string     `yaml:"name"`
	ModelType string     `yaml:"model_type"`
	Purpose   string     `yaml:"purpose"`
	Artifacts []Artifact `yaml:"artifacts"`
	Metrics   Metrics    `yaml:"metrics"`
}

type Metrics map[string]FieldExtraction

type FieldExtraction struct {
	Source  Source  `yaml:"source"`
	Extract Extract `yaml:"extract"`
}

type Extract struct {
	JSON  *ExtractRule `yaml:"json,omitempty"`
	CSV   *ExtractRule `yaml:"csv,omitempty"`
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
