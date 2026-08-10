package upload

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	"dynamic-pdb/cli/internal/rcsb"
	extractorapi "dynamic-pdb/cli/internal/upload/extractors"
	artifactextractor "dynamic-pdb/cli/internal/upload/extractors/artifact"
	fileextractor "dynamic-pdb/cli/internal/upload/extractors/file"
	rcsbextractor "dynamic-pdb/cli/internal/upload/extractors/rcsb"
	"dynamic-pdb/cli/internal/upload/manifest"
)

const templatePDBID = "{{ pdb_id }}"

type Summary struct {
	Entries   int
	Models    int
	Artifacts int
	Skipped   int
	StatePath string
}

type Uploader struct {
	dynamicPDBClient dynamicpdbapi.Client
	rcsb             rcsb.Client
	progress         Progress
}

func New(dynamicPDBClient dynamicpdbapi.Client, rcsbClient rcsb.Client, progress Progress) *Uploader {
	return &Uploader{
		dynamicPDBClient: dynamicPDBClient,
		rcsb:             rcsbClient,
		progress:         progress,
	}
}

func (u *Uploader) Upload(ctx context.Context, manifestPath string) (Summary, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return Summary{}, errors.New("manifest path is required")
	}
	if u == nil {
		return Summary{}, errors.New("uploader is required")
	}
	if u.dynamicPDBClient == nil {
		return Summary{}, errors.New("dynamic PDB client is required")
	}
	if u.rcsb == nil {
		return Summary{}, errors.New("RCSB client is required")
	}

	resolvedManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Summary{}, fmt.Errorf("resolve manifest path: %w", err)
	}

	uploadingManifest, err := manifest.Read(resolvedManifestPath)
	if err != nil {
		return Summary{}, err
	}
	if strings.TrimSpace(uploadingManifest.DataRoot) == "" {
		return Summary{}, errors.New("manifest data_root is required")
	}
	entryTemplate, err := manifestEntry(uploadingManifest)
	if err != nil {
		return Summary{}, err
	}
	dataRoot := uploadingManifest.DataRoot
	if !filepath.IsAbs(dataRoot) {
		dataRoot = filepath.Join(filepath.Dir(resolvedManifestPath), dataRoot)
	}
	dataRoot, err = filepath.Abs(dataRoot)
	if err != nil {
		return Summary{}, fmt.Errorf("resolve manifest data_root: %w", err)
	}

	pdbIDs, err := discoverPDBIDs(dataRoot, entryTemplate)
	if err != nil {
		return Summary{}, err
	}
	statePath := uploadStatePath(resolvedManifestPath)
	state, err := readUploadState(statePath)
	if err != nil {
		return Summary{}, err
	}
	uploadingEntries := state.uploadingEntries()
	if len(uploadingEntries) > 0 {
		return Summary{}, fmt.Errorf("upload state has unfinished entries %s; fix the failed upload and remove those entries from %s before restarting", strings.Join(uploadingEntries, ", "), statePath)
	}

	selectedPDBIDs := filteredPDBIDs(pdbIDs, uploadingManifest.Filter)
	pendingPDBIDs := pendingPDBIDs(selectedPDBIDs, state)
	if err := u.progress.Start(len(pendingPDBIDs)); err != nil {
		return Summary{}, fmt.Errorf("start upload progress: %w", err)
	}
	result := Summary{StatePath: statePath}
	result.Skipped = len(pdbIDs) - len(pendingPDBIDs)
	for _, pdbID := range pendingPDBIDs {
		if err := state.startEntry(statePath, pdbID); err != nil {
			return result, err
		}
		existingEntry, err := u.existingEntryByPDBID(ctx, pdbID)
		if err != nil {
			return result, fmt.Errorf("check existing entry %s: %w", pdbID, err)
		}
		entryResult, err := u.uploadEntry(ctx, entryTemplate, dataRoot, pdbID, existingEntry)
		if err != nil {
			return result, fmt.Errorf("upload %s: %w", pdbID, err)
		}
		result.Entries++
		result.Models += entryResult.Models
		result.Artifacts += entryResult.Artifacts
		if err := state.completeEntry(statePath, entryResult); err != nil {
			return result, err
		}
		if err := u.progress.EntryDone(entryResult.PDBID, entryResult.EntryID, entryResult.Models, entryResult.Artifacts); err != nil {
			return result, fmt.Errorf("update upload progress: %w", err)
		}
	}
	if err := u.progress.Finish(); err != nil {
		return result, fmt.Errorf("finish upload progress: %w", err)
	}
	return result, nil
}

type entryUploadResult struct {
	PDBID          string
	EntryID        string
	CreatedEntryID string
	Models         int
	Artifacts      int
	ModelIDs       []string
	ArtifactIDs    []string
	RunIDs         []string
	MetricIDs      []string
}

