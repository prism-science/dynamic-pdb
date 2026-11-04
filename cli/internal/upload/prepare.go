package upload

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	extractorapi "dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

// An entry is uploaded in two steps. Preparing reads every file and RCSB value the
// entry needs, gives each artifact, run and metric its id, and checks the result
// against the API's schema rules; nothing is written anywhere. Only an entry that
// passes is recorded as "uploading" in the state file and has its files uploaded.

// EntryCheckError lists why an entry would not pass the API's schema checks. It is
// found while preparing, so nothing of the entry was uploaded.
type EntryCheckError struct {
	PDBID    string
	Problems []string
}

func (e *EntryCheckError) Error() string {
	return fmt.Sprintf("%s would not pass the schema checks, so nothing of it was uploaded (%s):\n  %s",
		e.PDBID, countNoun(len(e.Problems), "problem"), strings.Join(e.Problems, "\n  "))
}

// preparedArtifact is a manifest artifact resolved to its bytes or URI, with the id
// it will have in the API.
type preparedArtifact struct {
	Ref     artifactRef
	Name    string
	Level   string
	Type    string
	Payload extractorapi.Artifact
}

// request returns the artifact's API request; uri is where its bytes live.
func (a preparedArtifact) request(uri string) dynamicpdbapi.CreateArtifactRequest {
	return dynamicpdbapi.CreateArtifactRequest{
		ID:        a.Ref.ArtifactID,
		Name:      a.Name,
		Level:     a.Level,
		Type:      a.Type,
		URI:       stringPtr(uri),
		SHA256:    stringPtr(a.Payload.SHA256),
		Format:    stringPtr(a.Ref.Format),
		SizeBytes: int64Ptr(a.Payload.Size),
		Metadata:  a.Payload.Metadata,
	}
}

// preparedModel is a manifest model with every value resolved and checked.
type preparedModel struct {
	// UploadModelID names the model in file upload requests.
	UploadModelID     string
	Title             string
	Metadata          map[string]any
	IdempotencyKey    *string
	PrimaryArtifactID string
	Artifacts         []preparedArtifact
	Runs              []dynamicpdbapi.CreateRunRequest
	Metrics           []dynamicpdbapi.CreateMetricRequest
}

// preparedEntry is an entry with every value resolved and checked. For an entry that
// already exists only Models is set.
type preparedEntry struct {
	// UploadEntryID names a new entry in file upload requests.
	UploadEntryID string
	Title         string
	Properties    dynamicpdbapi.EntryProperties
	PreviewImage  *extractorapi.Image
	Artifacts     []preparedArtifact
	Models        []preparedModel
}

// prepareEntry resolves and checks the entry for pdbID: the entry itself when
// existingEntry is nil, and its models. It returns an *EntryCheckError listing
// every schema problem, or another error when a file or RCSB value cannot be read.
func (u *Uploader) prepareEntry(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbID string,
	existingEntry *dynamicpdbapi.Entry,
) (preparedEntry, error) {
	var prepared preparedEntry
	var problems []string
	if existingEntry == nil {
		newEntry, entryProblems, err := u.prepareNewEntry(ctx, entryTemplate, dataRoot, pdbID)
		if err != nil {
			return preparedEntry{}, err
		}
		prepared = newEntry
		problems = append(problems, entryProblems...)
	}
	models, modelProblems, err := u.prepareModels(ctx, entryTemplate.Models, dataRoot, pdbID)
	if err != nil {
		return preparedEntry{}, err
	}
	prepared.Models = models
	problems = append(problems, modelProblems...)
	if len(problems) > 0 {
		return preparedEntry{}, &EntryCheckError{PDBID: canonicalPDBID(pdbID), Problems: problems}
	}
	return prepared, nil
}

func (u *Uploader) prepareNewEntry(
	ctx context.Context,
	entryTemplate manifest.Entry,
	dataRoot string,
	pdbID string,
) (preparedEntry, []string, error) {
	entryPDBID := canonicalPDBID(pdbID)
	metadata, err := u.entryMetadata(ctx, dataRoot, entryTemplate.Metadata, entryPDBID)
	if err != nil {
		return preparedEntry{}, nil, err
	}
	properties, err := entryProperties(metadata)
	if err != nil {
		return preparedEntry{}, nil, err
	}
	title := strings.ReplaceAll(entryTemplate.Title, templatePDBID, entryPDBID)
	if extractedTitle := entryTitle(metadata); extractedTitle != nil {
		title = *extractedTitle
	}
	if strings.TrimSpace(title) == "" {
		title = entryPDBID
	}
	previewImage, err := u.previewImage(ctx, dataRoot, entryTemplate.PreviewImage, pdbID)
	if err != nil {
		return preparedEntry{}, nil, err
	}
	artifacts := make([]preparedArtifact, 0, len(entryTemplate.Artifacts))
	for _, artifact := range entryTemplate.Artifacts {
		prepared, ok, err := u.prepareArtifact(ctx, dataRoot, pdbID, artifact, false)
		if err != nil {
			return preparedEntry{}, nil, err
		}
		if ok {
			artifacts = append(artifacts, prepared)
		}
	}
	return preparedEntry{
		UploadEntryID: uuid.NewString(),
		Title:         title,
		Properties:    properties,
		PreviewImage:  previewImage,
		Artifacts:     artifacts,
	}, newEntryProblems(properties), nil
}

