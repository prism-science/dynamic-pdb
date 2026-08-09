package extractors

import (
	"context"

	"dynamic-pdb/cli/internal/upload/manifest"
)

type FieldExtractor interface {
	Extract(ctx context.Context, pdbID string, source manifest.Source, extract manifest.Extract) (any, bool, error)
}

type ArtifactExtractor interface {
	Extract(ctx context.Context, pdbID string, artifact manifest.Artifact) (Artifact, bool, error)
}

type Artifact struct {
	Filename  string
	Size      int64
	Format    string
	URI       string
	SHA256    string
	Metadata  map[string]any
	LocalPath string
	Contents  []byte
}