func (u *Uploader) uploadEntry(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbID string,
	existingEntry *dynamicpdbapi.Entry,
) (entryUploadResult, error) {
	entryPDBID := canonicalPDBID(pdbID)
	uploadedEntry, err := u.ensureEntryUploaded(ctx, entryTemplate, dataRoot, pdbID, entryPDBID, existingEntry)
	if err != nil {
		return entryUploadResult{}, err
	}

	uploadedModels, err := u.uploadModels(ctx, entryTemplate.Models, dataRoot, pdbID, uploadedEntry.EntryID)
	if err != nil {
		return entryUploadResult{}, err
	}

	return entryUploadResult{
		PDBID:          entryPDBID,
		EntryID:        uploadedEntry.EntryID,
		CreatedEntryID: uploadedEntry.CreatedEntryID,
		Models:         uploadedModels.Count,
		Artifacts:      uploadedEntry.ArtifactCount + uploadedModels.ArtifactCount,
		ModelIDs:       uploadedModels.ModelIDs,
		ArtifactIDs:    append(uploadedEntry.ArtifactIDs, uploadedModels.ArtifactIDs...),
		RunIDs:         uploadedModels.RunIDs,
		MetricIDs:      uploadedModels.MetricIDs,
	}, nil
}

type uploadedEntry struct {
	EntryID        string
	CreatedEntryID string
	ArtifactIDs    []string
	ArtifactCount  int
}

func (u *Uploader) ensureEntryUploaded(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbID string,
	entryPDBID string,
	existingEntry *dynamicpdbapi.Entry,
) (uploadedEntry, error) {
	if existingEntry != nil {
		return uploadedEntry{EntryID: existingEntry.ID}, nil
	}

	entryID := uuid.NewString()
	metadata, err := u.entryMetadata(ctx, dataRoot, entryTemplate.Metadata, entryPDBID)
	if err != nil {
		return uploadedEntry{}, err
	}

	thumbnailImageURL, err := u.uploadPreviewImage(ctx, dataRoot, entryID, entryTemplate.PreviewImage, pdbID)
	if err != nil {
		return uploadedEntry{}, err
	}

	entryArtifacts := make([]dynamicpdbapi.CreateArtifactRequest, 0)
	uploadedArtifactIDs := make([]string, 0, len(entryTemplate.Artifacts))
	for _, artifact := range entryTemplate.Artifacts {
		uploaded, ok, err := u.uploadArtifact(ctx, dataRoot, entryID, nil, artifact, pdbID)
		if err != nil {
			return uploadedEntry{}, err
		}
		if ok {
			entryArtifacts = append(entryArtifacts, uploaded.Request)
			uploadedArtifactIDs = append(uploadedArtifactIDs, uploaded.Ref.ArtifactID)
		}
	}

	entryIDPtr := entryID
	name := strings.ReplaceAll(entryTemplate.Name, templatePDBID, canonicalPDBID(entryPDBID))
	if strings.TrimSpace(name) == "" {
		name = entryPDBID
	}
	if err := u.dynamicPDBClient.CreateEntry(ctx, dynamicpdbapi.CreateEntryRequest{
		ID:                &entryIDPtr,
		Name:              name,
		Description:       entryDescription(metadata),
		ThumbnailImageURL: thumbnailImageURL,
		Metadata:          metadata,
		Artifacts:         entryArtifacts,
	}); err != nil {
		if isPDBRefConflict(err) {
			existingEntry, lookupErr := u.existingEntryByPDBID(ctx, entryPDBID)
			if lookupErr != nil {
				return uploadedEntry{}, lookupErr
			}
			if existingEntry != nil {
				return uploadedEntry{EntryID: existingEntry.ID}, nil
			}
		}
		return uploadedEntry{}, err
	}

	return uploadedEntry{
		EntryID:        entryID,
		CreatedEntryID: entryID,
		ArtifactIDs:    uploadedArtifactIDs,
		ArtifactCount:  len(entryArtifacts),
	}, nil
}

type uploadedModels struct {
	Count         int
	ArtifactCount int
	ModelIDs      []string
	ArtifactIDs   []string
	RunIDs        []string
	MetricIDs     []string
}

func (u *Uploader) uploadModels(
	ctx context.Context,
	modelTemplates []manifest.ModelPattern,
	dataRoot string,
	pdbID string,
	entryID string,
) (uploadedModels, error) {
	uploadedModelIDs := make([]string, 0, len(modelTemplates))
	uploadedArtifactIDs := make([]string, 0)
	uploadedRunIDs := make([]string, 0)
	uploadedMetricIDs := make([]string, 0)
	artifactCount := 0
	for _, model := range modelTemplates {
		createdModel, ok, err := u.uploadModel(ctx, dataRoot, entryID, model, pdbID)
		if err != nil {
			return uploadedModels{}, err
		}
		if !ok {
			continue
		}
		uploadedModelIDs = append(uploadedModelIDs, createdModel.ModelID)
		uploadedArtifactIDs = append(uploadedArtifactIDs, createdModel.ArtifactIDs...)
		uploadedRunIDs = append(uploadedRunIDs, createdModel.RunIDs...)
		uploadedMetricIDs = append(uploadedMetricIDs, createdModel.MetricIDs...)
		artifactCount += createdModel.ArtifactCount
	}
	return uploadedModels{
		Count:         len(uploadedModelIDs),
		ArtifactCount: artifactCount,
		ModelIDs:      uploadedModelIDs,
		ArtifactIDs:   uploadedArtifactIDs,
		RunIDs:        uploadedRunIDs,
		MetricIDs:     uploadedMetricIDs,
	}, nil
}

