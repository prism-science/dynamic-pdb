package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/cif"
)

// artifactExtractionReadLimit bounds the ranged GET used to read an uploaded
// structure file. Archive files list entities, connections and mutations before
// the coordinates, so this covers them for files of any size.
const artifactExtractionReadLimit = 4 * 1024 * 1024

const artifactExtractionDataType = ArtifactExtractionDataType("artifact_extractions")

func (s *Server) ExtractArtifact(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	artifacts, err := s.database.Artifacts.List(r.Context(), db.ArtifactFilters{ID: &id})
	if err != nil {
		slog.ErrorContext(r.Context(), "load artifact for extraction failed", "err", err, "artifact_id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load artifact")
		return
	}
	if len(artifacts) == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "artifact not found")
		return
	}
	artifact := artifacts[0]
	if !s.canExtractArtifact(w, r, user, artifact) {
		return
	}
	source, problem := artifactExtractionSource(artifact)
	if problem != "" {
		writeError(w, http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", problem)
		return
	}

	head, err := s.fileCDN.ReadArtifactHead(r.Context(), artifact.ID.String(), *artifact.URI, artifactExtractionReadLimit)
	switch {
	case errors.Is(err, cdn.ErrObjectNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "nothing is stored at the artifact URI")
		return
	case errors.Is(err, cdn.ErrInvalidObjectURL):
		writeError(w, http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", "artifact URI is not an uploaded file of this artifact")
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "read artifact head failed", "err", err, "artifact_id", id)
		writeError(w, http.StatusBadGateway, "BAD_GATEWAY", "failed to read the artifact from storage")
		return
	}

	attributes, err := buildArtifactExtraction(head.Data, head.TotalSize, source)
	if err != nil {
		slog.ErrorContext(r.Context(), "extract artifact values failed", "err", err, "artifact_id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to extract values")
		return
	}
	writeJSON(w, http.StatusOK, ArtifactExtractionDocument{
		Data: ArtifactExtractionData{
			Type:       artifactExtractionDataType,
			Id:         id,
			Attributes: attributes,
		},
	})
}

// canExtractArtifact allows the artifact's creator and reviewers.
func (s *Server) canExtractArtifact(
	w http.ResponseWriter,
	r *http.Request,
	user *models.User,
	artifact models.Artifact,
) bool {
	if artifact.CreatedBy == user.ID {
		return true
	}
	allowed, err := s.authorizer.HasRole(r.Context(), user.ID, models.RoleKeyReviewer)
	if err != nil {
		slog.ErrorContext(r.Context(), "artifact extraction authorization failed", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authorize request")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the artifact's creator or a reviewer can extract values")
		return false
	}
	return true
}

// artifactExtractionSource returns the `file:<sha256>` source tag, or why the
// artifact cannot be extracted.
func artifactExtractionSource(artifact models.Artifact) (string, string) {
	if artifact.URI == nil || strings.TrimSpace(*artifact.URI) == "" {
		return "", "artifact has no uploaded file"
	}
	if artifact.Format == nil || !isMMCIFFormat(*artifact.Format) {
		return "", "only mmCIF artifacts can be extracted"
	}
	if artifact.SHA256 == nil || strings.TrimSpace(*artifact.SHA256) == "" {
		return "", "artifact has no sha256"
	}
	return "file:" + *artifact.SHA256, ""
}

func isMMCIFFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "cif", "mmcif":
		return true
	default:
		return false
	}
}

type atomValues struct {
	atomCount       int
	modeledResidues int
	proteinChains   int
	altlocFraction  float64
}

