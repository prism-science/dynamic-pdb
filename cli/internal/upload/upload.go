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
	"log/slog"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	extractorapi "dynamic-pdb/cli/internal/upload/extractors"
	artifactextractor "dynamic-pdb/cli/internal/upload/extractors/artifact"
	fileextractor "dynamic-pdb/cli/internal/upload/extractors/file"
	rcsbextractor "dynamic-pdb/cli/internal/upload/extractors/rcsb"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/rcsb"
)

const templatePDBID = "{{ pdb_id }}"

const uploadPartAttempts = 3

type Summary struct {
	Entries   int
	Models    int
	Artifacts int
	Skipped   int
	StatePath string
}

type Options struct {
	Include         []string
	Skip            []string
	OverrideInclude bool
	OverrideSkip    bool
}

type Uploader struct {
	dynamicPDBClient dynamicpdbapi.Client
	rcsb             rcsb.Client
	progress         Progress
	concurrency      int
	partConcurrency  int
	// structureFields holds each structure artifact's parsed fields, so a
	// file is read once however many manifest fields come from it.
	structureFields *artifactextractor.FieldCache
	// legacyManifestWarning logs once per run that a manifest predates cofactors.
	legacyManifestWarning sync.Once
}

func New(
	dynamicPDBClient dynamicpdbapi.Client,
	rcsbClient rcsb.Client,
	progress Progress,
	concurrency int,
	partConcurrency ...int,
) *Uploader {
	normalizedPartConcurrency := 1
	if len(partConcurrency) > 0 {
		normalizedPartConcurrency = normalizeConcurrency(partConcurrency[0])
	}
	return &Uploader{
		dynamicPDBClient: dynamicPDBClient,
		rcsb:             rcsbClient,
		progress:         progress,
		concurrency:      normalizeConcurrency(concurrency),
		partConcurrency:  normalizedPartConcurrency,
		structureFields:  artifactextractor.NewFieldCache(),
	}
}