type uploadedModel struct {
	ModelID       string
	ArtifactIDs   []string
	RunIDs        []string
	MetricIDs     []string
	ArtifactCount int
}

func (u *Uploader) uploadModel(
	ctx context.Context,
	dataRoot string,
	entryID string,
	model manifest.ModelPattern,
	pdbID string,
) (uploadedModel, bool, error) {
	modelID := uuid.NewString()
	modelArtifacts := make([]dynamicpdbapi.CreateArtifactRequest, 0, len(model.Artifacts))
	modelArtifactRefs := make([]uploadedArtifactRef, 0, len(model.Artifacts))
	artifactPayloads := make(map[string]extractorapi.Artifact, len(model.Artifacts))
	hasCoordinates, err := u.hasModelCoordinates(ctx, dataRoot, pdbID, model)
	if err != nil {
		return uploadedModel{}, false, err
	}
	if !hasCoordinates {
		return uploadedModel{}, false, nil
	}
	for _, artifact := range model.Artifacts {
		uploaded, ok, err := u.uploadArtifact(ctx, dataRoot, entryID, &modelID, artifact, pdbID)
		if err != nil {
			return uploadedModel{}, false, err
		}
		if ok {
			modelArtifacts = append(modelArtifacts, uploaded.Request)
			modelArtifactRefs = append(modelArtifactRefs, uploaded.Ref)
			artifactPayloads[artifact.ID] = uploaded.Payload
		}
	}
	if len(modelArtifacts) == 0 {
		return uploadedModel{}, false, nil
	}

	metadata, err := u.modelMetadata(ctx, dataRoot, pdbID, model.Metadata, artifactPayloads)
	if err != nil {
		return uploadedModel{}, false, err
	}
	if strings.TrimSpace(model.ModelType) != "" {
		metadata["model_type"] = strings.TrimSpace(model.ModelType)
	}
	if strings.TrimSpace(model.Purpose) != "" {
		metadata["purpose"] = strings.TrimSpace(model.Purpose)
	}
	metrics, err := u.modelMetrics(ctx, dataRoot, pdbID, model.Metrics, artifactPayloads)
	if err != nil {
		return uploadedModel{}, false, err
	}
	program, err := modelProgram(ctx, pdbID, artifactPayloads)
	if err != nil {
		return uploadedModel{}, false, err
	}

	modelIDPtr := modelID
	primaryArtifactID := modelArtifacts[0].ID
	name := strings.TrimSpace(model.Name)
	if name == "" {
		name = model.ID
	}
	request := dynamicpdbapi.CreateModelRequest{
		ID:                &modelIDPtr,
		Name:              name,
		Metadata:          metadata,
		PrimaryArtifactID: &primaryArtifactID,
		Artifacts:         modelArtifacts,
		Runs:              modelRuns(modelArtifactRefs, program),
		Metrics:           metrics,
	}
	if err := u.dynamicPDBClient.CreateModel(ctx, entryID, request); err != nil {
		return uploadedModel{}, false, err
	}
	return uploadedModel{
		ModelID:       modelID,
		ArtifactIDs:   artifactIDs(modelArtifacts),
		RunIDs:        runIDs(request.Runs),
		MetricIDs:     metricIDs(metrics),
		ArtifactCount: len(modelArtifacts),
	}, true, nil
}

func (u *Uploader) hasModelCoordinates(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	model manifest.ModelPattern,
) (bool, error) {
	coordinates, _, ok := modelCoordinatesArtifact(model)
	if !ok {
		return false, nil
	}
	if !rcsbSourceIsEmpty(coordinates.Source.RCSB) {
		return true, nil
	}
	if len(coordinates.Source.Files) == 0 {
		return false, nil
	}
	_, ok, err := fileextractor.NewArtifactExtractor(dataRoot).Extract(ctx, pdbID, coordinates)
	if err != nil {
		return false, fmt.Errorf("check model coordinates: %w", err)
	}
	return ok, nil
}

func modelCoordinatesArtifact(model manifest.ModelPattern) (manifest.Artifact, int, bool) {
	for index, artifact := range model.Artifacts {
		if strings.EqualFold(strings.TrimSpace(artifact.ID), "coordinates") {
			return artifact, index, true
		}
	}
	return manifest.Artifact{}, 0, false
}