// buildArtifactExtraction parses the head of an mmCIF file and returns the
// extracted values. Values that cannot be extracted or break the schema rules
// become warnings.
func buildArtifactExtraction(data []byte, totalSize int64, source string) (ArtifactExtractionAttributes, error) {
	truncated := totalSize > int64(len(data))
	var document *cif.Document
	if truncated {
		document = cif.ParseTruncated(string(data))
	} else {
		document = cif.Parse(string(data))
	}
	result, err := cif.Extract(document)
	if err != nil {
		return ArtifactExtractionAttributes{}, fmt.Errorf("extract mmCIF values: %w", err)
	}

	values := []ExtractedValue{}
	warnings := []ExtractionWarning{}
	for _, detail := range result.Warnings {
		warnings = append(warnings, ExtractionWarning{Detail: detail})
	}

	var atoms atomValues
	if result.Atoms != nil {
		atoms = atomValues{
			atomCount:       result.Atoms.AtomCount,
			modeledResidues: result.Atoms.ModeledResidues,
			proteinChains:   result.Atoms.ProteinChains,
			altlocFraction:  result.Atoms.AltlocFraction,
		}
		values = append(values,
			ExtractedValue{Name: "atom_count", Value: atoms.atomCount, Source: source},
			ExtractedValue{Name: "modeled_residues", Value: atoms.modeledResidues, Source: source},
			ExtractedValue{Name: "unique_protein_chains", Value: atoms.proteinChains, Source: source},
			ExtractedValue{Name: "altloc_fraction", Value: atoms.altlocFraction, Source: source},
		)
	} else if len(result.Warnings) == 0 {
		warnings = append(warnings, ExtractionWarning{Detail: "no atom_site records found: atom counts, residue counts and altloc fraction not extracted"})
	}

	if result.Atoms != nil || len(result.Ligands) > 0 {
		values = append(values, compoundsValue("ligands", result.Ligands, source))
	}
	if result.Atoms != nil || len(result.Cofactors) > 0 {
		values = append(values, compoundsValue("cofactors", result.Cofactors, source))
	}
	if len(result.Mutations) > 0 {
		values = append(values, mutationsValue(result.Mutations, source))
	}
	if result.Program.Name != "" {
		values = append(values, ExtractedValue{Name: "program_name", Value: result.Program.Name, Source: source})
	}
	if result.Program.Version != "" {
		values = append(values, ExtractedValue{Name: "program_version", Value: result.Program.Version, Source: source})
	}

	values, ruleWarnings, err := applySchemaRules(values, atoms)
	if err != nil {
		return ArtifactExtractionAttributes{}, fmt.Errorf("apply schema rules: %w", err)
	}
	warnings = append(warnings, ruleWarnings...)

	return ArtifactExtractionAttributes{
		Source:           source,
		ReferenceVersion: result.ReferenceVersion,
		BytesRead:        int64(len(data)),
		ObjectSize:       max(totalSize, int64(len(data))),
		Truncated:        truncated,
		Values:           values,
		Warnings:         warnings,
	}, nil
}

func compoundsValue(name string, compounds []cif.Compound, source string) ExtractedValue {
	value := ExtractedValue{Name: name, Value: compounds, Source: source}
	var reasons []string
	for _, compound := range compounds {
		if compound.NeedsReview {
			value.NeedsReview = true
			reasons = appendUnique(reasons, compound.ReviewReason)
		}
	}
	value.ReviewReason = joinedReason(reasons)
	return value
}

func mutationsValue(sets []cif.MutationSet, source string) ExtractedValue {
	value := ExtractedValue{Name: "mutations", Value: sets, Source: source}
	var reasons []string
	for _, set := range sets {
		if set.NeedsReview {
			value.NeedsReview = true
			reasons = appendUnique(reasons, set.ReviewReason)
		}
	}
	value.ReviewReason = joinedReason(reasons)
	return value
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func joinedReason(reasons []string) *string {
	if len(reasons) == 0 {
		return nil
	}
	return ptr(strings.Join(reasons, "; "))
}

// applySchemaRules checks the extracted counts and fractions with the same
// rules writes use. A value that breaks a rule is dropped and reported.
func applySchemaRules(values []ExtractedValue, atoms atomValues) ([]ExtractedValue, []ExtractionWarning, error) {
	metadata := models.ModelMetadata{
		AtomCount:           &atoms.atomCount,
		ModeledResidues:     &atoms.modeledResidues,
		UniqueProteinChains: &atoms.proteinChains,
		AltLocFraction:      &atoms.altlocFraction,
	}
	fieldErrors := validation.ModelMetadata(validation.Root, metadata)
	if len(fieldErrors) == 0 {
		return values, []ExtractionWarning{}, nil
	}
	dropped := map[string]bool{}
	warnings := make([]ExtractionWarning, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		name, err := extractedValueName(fieldError.Pointer)
		if err != nil {
			return nil, nil, err
		}
		dropped[name] = true
		warnings = append(warnings, ExtractionWarning{
			Field:   ptr(name),
			Pointer: ptr(string(fieldError.Pointer)),
			Code:    ptr(string(fieldError.Code)),
			Detail:  fieldError.Detail,
		})
	}
	kept := make([]ExtractedValue, 0, len(values))
	for _, value := range values {
		if !dropped[value.Name] {
			kept = append(kept, value)
		}
	}
	return kept, warnings, nil
}

func extractedValueName(pointer validation.Pointer) (string, error) {
	switch pointer {
	case validation.Root.Field("atom_count"):
		return "atom_count", nil
	case validation.Root.Field("modeled_residues"):
		return "modeled_residues", nil
	case validation.Root.Field("unique_protein_chains"):
		return "unique_protein_chains", nil
	case validation.Root.Field("altloc_fraction"):
		return "altloc_fraction", nil
	default:
		return "", fmt.Errorf("unknown extracted value for pointer: %s", pointer)
	}
}
