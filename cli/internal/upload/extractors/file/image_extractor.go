package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

type ImageExtractor struct {
	dataRoot string
}

var _ extractors.ImageExtractor = ImageExtractor{}

func NewImageExtractor(dataRoot string) ImageExtractor {
	return ImageExtractor{dataRoot: dataRoot}
}

func (e ImageExtractor) Extract(
	_ context.Context,
	pdbID string,
	source manifest.Source,
) (extractors.Image, bool, error) {
	for _, fileSource := range source.Files {
		fileSource = strings.ReplaceAll(fileSource, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
		image, ok, err := e.localImage(fileSource)
		if err != nil || ok {
			return image, ok, err
		}
	}
	return extractors.Image{}, false, nil
}

func (e ImageExtractor) localImage(source string) (extractors.Image, bool, error) {
	if archiveSource, entryName, ok := splitZipSource(source); ok {
		return e.zipImage(archiveSource, entryName, source)
	}

	path := resolvePath(e.dataRoot, source)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return extractors.Image{}, false, nil
		}
		return extractors.Image{}, false, fmt.Errorf("stat image source %s: %w", source, err)
	}
	return extractors.Image{
		Filename:  filepath.Base(path),
		Size:      info.Size(),
		LocalPath: path,
	}, true, nil
}

func (e ImageExtractor) zipImage(archiveSource string, entryName string, source string) (extractors.Image, bool, error) {
	archivePath := resolvePath(e.dataRoot, archiveSource)
	contents, ok, err := zipContents(archivePath, entryName, source)
	if err != nil || !ok {
		return extractors.Image{}, ok, err
	}
	return extractors.Image{
		Filename:  filepath.Base(entryName),
		Size:      int64(len(contents)),
		LocalPath: archivePath + "#" + entryName,
		Contents:  contents,
	}, true, nil
}