type uploadedArtifact struct {
	Request dynamicpdbapi.CreateArtifactRequest
	Payload extractorapi.Artifact
	Ref     uploadedArtifactRef
}

type uploadedArtifactRef struct {
	ManifestID string
	ArtifactID string
	Format     string
}

func (u *Uploader) uploadArtifact(
	ctx context.Context,
	dataRoot string,
	entryID string,
	modelID *string,
	artifact manifest.Artifact,
	pdbID string,
) (uploadedArtifact, bool, error) {
	payload, ok, err := u.artifactPayload(ctx, dataRoot, pdbID, artifact)
	if err != nil {
		return uploadedArtifact{}, false, err
	}
	if !ok {
		return uploadedArtifact{}, false, nil
	}

	artifactID := uuid.NewString()
	artifactURI := strings.TrimSpace(payload.URI)
	if artifactURI == "" {
		uploadedURL, err := uploadPayload(ctx, u.dynamicPDBClient, entryID, modelID, artifactID, payload.Filename, payload.Size, payload.LocalPath, payload.Contents)
		if err != nil {
			return uploadedArtifact{}, false, err
		}
		artifactURI = uploadedURL
	}
	name, err := artifactName(artifact, payload)
	if err != nil {
		return uploadedArtifact{}, false, err
	}
	level := strings.TrimSpace(artifact.Level)
	if level == "" {
		level = "L1"
	}
	request := dynamicpdbapi.CreateArtifactRequest{
		ID:        artifactID,
		Name:      name,
		Level:     level,
		URI:       stringPtr(artifactURI),
		SHA256:    stringPtr(payload.SHA256),
		Format:    stringPtr(payload.Format),
		SizeBytes: int64Ptr(payload.Size),
		Metadata:  payload.Metadata,
	}
	ref := uploadedArtifactRef{
		ManifestID: artifact.ID,
		ArtifactID: artifactID,
		Format:     payload.Format,
	}
	return uploadedArtifact{Request: request, Payload: payload, Ref: ref}, true, nil
}

func (u *Uploader) uploadPreviewImage(
	ctx context.Context,
	dataRoot string,
	entryID string,
	previewImage *manifest.EntryPreviewImage,
	pdbID string,
) (*string, error) {
	if previewImage == nil || sourceIsEmpty(previewImage.Source) {
		return nil, nil
	}
	image, ok, err := u.imagePayload(ctx, dataRoot, pdbID, previewImage.Source)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	uploadedURL, err := uploadPayload(ctx, u.dynamicPDBClient, entryID, nil, uuid.NewString(), image.Filename, image.Size, image.LocalPath, image.Contents)
	if err != nil {
		return nil, fmt.Errorf("upload preview image: %w", err)
	}
	return &uploadedURL, nil
}

func (u *Uploader) imagePayload(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	source manifest.Source,
) (extractorapi.Image, bool, error) {
	switch {
	case sourceIsEmpty(source):
		return extractorapi.Image{}, false, nil
	case !rcsbSourceIsEmpty(source.RCSB):
		return rcsbextractor.NewImageExtractor(u.rcsb).Extract(ctx, pdbID, source)
	case strings.TrimSpace(source.Artifact) != "":
		return extractorapi.Image{}, false, fmt.Errorf("artifact reference %s cannot be used as an image source", source.Artifact)
	default:
		return fileextractor.NewImageExtractor(dataRoot).Extract(ctx, pdbID, source)
	}
}

func uploadPayload(
	ctx context.Context,
	dynamicPDBClient dynamicpdbapi.Client,
	entryID string,
	modelID *string,
	artifactID string,
	filename string,
	size int64,
	localPath string,
	contents []byte,
) (string, error) {
	grant, err := dynamicPDBClient.CreateFileUpload(ctx, dynamicpdbapi.CreateFileUploadRequest{
		EntryID:    entryID,
		ModelID:    modelID,
		ArtifactID: artifactID,
		Filename:   filename,
		Size:       size,
	})
	if err != nil {
		return "", err
	}

	completedParts := make([]dynamicpdbapi.CompletedFileUploadPart, 0, len(grant.Parts))
	for _, part := range grant.Parts {
		reader, size, closeReader, err := partReader(contents, localPath, size, grant.PartSize, part.PartNumber)
		if err != nil {
			return "", err
		}
		etag, uploadErr := dynamicPDBClient.PutUploadPart(ctx, part.URL, reader, size)
		closeErr := closeReader()
		if uploadErr != nil {
			return "", uploadErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		completedParts = append(completedParts, dynamicpdbapi.CompletedFileUploadPart{
			PartNumber: part.PartNumber,
			ETag:       etag,
		})
	}
	if err := dynamicPDBClient.CompleteFileUpload(ctx, dynamicpdbapi.CompleteFileUploadRequest{
		Key:      grant.Key,
		UploadID: grant.UploadID,
		Parts:    completedParts,
	}); err != nil {
		return "", err
	}
	return grant.ObjectURL, nil
}

func partReader(contents []byte, localPath string, payloadSize int64, partSize int64, partNumber int) (io.Reader, int64, func() error, error) {
	offset := int64(partNumber-1) * partSize
	size := min(partSize, payloadSize-offset)
	if size < 0 {
		return nil, 0, nil, fmt.Errorf("part %d starts beyond payload size", partNumber)
	}
	if contents != nil {
		return bytes.NewReader(contents[offset : offset+size]), size, func() error { return nil }, nil
	}
	file, err := os.Open(localPath)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("open upload file: %w", err)
	}
	return io.NewSectionReader(file, offset, size), size, file.Close, nil
}

