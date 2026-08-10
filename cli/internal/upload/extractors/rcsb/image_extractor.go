package rcsb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dynamic-pdb/cli/internal/rcsb"
	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

type ImageExtractor struct {
	client rcsb.Client
}

var _ extractors.ImageExtractor = ImageExtractor{}

func NewImageExtractor(client rcsb.Client) ImageExtractor {
	return ImageExtractor{client: client}
}

func (e ImageExtractor) Extract(
	ctx context.Context,
	pdbID string,
	source manifest.Source,
) (extractors.Image, bool, error) {
	if rcsbSourceIsEmpty(source.RCSB) {
		return extractors.Image{}, false, nil
	}
	if e.client == nil {
		return extractors.Image{}, false, errors.New("RCSB client is required")
	}
	file := strings.ReplaceAll(source.RCSB.File, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
	if strings.TrimSpace(file) == "" {
		return extractors.Image{}, false, nil
	}
	image, err := e.client.GetImage(ctx, pdbID, file)
	if err != nil {
		return extractors.Image{}, false, fmt.Errorf("get RCSB image %s: %w", file, err)
	}
	return imagePayload(image), true, nil
}

func imagePayload(image rcsb.Artifact) extractors.Image {
	return extractors.Image{
		Filename: image.Filename,
		Size:     int64(len(image.Contents)),
		Contents: image.Contents,
	}
}