func (u *Uploader) Upload(ctx context.Context, manifestPath string, options ...Options) (Summary, error) {
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
	if err := manifest.Validate(uploadingManifest); err != nil {
		return Summary{}, err
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

	uploadFilter := appliedUploadFilter(uploadingManifest.Filter, options...)
	selectedPDBIDs := filteredPDBIDs(pdbIDs, uploadFilter)
	pendingPDBIDs := pendingPDBIDs(selectedPDBIDs, state)
	if err := u.progress.Start(len(pendingPDBIDs)); err != nil {
		return Summary{}, fmt.Errorf("start upload progress: %w", err)
	}
	recorder := newUploadStateRecorder(statePath, state)
	uploadSummary, err := u.uploadEntries(ctx, entryTemplate, dataRoot, pendingPDBIDs, recorder)
	uploadSummary.StatePath = statePath
	uploadSummary.Skipped = len(pdbIDs) - len(pendingPDBIDs)
	if err != nil {
		return uploadSummary, err
	}
	if err := u.progress.Finish(); err != nil {
		return uploadSummary, fmt.Errorf("finish upload progress: %w", err)
	}
	return uploadSummary, nil
}

func (u *Uploader) uploadEntries(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbIDs []string,
	state *uploadStateRecorder,
) (Summary, error) {
	if len(pdbIDs) == 0 {
		return Summary{}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// dispatchCtx stops handing out entries. An entry that fails its schema checks
	// stops only the dispatch, so entries already uploading finish and leave no
	// unfinished state; any other error also cancels them.
	dispatchCtx, stopDispatch := context.WithCancel(ctx)
	defer stopDispatch()

	jobs := make(chan string)
	results := make(chan entryUploadResult, len(pdbIDs))
	errors := make(chan error, 1)
	workers := min(normalizeConcurrency(u.concurrency), len(pdbIDs))
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for pdbID := range jobs {
				if dispatchCtx.Err() != nil {
					return
				}
				entryResult, err := u.uploadEntryJob(ctx, entryTemplate, dataRoot, pdbID, state)
				if err != nil {
					select {
					case errors <- err:
						if isEntryCheckError(err) {
							stopDispatch()
						} else {
							cancel()
						}
					default:
					}
					return
				}
				select {
				case results <- entryResult:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, pdbID := range pdbIDs {
			select {
			case jobs <- pdbID:
			case <-dispatchCtx.Done():
				return
			}
		}
	}()

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	summary := Summary{}
	for entryResult := range results {
		summary.Entries++
		summary.Models += entryResult.Models
		summary.Artifacts += entryResult.Artifacts
	}
	select {
	case err := <-errors:
		return summary, err
	default:
		return summary, nil
	}
}

func (u *Uploader) uploadEntryJob(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbID string,
	state *uploadStateRecorder,
) (entryUploadResult, error) {
	existingEntry, err := u.existingEntryByPDBID(ctx, pdbID)
	if err != nil {
		return entryUploadResult{}, fmt.Errorf("check existing entry %s: %w", pdbID, err)
	}
	prepared, err := u.prepareEntry(ctx, entryTemplate, dataRoot, pdbID, existingEntry)
	if err != nil {
		if isEntryCheckError(err) {
			return entryUploadResult{}, err
		}
		return entryUploadResult{}, fmt.Errorf("prepare %s: %w", pdbID, err)
	}
	if err := state.startEntry(pdbID); err != nil {
		return entryUploadResult{}, err
	}
	entryResult, err := u.uploadEntry(ctx, entryTemplate, dataRoot, pdbID, existingEntry, prepared)
	if err != nil {
		return entryUploadResult{}, fmt.Errorf("upload %s: %w", pdbID, err)
	}
	if err := state.completeEntry(entryResult); err != nil {
		return entryUploadResult{}, err
	}
	if err := u.entryDone(entryResult); err != nil {
		return entryUploadResult{}, fmt.Errorf("update upload progress: %w", err)
	}
	return entryResult, nil
}

func (u *Uploader) entryDone(result entryUploadResult) error {
	return u.progress.EntryDone(result.PDBID, result.EntryID, result.Models, result.Artifacts)
}

func normalizeConcurrency(concurrency int) int {
	if concurrency < 1 {
		return 1
	}
	return concurrency
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
	prepared preparedEntry,
) (entryUploadResult, error) {
	entryPDBID := canonicalPDBID(pdbID)
	if existingEntry != nil {
		uploadedModels, err := u.uploadModels(ctx, prepared.Models, existingEntry.ID, true)
		if err != nil {
			return entryUploadResult{}, err
		}
		return entryUploadResult{
			PDBID:       entryPDBID,
			EntryID:     existingEntry.ID,
			Models:      uploadedModels.Count,
			Artifacts:   uploadedModels.ArtifactCount,
			ModelIDs:    uploadedModels.ModelIDs,
			ArtifactIDs: uploadedModels.ArtifactIDs,
			RunIDs:      uploadedModels.RunIDs,
			MetricIDs:   uploadedModels.MetricIDs,
		}, nil
	}

	uploadedEntry, uploadedModels, err := u.uploadNewEntry(ctx, entryPDBID, prepared)
	if err != nil {
		return entryUploadResult{}, err
	}
	if uploadedEntry.Existing {
		// Another upload created the entry after the lookup. Its models are prepared
		// again, so their files go up under fresh ids for that entry.
		models, problems, err := u.prepareModels(ctx, entryTemplate.Models, dataRoot, pdbID)
		if err != nil {
			return entryUploadResult{}, err
		}
		if len(problems) > 0 {
			return entryUploadResult{}, &EntryCheckError{PDBID: entryPDBID, Problems: problems}
		}
		uploadedModels, err = u.uploadModels(ctx, models, uploadedEntry.EntryID, true)
		if err != nil {
			return entryUploadResult{}, err
		}
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
	Existing       bool
	ArtifactIDs    []string
	ArtifactCount  int
}

func (u *Uploader) uploadNewEntry(
	ctx context.Context,
	entryPDBID string,
	prepared preparedEntry,
) (uploadedEntry, uploadedModelSet, error) {
	thumbnailImageURL, err := u.uploadPreviewImage(ctx, prepared.UploadEntryID, prepared.PreviewImage)
	if err != nil {
		return uploadedEntry{}, uploadedModelSet{}, err
	}
	entryArtifacts, err := u.uploadArtifacts(ctx, prepared.UploadEntryID, nil, prepared.Artifacts)
	if err != nil {
		return uploadedEntry{}, uploadedModelSet{}, err
	}
	uploadedModels, err := u.uploadModels(ctx, prepared.Models, prepared.UploadEntryID, false)
	if err != nil {
		return uploadedEntry{}, uploadedModelSet{}, err
	}

	result, err := u.dynamicPDBClient.CreateEntry(ctx, dynamicpdbapi.CreateEntryRequest{
		Entry: dynamicpdbapi.CreateEntryData{
			EntryProperties:   prepared.Properties,
			Title:             prepared.Title,
			ThumbnailImageURL: thumbnailImageURL,
			Artifacts:         entryArtifacts,
		},
		ModelOperations: uploadedModels.ModelOperations,
	})
	if err != nil {
		if isPDBRefConflict(err) {
			existingEntry, lookupErr := u.existingEntryByPDBID(ctx, entryPDBID)
			if lookupErr != nil {
				return uploadedEntry{}, uploadedModelSet{}, lookupErr
			}
			if existingEntry != nil {
				return uploadedEntry{EntryID: existingEntry.ID, Existing: true}, uploadedModelSet{}, nil
			}
		}
		return uploadedEntry{}, uploadedModelSet{}, err
	}
	if len(result.ModelResults) != len(uploadedModels.ModelOperations) {
		return uploadedEntry{}, uploadedModelSet{}, fmt.Errorf(
			"backend returned %d model results for %d model operations",
			len(result.ModelResults),
			len(uploadedModels.ModelOperations),
		)
	}
	uploadedModels.ModelIDs = make([]string, 0, len(result.ModelResults))
	for _, modelResult := range result.ModelResults {
		if strings.TrimSpace(modelResult.ModelID) == "" {
			return uploadedEntry{}, uploadedModelSet{}, fmt.Errorf("backend returned an empty model id")
		}
		uploadedModels.ModelIDs = append(uploadedModels.ModelIDs, modelResult.ModelID)
	}

	return uploadedEntry{
		EntryID:        result.EntryID,
		CreatedEntryID: result.EntryID,
		ArtifactIDs:    artifactIDs(entryArtifacts),
		ArtifactCount:  len(entryArtifacts),
	}, uploadedModels, nil
}

type uploadedModelSet struct {
	Count           int
	ArtifactCount   int
	ModelIDs        []string
	ArtifactIDs     []string
	RunIDs          []string
	MetricIDs       []string
	ModelOperations []dynamicpdbapi.AddModelOperation
}

// uploadModels uploads the files of each prepared model. With create, each model is
// created under entryID at once; otherwise its add operation is returned for the
// entry's create request.
func (u *Uploader) uploadModels(
	ctx context.Context,
	models []preparedModel,
	entryID string,
	create bool,
) (uploadedModelSet, error) {
	uploadedModelIDs := make([]string, 0, len(models))
	uploadedArtifactIDs := make([]string, 0)
	uploadedRunIDs := make([]string, 0)
	uploadedMetricIDs := make([]string, 0)
	modelOperations := make([]dynamicpdbapi.AddModelOperation, 0, len(models))
	artifactCount := 0
	for _, model := range models {
		createdModel, err := u.uploadModel(ctx, entryID, model, create)
		if err != nil {
			return uploadedModelSet{}, err
		}
		uploadedModelIDs = append(uploadedModelIDs, createdModel.ModelID)
		uploadedArtifactIDs = append(uploadedArtifactIDs, createdModel.ArtifactIDs...)
		uploadedRunIDs = append(uploadedRunIDs, createdModel.RunIDs...)
		uploadedMetricIDs = append(uploadedMetricIDs, createdModel.MetricIDs...)
		modelOperations = append(modelOperations, createdModel.Operation)
		artifactCount += createdModel.ArtifactCount
	}
	return uploadedModelSet{
		Count:           len(uploadedModelIDs),
		ArtifactCount:   artifactCount,
		ModelIDs:        uploadedModelIDs,
		ArtifactIDs:     uploadedArtifactIDs,
		RunIDs:          uploadedRunIDs,
		MetricIDs:       uploadedMetricIDs,
		ModelOperations: modelOperations,
	}, nil
}

type uploadedModel struct {
	ModelID       string
	ArtifactIDs   []string
	RunIDs        []string
	MetricIDs     []string
	ArtifactCount int
	Operation     dynamicpdbapi.AddModelOperation
}

func (u *Uploader) uploadModel(
	ctx context.Context,
	entryID string,
	model preparedModel,
	create bool,
) (uploadedModel, error) {
	modelArtifacts, err := u.uploadArtifacts(ctx, entryID, &model.UploadModelID, model.Artifacts)
	if err != nil {
		return uploadedModel{}, err
	}
	primaryArtifactID := model.PrimaryArtifactID
	data := dynamicpdbapi.AddModelData{
		Title:             model.Title,
		Metadata:          model.Metadata,
		IdempotencyKey:    model.IdempotencyKey,
		PrimaryArtifactID: &primaryArtifactID,
		Artifacts:         modelArtifacts,
		Runs:              model.Runs,
		Metrics:           model.Metrics,
	}
	uploaded := uploadedModel{
		ArtifactIDs:   artifactIDs(modelArtifacts),
		RunIDs:        runIDs(model.Runs),
		MetricIDs:     metricIDs(model.Metrics),
		ArtifactCount: len(modelArtifacts),
		Operation:     dynamicpdbapi.AddModelOperation{Op: "add", Data: data},
	}
	if !create {
		return uploaded, nil
	}
	// AddModelData and CreateModelData carry the same fields: one is a model inside
	// an entry's create request, the other a model added to an existing entry.
	result, err := u.dynamicPDBClient.CreateModel(ctx, entryID, dynamicpdbapi.CreateModelRequest{
		Model: dynamicpdbapi.CreateModelData(data),
	})
	if err != nil {
		return uploadedModel{}, err
	}
	uploaded.ModelID = result.ModelID
	return uploaded, nil
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
		if isCoordinatesArtifact(artifact.ID) {
			return artifact, index, true
		}
	}
	return manifest.Artifact{}, 0, false
}

// artifactRef names an artifact by its manifest id and its API id, with its format.
type artifactRef struct {
	ManifestID string
	ArtifactID string
	Format     string
}

// uploadArtifacts uploads the bytes of each prepared artifact that has no URI yet,
// under entryID and modelID, and returns the artifacts' API requests.
func (u *Uploader) uploadArtifacts(
	ctx context.Context,
	entryID string,
	modelID *string,
	artifacts []preparedArtifact,
) ([]dynamicpdbapi.CreateArtifactRequest, error) {
	requests := make([]dynamicpdbapi.CreateArtifactRequest, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactURI := strings.TrimSpace(artifact.Payload.URI)
		if artifactURI == "" {
			payload := artifact.Payload
			uploadedURL, err := u.uploadPayload(ctx, entryID, modelID, artifact.Ref.ArtifactID, payload.Filename, payload.Size, payload.LocalPath, payload.Contents)
			if err != nil {
				return nil, err
			}
			artifactURI = uploadedURL
		}
		requests = append(requests, artifact.request(artifactURI))
	}
	return requests, nil
}

func artifactTypeForRequest(artifact manifest.Artifact, attachedToModel bool) string {
	if artifactType := strings.TrimSpace(artifact.Type); artifactType != "" {
		return artifactType
	}

	artifactID := strings.ToLower(strings.TrimSpace(artifact.ID))
	if !attachedToModel {
		if artifactID == "fasta" ||
			(artifact.Source.RCSB != nil && strings.EqualFold(strings.TrimSpace(artifact.Source.RCSB.Resource), "fasta")) {
			return "fasta"
		}
		return "other"
	}

	switch {
	case artifactID == coordinatesArtifactID:
		return "model"
	case artifactID == "structure_factors" || strings.HasPrefix(artifactID, "structure_factors_"):
		return "structure_factors"
	default:
		return "other"
	}
}

func (u *Uploader) uploadPreviewImage(ctx context.Context, entryID string, image *extractorapi.Image) (*string, error) {
	if image == nil {
		return nil, nil
	}
	uploadedURL, err := u.uploadPayload(ctx, entryID, nil, uuid.NewString(), image.Filename, image.Size, image.LocalPath, image.Contents)
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

func (u *Uploader) uploadPayload(
	ctx context.Context,
	entryID string,
	modelID *string,
	artifactID string,
	filename string,
	size int64,
	localPath string,
	contents []byte,
) (string, error) {
	grant, err := u.dynamicPDBClient.CreateFileUpload(ctx, dynamicpdbapi.CreateFileUploadRequest{
		EntryID:    entryID,
		ModelID:    modelID,
		ArtifactID: artifactID,
		Filename:   filename,
		Size:       size,
	})
	if err != nil {
		return "", err
	}

	completedParts, err := u.uploadParts(ctx, grant.Parts, contents, localPath, size, grant.PartSize)
	if err != nil {
		return "", err
	}
	if err := u.dynamicPDBClient.CompleteFileUpload(ctx, dynamicpdbapi.CompleteFileUploadRequest{
		Key:      grant.Key,
		UploadID: grant.UploadID,
		Parts:    completedParts,
	}); err != nil {
		return "", err
	}
	return grant.ObjectURL, nil
}

func (u *Uploader) uploadParts(
	ctx context.Context,
	parts []dynamicpdbapi.FileUploadPart,
	contents []byte,
	localPath string,
	size int64,
	partSize int64,
) ([]dynamicpdbapi.CompletedFileUploadPart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	if normalizeConcurrency(u.partConcurrency) == 1 || len(parts) == 1 {
		return u.uploadPartsSequentially(ctx, parts, contents, localPath, size, partSize)
	}
	return u.uploadPartsConcurrently(ctx, parts, contents, localPath, size, partSize)
}

func (u *Uploader) uploadPartsSequentially(
	ctx context.Context,
	parts []dynamicpdbapi.FileUploadPart,
	contents []byte,
	localPath string,
	size int64,
	partSize int64,
) ([]dynamicpdbapi.CompletedFileUploadPart, error) {
	completedParts := make([]dynamicpdbapi.CompletedFileUploadPart, 0, len(parts))
	for _, part := range parts {
		completed, err := u.uploadPart(ctx, part, contents, localPath, size, partSize)
		if err != nil {
			return nil, err
		}
		completedParts = append(completedParts, completed)
	}
	return completedParts, nil
}

func (u *Uploader) uploadPartsConcurrently(
	ctx context.Context,
	parts []dynamicpdbapi.FileUploadPart,
	contents []byte,
	localPath string,
	size int64,
	partSize int64,
) ([]dynamicpdbapi.CompletedFileUploadPart, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type partResult struct {
		part dynamicpdbapi.CompletedFileUploadPart
		err  error
	}

	jobs := make(chan dynamicpdbapi.FileUploadPart)
	results := make(chan partResult, len(parts))
	workers := min(normalizeConcurrency(u.partConcurrency), len(parts))
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for part := range jobs {
				completed, err := u.uploadPart(ctx, part, contents, localPath, size, partSize)
				if err != nil {
					results <- partResult{err: err}
					cancel()
					return
				}
				results <- partResult{part: completed}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, part := range parts {
			select {
			case jobs <- part:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		waitGroup.Wait()
		close(results)
	}()

	completedParts := make([]dynamicpdbapi.CompletedFileUploadPart, 0, len(parts))
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		completedParts = append(completedParts, result.part)
	}
	sort.Slice(completedParts, func(left int, right int) bool {
		return completedParts[left].PartNumber < completedParts[right].PartNumber
	})
	return completedParts, nil
}

func (u *Uploader) uploadPart(
	ctx context.Context,
	part dynamicpdbapi.FileUploadPart,
	contents []byte,
	localPath string,
	size int64,
	partSize int64,
) (dynamicpdbapi.CompletedFileUploadPart, error) {
	etag, err := uploadPartWithRetry(ctx, u.dynamicPDBClient, part, contents, localPath, size, partSize)
	if err != nil {
		return dynamicpdbapi.CompletedFileUploadPart{}, err
	}
	return dynamicpdbapi.CompletedFileUploadPart{
		PartNumber: part.PartNumber,
		ETag:       etag,
	}, nil
}

func uploadPartSize(payloadSize int64, partSize int64, partNumber int) int64 {
	offset := int64(partNumber-1) * partSize
	size := min(partSize, payloadSize-offset)
	if size < 0 {
		return 0
	}
	return size
}

func uploadPartWithRetry(
	ctx context.Context,
	dynamicPDBClient dynamicpdbapi.Client,
	part dynamicpdbapi.FileUploadPart,
	contents []byte,
	localPath string,
	payloadSize int64,
	partSize int64,
) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= uploadPartAttempts; attempt++ {
		reader, size, closeReader, err := partReader(contents, localPath, payloadSize, partSize, part.PartNumber)
		if err != nil {
			return "", err
		}
		etag, uploadErr := dynamicPDBClient.PutUploadPart(ctx, part.URL, reader, size)
		closeErr := closeReader()
		if uploadErr == nil && closeErr == nil {
			return etag, nil
		}
		if uploadErr != nil {
			lastErr = uploadErr
		}
		if closeErr != nil {
			lastErr = closeErr
		}
		if !shouldRetryUploadPart(ctx, lastErr, attempt) {
			return "", lastErr
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return "", lastErr
}

func shouldRetryUploadPart(ctx context.Context, err error, attempt int) bool {
	if err == nil || attempt >= uploadPartAttempts {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	var dynamicPDBError *dynamicpdbapi.Error
	if errors.As(err, &dynamicPDBError) && dynamicPDBError.Status < 500 {
		return dynamicPDBError.Code == "RequestTimeout"
	}
	return true
}

func partReader(contents []byte, localPath string, payloadSize int64, partSize int64, partNumber int) (io.Reader, int64, func() error, error) {
	offset := int64(partNumber-1) * partSize
	size := uploadPartSize(payloadSize, partSize, partNumber)
	if size < 0 {
		return nil, 0, nil, fmt.Errorf("part %d starts beyond payload size", partNumber)
	}
	if offset > payloadSize {
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
	extensions := map[string]any{}
	metadata, upgraded := metadata.WithCoordinateCofactors()
	if upgraded {
		u.legacyManifestWarning.Do(func() {
			slog.Warn("manifest was saved before cofactors existed: reading cofactors and review flags from the coordinates file so HEM and metal ions are not dropped; regenerate the manifest to make this permanent")
		})
	}
	for key, field := range metadata {
		value, ok, err := u.fieldValue(ctx, dataRoot, pdbID, field, artifacts)
		if err != nil {
			return nil, fmt.Errorf("extract model metadata %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if name, nested := strings.CutPrefix(key, modelExtensionKeyPrefix); nested {
			extensions[name] = value
			continue
		}
		result[key] = toCanonicalModelMetadataValue(key, value)
	}
	if len(extensions) > 0 {
		merged, err := mergeModelExtensions(result["extensions"], extensions)
		if err != nil {
			return nil, err
		}
		result["extensions"] = merged
	}
	return result, nil
}

// mergeModelExtensions adds the extracted extension keys to the manifest's own
// "extensions" object. A key the manifest already sets is never replaced.
func mergeModelExtensions(own any, extracted map[string]any) (map[string]any, error) {
	if own == nil {
		return extracted, nil
	}
	ownObject, ok := own.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("model metadata extensions must be an object, got %T", own)
	}
	merged := make(map[string]any, len(ownObject)+len(extracted))
	for key, value := range extracted {
		merged[key] = value
	}
	for key, value := range ownObject {
		merged[key] = value
	}
	return merged, nil
}

// modelExtensionKeyPrefix marks a manifest model metadata key whose value is
// uploaded under the model's open "extensions" object, for data the model
// schema has no declared field for.
const modelExtensionKeyPrefix = "extensions."

func toCanonicalModelMetadataValue(key string, value any) any {
	switch key {
	case "atom_count", "modeled_residues", "unique_protein_chains":
		if number, ok := integerValue(value); ok {
			return number
		}
	case "altloc_fraction", "unmodeled_fraction":
		if number, ok := floatValue(value); ok {
			return number
		}
	case "ligands":
		return rcsbLigands(appendStringValues(nil, value))
	case "cofactors":
		return canonicalModelComponents(appendStringValues(nil, value))
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

// modelMetrics reads each metric of the manifest. A value read from the coordinates
// file links to coordinatesRunID, the run of the program that wrote that file; a
// value whose extraction names its software carries that software.
func (u *Uploader) modelMetrics(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	metrics manifest.Metrics,
	artifacts map[string]extractorapi.Artifact,
	coordinatesRunID *string,
) ([]modelMetric, error) {
	values := make([]modelMetric, 0, len(metrics))
	for _, key := range metricKeys(metrics) {
		field := metrics[key]
		value, extraction, ok, err := u.fieldValueAndExtraction(ctx, dataRoot, pdbID, field, artifacts)
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
		request := dynamicpdbapi.CreateMetricRequest{
			ID:    uuid.NewString(),
			Key:   key,
			Value: number,
		}
		if extraction.Software != nil {
			request.SoftwareName = stringPtr(extraction.Software.Name)
			request.SoftwareVersion = stringPtr(extraction.Software.Version)
		}
		fromCoordinates := isCoordinatesArtifact(extraction.Source.Artifact)
		if fromCoordinates {
			request.RunID = coordinatesRunID
		}
		values = append(values, modelMetric{Request: request, FromCoordinates: fromCoordinates})
	}
	return values, nil
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
	value, _, ok, err := u.fieldValueAndExtraction(ctx, dataRoot, pdbID, fields, artifacts)
	return value, ok, err
}

// fieldValueAndExtraction returns the first value found by fields, in order, and the
// extraction that found it.
func (u *Uploader) fieldValueAndExtraction(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	fields []manifest.FieldExtraction,
	artifacts map[string]extractorapi.Artifact,
) (any, manifest.FieldExtraction, bool, error) {
	for _, field := range fields {
		value, ok, err := u.fieldExtractionValue(ctx, dataRoot, pdbID, field, artifacts)
		if err != nil || fieldValuePresent(value, ok) {
			return value, field, ok, err
		}
	}
	return nil, manifest.FieldExtraction{}, false, nil
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
		return artifactextractor.NewCachedFieldExtractor(artifacts, u.structureFields).Extract(ctx, pdbID, source, field.Extract)
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

func appliedUploadFilter(filter manifest.Filter, options ...Options) manifest.Filter {
	merged := filter
	for _, option := range options {
		if option.OverrideInclude {
			merged.Include = append([]string(nil), option.Include...)
		}
		if option.OverrideSkip {
			merged.Skip = append([]string(nil), option.Skip...)
		}
	}
	return merged
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
	pdbID, ok := entry.ExternalRefs["pdb"]
	if !ok {
		return "", false
	}
	pdbID = canonicalPDBID(pdbID)
	return pdbID, pdbID != ""
}

func entryProperties(metadata map[string]any) (dynamicpdbapi.EntryProperties, error) {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return dynamicpdbapi.EntryProperties{}, fmt.Errorf("encode entry properties: %w", err)
	}
	var properties dynamicpdbapi.EntryProperties
	if err := json.Unmarshal(encoded, &properties); err != nil {
		return dynamicpdbapi.EntryProperties{}, fmt.Errorf("decode entry properties: %w", err)
	}
	return properties, nil
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

func entryTitle(metadata map[string]any) *string {
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

func (u *Uploader) modelProgram(ctx context.Context, pdbID string, artifacts map[string]extractorapi.Artifact) (*modelProgramInfo, error) {
	extractor := artifactextractor.NewCachedFieldExtractor(artifacts, u.structureFields)
	name, ok, err := extractor.Extract(ctx, pdbID, manifest.Source{Artifact: coordinatesArtifactID}, structureExtract("program.name"))
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
	version, ok, err := extractor.Extract(ctx, pdbID, manifest.Source{Artifact: coordinatesArtifactID}, structureExtract("program.version"))
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

// coordinatesArtifactID is the manifest id of a model's coordinates file.
const coordinatesArtifactID = "coordinates"

func isCoordinatesArtifact(manifestID string) bool {
	return strings.EqualFold(strings.TrimSpace(manifestID), coordinatesArtifactID)
}

// coordinatesRunID returns the id of the model's one run, or nil when it has none.
func coordinatesRunID(runs []dynamicpdbapi.CreateRunRequest) *string {
	if len(runs) != 1 {
		return nil
	}
	return &runs[0].ID
}

// runArtifactDirection is the default direction rule: coordinates and log_* files
// are what the program wrote; mtz_*, structure_factors_* and density files what it
// read. Other artifacts are not linked to the run.
func runArtifactDirection(artifact artifactRef) (string, bool) {
	id := strings.ToLower(strings.TrimSpace(artifact.ManifestID))
	format := strings.ToLower(strings.TrimSpace(artifact.Format))
	switch {
	case id == coordinatesArtifactID || strings.HasPrefix(id, "log_"):
		return runDirectionOutput, true
	case strings.HasPrefix(id, "mtz_") || strings.HasPrefix(id, "structure_factors_"):
		return runDirectionInput, true
	case format == "mtz" || format == "structure_factors_cif" || format == "map" || format == "ccp4" || format == "mrc":
		return runDirectionInput, true
	default:
		return "", false
	}
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

func canonicalModelComponents(components []string) []string {
	values := []string{}
	for _, component := range components {
		for _, value := range strings.FieldsFunc(component, ligandSeparator) {
			values = appendUniqueNonEmptyString(values, strings.ToUpper(value))
		}
	}
	return values
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

// artifactFormat returns the artifact's format in lowercase, as the schema requires.
func artifactFormat(artifact manifest.Artifact, payload extractorapi.Artifact) string {
	format := strings.TrimSpace(artifact.Format)
	if format == "" {
		format = payload.Format
	}
	return strings.ToLower(format)
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

// isEntryCheckError reports whether err is an entry's schema check failure, found
// before anything of the entry was uploaded.
func isEntryCheckError(err error) bool {
	var checkError *EntryCheckError
	return errors.As(err, &checkError)
}