// prepareModels prepares every model that has coordinates. Each schema problem
// starts with the manifest id of its model.
func (u *Uploader) prepareModels(
	ctx context.Context,
	models []manifest.ModelPattern,
	dataRoot string,
	pdbID string,
) ([]preparedModel, []string, error) {
	prepared := make([]preparedModel, 0, len(models))
	var problems []string
	for _, model := range models {
		preparedModel, modelProblems, ok, err := u.prepareModel(ctx, dataRoot, pdbID, model)
		if err != nil {
			return nil, nil, err
		}
		for _, problem := range modelProblems {
			problems = append(problems, fmt.Sprintf("model %q: %s", model.ID, problem))
		}
		if ok {
			prepared = append(prepared, preparedModel)
		}
	}
	return prepared, problems, nil
}

// prepareModel returns ok false for a model without coordinates or without any
// artifact, which is skipped as before.
func (u *Uploader) prepareModel(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	model manifest.ModelPattern,
) (preparedModel, []string, bool, error) {
	hasCoordinates, err := u.hasModelCoordinates(ctx, dataRoot, pdbID, model)
	if err != nil {
		return preparedModel{}, nil, false, err
	}
	if !hasCoordinates {
		return preparedModel{}, nil, false, nil
	}
	artifacts := make([]preparedArtifact, 0, len(model.Artifacts))
	payloads := make(map[string]extractorapi.Artifact, len(model.Artifacts))
	for _, artifact := range model.Artifacts {
		prepared, ok, err := u.prepareArtifact(ctx, dataRoot, pdbID, artifact, true)
		if err != nil {
			return preparedModel{}, nil, false, err
		}
		if ok {
			artifacts = append(artifacts, prepared)
			payloads[artifact.ID] = prepared.Payload
		}
	}
	if len(artifacts) == 0 {
		return preparedModel{}, nil, false, nil
	}

	metadata, err := u.modelMetadata(ctx, dataRoot, pdbID, model.Metadata, payloads)
	if err != nil {
		return preparedModel{}, nil, false, err
	}
	if strings.TrimSpace(model.ModelType) != "" {
		metadata["model_type"] = strings.TrimSpace(model.ModelType)
	}
	if strings.TrimSpace(model.Purpose) != "" {
		metadata["purpose"] = strings.TrimSpace(model.Purpose)
	}
	program, err := u.modelProgram(ctx, pdbID, payloads)
	if err != nil {
		return preparedModel{}, nil, false, err
	}
	draft := newRunDraft(model.Run, artifactRefs(artifacts), program)
	runs := draft.runs()
	metrics, err := u.modelMetrics(ctx, dataRoot, pdbID, model.Metrics, payloads, coordinatesRunID(runs))
	if err != nil {
		return preparedModel{}, nil, false, err
	}

	title := strings.TrimSpace(model.Title)
	if title == "" {
		title = model.ID
	}
	return preparedModel{
		UploadModelID:     uuid.NewString(),
		Title:             title,
		Metadata:          metadata,
		IdempotencyKey:    stringPtr(artifacts[0].Payload.SHA256),
		PrimaryArtifactID: artifacts[0].Ref.ArtifactID,
		Artifacts:         artifacts,
		Runs:              runs,
		Metrics:           metricRequests(metrics),
	}, computedModelProblems(model.ModelType, draft, metrics), true, nil
}

// prepareArtifact resolves an artifact's bytes or URI and gives it an id. It
// returns ok false when the artifact's source yields nothing.
func (u *Uploader) prepareArtifact(
	ctx context.Context,
	dataRoot string,
	pdbID string,
	artifact manifest.Artifact,
	attachedToModel bool,
) (preparedArtifact, bool, error) {
	payload, ok, err := u.artifactPayload(ctx, dataRoot, pdbID, artifact)
	if err != nil {
		return preparedArtifact{}, false, err
	}
	if !ok {
		return preparedArtifact{}, false, nil
	}
	name, err := artifactName(artifact, payload)
	if err != nil {
		return preparedArtifact{}, false, err
	}
	level := strings.TrimSpace(artifact.Level)
	if level == "" {
		level = "L1"
	}
	return preparedArtifact{
		Ref: artifactRef{
			ManifestID: artifact.ID,
			ArtifactID: uuid.NewString(),
			Format:     artifactFormat(artifact, payload),
		},
		Name:    name,
		Level:   level,
		Type:    artifactTypeForRequest(artifact, attachedToModel),
		Payload: payload,
	}, true, nil
}

// previewImage resolves the entry's preview image, or returns nil when the
// manifest names none or its source yields nothing.
func (u *Uploader) previewImage(
	ctx context.Context,
	dataRoot string,
	previewImage *manifest.EntryPreviewImage,
	pdbID string,
) (*extractorapi.Image, error) {
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
	return &image, nil
}

func artifactRefs(artifacts []preparedArtifact) []artifactRef {
	refs := make([]artifactRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		refs = append(refs, artifact.Ref)
	}
	return refs
}

func countNoun(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