func discoverPDBIDs(dataRoot string, entryTemplate manifest.Entry) ([]string, error) {
	patterns := localFilePatterns(entryTemplate)
	pdbIDs := make(map[string]struct{})
	localFiles, err := allLocalFiles(dataRoot)
	if err != nil {
		return nil, err
	}
	for _, localFile := range localFiles {
		for _, pattern := range patterns {
			matches := pattern.FindStringSubmatch(localFile)
			if len(matches) == 2 {
				pdbIDs[strings.ToLower(matches[1])] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(pdbIDs))
	for pdbID := range pdbIDs {
		ids = append(ids, pdbID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, errors.New("manifest did not match any local files")
	}
	return ids, nil
}

func allLocalFiles(dataRoot string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		source, err := filepath.Rel(dataRoot, path)
		if err != nil {
			return fmt.Errorf("build source path for %s: %w", path, err)
		}
		source = filepath.ToSlash(source)
		if strings.EqualFold(filepath.Ext(source), ".zip") {
			zipFiles, err := allFilesInsideZip(path, source)
			if err != nil {
				return err
			}
			files = append(files, zipFiles...)
			return nil
		}
		files = append(files, source)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list local sources: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func allFilesInsideZip(archivePath string, archiveSource string) ([]string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open zip %s: %w", archiveSource, err)
	}
	files := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		name := strings.TrimLeft(strings.ReplaceAll(file.Name, "\\", "/"), "/")
		if name == "" || file.FileInfo().IsDir() || strings.HasPrefix(pathpkg.Base(name), ".") {
			continue
		}
		files = append(files, archiveSource+"#"+name)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("close zip %s: %w", archiveSource, err)
	}
	return files, nil
}

func localFilePatterns(entryTemplate manifest.Entry) []*regexp.Regexp {
	sources := make([]string, 0)
	if entryTemplate.PreviewImage != nil {
		sources = append(sources, entryTemplate.PreviewImage.Source.Files...)
	}
	for _, artifact := range entryTemplate.Artifacts {
		sources = append(sources, artifact.Source.Files...)
	}
	for _, model := range entryTemplate.Models {
		for _, artifact := range model.Artifacts {
			sources = append(sources, artifact.Source.Files...)
		}
	}
	patterns := make([]*regexp.Regexp, 0, len(sources))
	for _, source := range sources {
		templateIndex := strings.Index(source, templatePDBID)
		if templateIndex < 0 {
			continue
		}
		patterns = append(patterns, regexp.MustCompile("^"+regexp.QuoteMeta(source[:templateIndex])+`([0-9][A-Za-z0-9]{3})`+regexp.QuoteMeta(source[templateIndex+len(templatePDBID):])+"$"))
	}
	return patterns
}

func sourceIsEmpty(source manifest.Source) bool {
	return len(source.Files) == 0 &&
		rcsbSourceIsEmpty(source.RCSB) &&
		strings.TrimSpace(source.Artifact) == ""
}

func rcsbSourceIsEmpty(source *manifest.RCSBSource) bool {
	return source == nil ||
		(strings.TrimSpace(source.PDBID) == "" &&
			strings.TrimSpace(source.Resource) == "" &&
			strings.TrimSpace(source.File) == "")
}

func (u *Uploader) entryMetadata(ctx context.Context, dataRoot string, metadata manifest.EntryMetadata, pdbID string) (map[string]any, error) {
	result := baseEntryMetadata(pdbID)
	for key, field := range metadata {
		value, ok, err := u.fieldValue(ctx, dataRoot, pdbID, field, nil)
		if err != nil {
			return nil, fmt.Errorf("extract entry metadata %s: %w", key, err)
		}
		if !ok {
			continue
		}
		result[key] = toCanonicalMetadataValue(key, value)
	}
	return result, nil
}

func baseEntryMetadata(pdbID string) map[string]any {
	pdbID = canonicalPDBID(pdbID)
	if pdbID == "" {
		return map[string]any{}
	}
	return map[string]any{"external_refs": map[string]any{"pdb": pdbID}}
}

func toCanonicalMetadataValue(key string, value any) any {
	switch key {
	case "method":
		text, ok := value.(string)
		if !ok {
			return value
		}
		return canonicalMethod(text)
	case "resolution":
		number, ok := floatValue(value)
		if ok {
			return number
		}
	}
	return value
}

func canonicalMethod(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "x-ray") || strings.Contains(value, "xray") {
		return "X-ray crystallography"
	}
	if strings.Contains(value, "electron microscopy") ||
		strings.Contains(value, "cryo-em") ||
		strings.Contains(value, "cryoem") {
		return "CryoEM"
	}
	return strings.TrimSpace(raw)
}

func (u *Uploader) modelMetadata(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	metadata manifest.ModelMetadata,
	artifacts map[string]extractorapi.Artifact,
) (map[string]any, error) {
	result := make(map[string]any)
	for key, field := range metadata {
		value, ok, err := u.fieldValue(ctx, dataRoot, pdbID, field, artifacts)
		if err != nil {
			return nil, fmt.Errorf("extract model metadata %s: %w", key, err)
		}
		if !ok {
			continue
		}
		result[key] = toCanonicalModelMetadataValue(key, value)
	}
	return result, nil
}

func toCanonicalModelMetadataValue(key string, value any) any {
	switch key {
	case "atom_count", "modeled_residues", "unique_protein_chains":
		if number, ok := integerValue(value); ok {
			return number
		}
	case "ligands":
		return rcsbLigands(appendStringValues(nil, value))
	case "authors":
		authors := appendStringValues(nil, value)
		if len(authors) > 0 {
			return authors
		}
	case "affiliation":
		affiliations := appendStringValues(nil, value)
		if len(affiliations) > 0 {
			return strings.Join(affiliations, "; ")
		}
	}
	return value
}

func integerValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		if err == nil {
			return int(number), true
		}
		float, err := typed.Float64()
		return int(float), err == nil
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		return number, err == nil
	default:
		return 0, false
	}
}

