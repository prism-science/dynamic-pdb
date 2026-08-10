package rcsb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"dynamic-pdb/cli/internal/rcsb"
	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

const templatePDBID = "{{ pdb_id }}"

type ArtifactExtractor struct {
	client rcsb.Client
}

var _ extractors.ArtifactExtractor = ArtifactExtractor{}

func NewArtifactExtractor(client rcsb.Client) ArtifactExtractor {
	return ArtifactExtractor{client: client}
}

func (e ArtifactExtractor) Extract(
	ctx context.Context,
	pdbID string,
	artifact manifest.Artifact,
) (extractors.Artifact, bool, error) {
	if rcsbSourceIsEmpty(artifact.Source.RCSB) {
		return extractors.Artifact{}, false, nil
	}
	if e.client == nil {
		return extractors.Artifact{}, false, errors.New("RCSB client is required")
	}
	resolved, err := e.rcsbArtifact(ctx, pdbID, artifact)
	if err != nil {
		return extractors.Artifact{}, false, err
	}
	return resolved, true, nil
}

func (e ArtifactExtractor) rcsbArtifact(
	ctx context.Context,
	pdbID string,
	artifact manifest.Artifact,
) (extractors.Artifact, error) {
	source := artifact.Source.RCSB
	resource := strings.TrimSpace(source.Resource)
	file := strings.ReplaceAll(source.File, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
	switch {
	case resource == "fasta":
		artifact, err := e.client.GetFASTA(ctx, pdbID)
		return artifactPayload(
			artifact,
			map[string]any{"records": parseFASTA(artifact.Contents)},
		), err
	case strings.TrimSpace(file) != "":
		artifact, err := e.client.GetFile(ctx, pdbID, file)
		return artifactPayload(artifact, nil), err
	default:
		return extractors.Artifact{}, fmt.Errorf("unsupported RCSB artifact source: %s", sourceDescription(source))
	}
}

func artifactPayload(artifact rcsb.Artifact, metadata map[string]any) extractors.Artifact {
	sha := ""
	if len(artifact.Contents) > 0 {
		hash := sha256.Sum256(artifact.Contents)
		sha = hex.EncodeToString(hash[:])
	}
	return extractors.Artifact{
		Filename: artifact.Filename,
		Size:     int64(len(artifact.Contents)),
		Format:   artifact.Format,
		URI:      artifact.URI,
		SHA256:   sha,
		Metadata: metadata,
		Contents: artifact.Contents,
	}
}

func sourceDescription(source *manifest.RCSBSource) string {
	if source == nil {
		return ""
	}
	if strings.TrimSpace(source.Resource) != "" {
		return "resource " + strings.TrimSpace(source.Resource)
	}
	return "file " + strings.TrimSpace(source.File)
}

func parseFASTA(contents []byte) []map[string]string {
	records := make([]map[string]string, 0)
	var header string
	var sequence strings.Builder
	flush := func() {
		if header == "" {
			return
		}
		records = append(records, map[string]string{
			"header":   header,
			"sequence": sequence.String(),
		})
		sequence.Reset()
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			flush()
			header = strings.TrimPrefix(line, ">")
			continue
		}
		sequence.WriteString(line)
	}
	flush()
	return records
}