func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func (u *Uploader) modelMetrics(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	metrics manifest.Metrics,
	artifacts map[string]extractorapi.Artifact,
) ([]dynamicpdbapi.CreateMetricRequest, error) {
	requests := make([]dynamicpdbapi.CreateMetricRequest, 0, len(metrics))
	for _, key := range metricKeys(metrics) {
		field := metrics[key]
		value, ok, err := u.fieldValue(ctx, dataRoot, pdbID, field, artifacts)
		if err != nil {
			return nil, fmt.Errorf("extract metric %s: %w", key, err)
		}
		if !ok {
			continue
		}
		number, ok := metricNumber(value)
		if !ok {
			return nil, fmt.Errorf("metric %s is not numeric", key)
		}
		requests = append(requests, dynamicpdbapi.CreateMetricRequest{
			ID:    uuid.NewString(),
			Key:   key,
			Value: number,
		})
	}
	return requests, nil
}

func metricKeys(metrics manifest.Metrics) []string {
	preferred := []string{"r_free", "r_work"}
	seen := make(map[string]struct{}, len(preferred))
	keys := make([]string, 0, len(metrics))
	for _, key := range preferred {
		if _, ok := metrics[key]; ok {
			keys = append(keys, key)
			seen[key] = struct{}{}
		}
	}
	rest := make([]string, 0, len(metrics))
	for key := range metrics {
		if _, ok := seen[key]; ok {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func metricNumber(value any) (float64, bool) {
	return floatValue(value)
}

func (u *Uploader) fieldValue(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	fields []manifest.FieldExtraction,
	artifacts map[string]extractorapi.Artifact,
) (any, bool, error) {
	for _, field := range fields {
		value, ok, err := u.fieldExtractionValue(ctx, dataRoot, pdbID, field, artifacts)
		if err != nil || fieldValuePresent(value, ok) {
			return value, ok, err
		}
	}
	return nil, false, nil
}

func (u *Uploader) fieldExtractionValue(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	field manifest.FieldExtraction,
	artifacts map[string]extractorapi.Artifact,
) (any, bool, error) {
	source := fieldExtractionSource(field)
	switch {
	case sourceIsEmpty(source):
		return nil, false, nil
	case !rcsbSourceIsEmpty(source.RCSB):
		return rcsbextractor.NewFieldExtractor(u.rcsb).Extract(ctx, pdbID, source, field.Extract)
	case strings.TrimSpace(source.Artifact) != "":
		return artifactextractor.NewFieldExtractor(artifacts).Extract(ctx, pdbID, source, field.Extract)
	default:
		return fileextractor.NewFieldExtractor(dataRoot).Extract(ctx, pdbID, source, field.Extract)
	}
}

func fieldExtractionSource(field manifest.FieldExtraction) manifest.Source {
	return field.Source
}

func fieldValuePresent(value any, ok bool) bool {
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []string:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return true
	}
}

func (u *Uploader) artifactPayload(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	artifact manifest.Artifact,
) (extractorapi.Artifact, bool, error) {
	switch {
	case sourceIsEmpty(artifact.Source):
		return extractorapi.Artifact{}, false, nil
	case !rcsbSourceIsEmpty(artifact.Source.RCSB):
		return rcsbextractor.NewArtifactExtractor(u.rcsb).Extract(ctx, pdbID, artifact)
	case strings.TrimSpace(artifact.Source.Artifact) != "":
		return extractorapi.Artifact{}, false, fmt.Errorf("artifact reference %s cannot be used as an artifact source", artifact.Source.Artifact)
	default:
		return fileextractor.NewArtifactExtractor(dataRoot).Extract(ctx, pdbID, artifact)
	}
}

func manifestEntry(uploadingManifest manifest.Manifest) (manifest.Entry, error) {
	switch len(uploadingManifest.Entries) {
	case 0:
		return manifest.Entry{}, errors.New("manifest entries must contain one entry template")
	case 1:
		return uploadingManifest.Entries[0], nil
	default:
		return manifest.Entry{}, errors.New("manifest entries with multiple templates are not supported yet")
	}
}

func filteredPDBIDs(pdbIDs []string, filter manifest.Filter) []string {
	include := idSet(filter.Include)
	skip := idSet(filter.Skip)
	selected := make([]string, 0, len(pdbIDs))
	for _, pdbID := range pdbIDs {
		normalized := strings.ToLower(strings.TrimSpace(pdbID))
		if len(include) > 0 {
			if _, ok := include[normalized]; !ok {
				continue
			}
		}
		if _, ok := skip[normalized]; ok {
			continue
		}
		selected = append(selected, pdbID)
	}
	return selected
}

func pendingPDBIDs(pdbIDs []string, state State) []string {
	pending := make([]string, 0, len(pdbIDs))
	for _, pdbID := range pdbIDs {
		if state.completedEntry(pdbID) {
			continue
		}
		pending = append(pending, pdbID)
	}
	return pending
}

func skippedPDBIDs(all []string, selected []string) []string {
	selectedSet := idSet(selected)
	skipped := make([]string, 0, len(all)-len(selected))
	for _, pdbID := range all {
		if _, ok := selectedSet[strings.ToLower(strings.TrimSpace(pdbID))]; !ok {
			skipped = append(skipped, canonicalPDBID(pdbID))
		}
	}
	return skipped
}

func idSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func canonicalPDBID(pdbID string) string {
	return strings.ToUpper(strings.TrimSpace(pdbID))
}

func (u *Uploader) existingEntryByPDBID(ctx context.Context, pdbID string) (*dynamicpdbapi.Entry, error) {
	pdbID = canonicalPDBID(pdbID)
	if pdbID == "" {
		return nil, nil
	}
	entries, err := u.dynamicPDBClient.ListEntries(ctx, dynamicpdbapi.ListEntriesParams{PDBIDs: []string{pdbID}})
	if err != nil {
		return nil, fmt.Errorf("list existing entries by PDB ID: %w", err)
	}
	for _, entry := range entries {
		entryPDBID, ok := entryPDBID(entry)
		if !ok || entryPDBID != pdbID {
			continue
		}
		entryCopy := entry
		return &entryCopy, nil
	}
	return nil, nil
}

func entryPDBID(entry dynamicpdbapi.Entry) (string, bool) {
	refs, ok := entry.Metadata["external_refs"].(map[string]any)
	if !ok {
		return "", false
	}
	pdbID, ok := refs["pdb"].(string)
	if !ok {
		return "", false
	}
	pdbID = canonicalPDBID(pdbID)
	return pdbID, pdbID != ""
}

func isPDBRefConflict(err error) bool {
	var dynamicPDBError *dynamicpdbapi.Error
	return errors.As(err, &dynamicPDBError) &&
		dynamicPDBError.Status == http.StatusConflict &&
		dynamicPDBError.Code == "ENTRY_PDB_REF_EXISTS"
}

func artifactIDs(artifacts []dynamicpdbapi.CreateArtifactRequest) []string {
	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		ids = append(ids, artifact.ID)
	}
	return ids
}

func entryDescription(metadata map[string]any) *string {
	title, ok := metadata["title"].(string)
	if !ok {
		return nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	return &title
}

type modelProgramInfo struct {
	Name    string
	Version string
}

func modelProgram(ctx context.Context, pdbID string, artifacts map[string]extractorapi.Artifact) (*modelProgramInfo, error) {
	extractor := artifactextractor.NewFieldExtractor(artifacts)
	name, ok, err := extractor.Extract(ctx, pdbID, manifest.Source{Artifact: "coordinates"}, structureExtract("program.name"))
	if err != nil {
		return nil, fmt.Errorf("extract model program name: %w", err)
	}
	if !ok {
		return nil, nil
	}
	program := modelProgramInfo{Name: strings.TrimSpace(fmt.Sprint(name))}
	if program.Name == "" {
		return nil, nil
	}
	version, ok, err := extractor.Extract(ctx, pdbID, manifest.Source{Artifact: "coordinates"}, structureExtract("program.version"))
	if err != nil {
		return nil, fmt.Errorf("extract model program version: %w", err)
	}
	if ok {
		program.Version = strings.TrimSpace(fmt.Sprint(version))
	}
	return &program, nil
}

func structureExtract(field string) manifest.Extract {
	return manifest.Extract{
		PDB:   &manifest.ExtractRule{Field: field},
		MMCIF: &manifest.ExtractRule{Field: field},
	}
}

func modelRuns(artifacts []uploadedArtifactRef, program *modelProgramInfo) []dynamicpdbapi.CreateRunRequest {
	runArtifacts := make([]dynamicpdbapi.CreateRunArtifactRequest, 0, len(artifacts))
	for _, artifact := range artifacts {
		direction, ok := runArtifactDirection(artifact)
		if !ok {
			continue
		}
		runArtifacts = append(runArtifacts, dynamicpdbapi.CreateRunArtifactRequest{
			ArtifactID: artifact.ArtifactID,
			Direction:  direction,
		})
	}
	if len(runArtifacts) == 0 || !hasRunOutput(runArtifacts) || !hasRunInput(runArtifacts) {
		return nil
	}
	if program == nil || strings.TrimSpace(program.Name) == "" {
		return nil
	}
	softwareName := strings.TrimSpace(program.Name)
	softwareVersion := stringPtr(program.Version)
	runID := uuid.NewString()
	return []dynamicpdbapi.CreateRunRequest{
		{
			ID:              runID,
			Name:            softwareName,
			SoftwareName:    &softwareName,
			SoftwareVersion: softwareVersion,
			Parameters:      map[string]any{},
			Metadata:        map[string]any{},
			Artifacts:       runArtifacts,
		},
	}
}

func runArtifactDirection(artifact uploadedArtifactRef) (string, bool) {
	id := strings.ToLower(strings.TrimSpace(artifact.ManifestID))
	format := strings.ToLower(strings.TrimSpace(artifact.Format))
	switch {
	case id == "coordinates" || strings.HasPrefix(id, "log_"):
		return "output", true
	case strings.HasPrefix(id, "mtz_") || strings.HasPrefix(id, "structure_factors_"):
		return "input", true
	case format == "mtz" || format == "structure_factors_cif" || format == "map" || format == "ccp4" || format == "mrc":
		return "input", true
	default:
		return "", false
	}
}

func hasRunOutput(artifacts []dynamicpdbapi.CreateRunArtifactRequest) bool {
	for _, artifact := range artifacts {
		if artifact.Direction == "output" {
			return true
		}
	}
	return false
}

func hasRunInput(artifacts []dynamicpdbapi.CreateRunArtifactRequest) bool {
	for _, artifact := range artifacts {
		if artifact.Direction == "input" {
			return true
		}
	}
	return false
}

func rcsbLigands(components []string) []string {
	ligands := []string{}
	for _, component := range components {
		for _, ligand := range strings.FieldsFunc(component, ligandSeparator) {
			ligand = strings.ToUpper(strings.TrimSpace(ligand))
			if ligand != "" && !rcsbIncidentalCompounds[ligand] {
				ligands = appendUniqueNonEmptyString(ligands, ligand)
			}
		}
	}
	return ligands
}

func ligandSeparator(value rune) bool {
	return value == ';' || value == ','
}

func appendUniqueNonEmptyString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendStringValues(values []string, value any) []string {
	switch typed := value.(type) {
	case string:
		values = appendUniqueNonEmptyString(values, typed)
	case []string:
		for _, item := range typed {
			values = appendUniqueNonEmptyString(values, item)
		}
	case []any:
		for _, item := range typed {
			values = appendStringValues(values, item)
		}
	}
	return values
}

var rcsbIncidentalCompounds = map[string]bool{
	"HOH": true,
	"DOD": true,
	"WAT": true,
	"H2O": true,
}

func runIDs(runs []dynamicpdbapi.CreateRunRequest) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}

func modelIDs(models []dynamicpdbapi.CreateModelRequest) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		id := modelID(model)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func modelID(model dynamicpdbapi.CreateModelRequest) string {
	if model.ID == nil {
		return ""
	}
	return *model.ID
}

func metricIDs(metrics []dynamicpdbapi.CreateMetricRequest) []string {
	ids := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		ids = append(ids, metric.ID)
	}
	return ids
}

func artifactName(artifact manifest.Artifact, payload extractorapi.Artifact) (string, error) {
	name := strings.TrimSpace(artifact.Name)
	if name != "" {
		return name, nil
	}
	name = strings.TrimSpace(payload.Filename)
	if name != "" {
		return name, nil
	}
	return "", fmt.Errorf("artifact %s has no name and resolved source has no filename", artifact.ID)
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func int64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
