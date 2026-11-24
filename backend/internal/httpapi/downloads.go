package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

const (
	downloadsOffCode          = "DOWNLOADS_OFF"
	downloadsBusyCode         = "DOWNLOADS_BUSY"
	selectionBusyCode         = "SELECTION_BUSY"
	downloadNotPreparableCode = "DOWNLOAD_NOT_PREPARABLE"
	downloadNotRetryableCode  = "DOWNLOAD_NOT_RETRYABLE"
	nothingToRetryCode        = "NOTHING_TO_RETRY"
	downloadRetryAction       = "retry archive"
	downloadNotFoundDetail    = "archive request not found"
	downloadCreateAction      = "request archive"
	downloadPrepareAction     = "prepare archive"
	// downloadsBusyRetryAfter is how long a client waits when archives are being built: about the
	// time the worker takes for a small archive.
	downloadsBusyRetryAfter = time.Minute
	// downloadSmallerSelectionAdvice ends the message of a blocked archive.
	downloadSmallerSelectionAdvice = "Choose fewer models or a smaller file choice, or download the files " +
		"with the scripted download, which has no size limit."
)

// DownloadsPolicy configures bulk-download archives (/v1/downloads).
type DownloadsPolicy struct {
	// Enabled turns the routes on. Off, they answer 404 with code DOWNLOADS_OFF.
	Enabled bool
	// MaxArchiveBytes is the most bytes the files of one archive may add up to.
	MaxArchiveBytes int64
	// MaxUnfinished caps the unfinished archives of all selections together (503 above it).
	MaxUnfinished int
	// MaxUnfinishedPerSelection caps the unfinished archives of one selection (429 above it).
	MaxUnfinishedPerSelection int
	// Requests and InternalRequests cap archive requests per client per Window, as for selections.
	Requests         int
	InternalRequests int
	Window           time.Duration
	TrustedProxies   []netip.Prefix
}

// downloads is the server's downloads setup; the zero value has the routes off.
type downloads struct {
	enabled                   bool
	maxArchiveBytes           int64
	maxUnfinished             int
	maxUnfinishedPerSelection int
	limiter                   *routeRateLimiter
	now                       func() time.Time
}

// caps are the database's caps on unfinished archives.
func (d downloads) caps() db.DownloadCaps {
	return db.DownloadCaps{MaxUnfinished: d.maxUnfinished, MaxUnfinishedPerSelection: d.maxUnfinishedPerSelection}
}

// WithDownloads configures bulk-download archives.
func (s *Server) WithDownloads(policy DownloadsPolicy) *Server {
	s.bulkDownloads = downloads{
		enabled:                   policy.Enabled,
		maxArchiveBytes:           policy.MaxArchiveBytes,
		maxUnfinished:             policy.MaxUnfinished,
		maxUnfinishedPerSelection: policy.MaxUnfinishedPerSelection,
		limiter: newRouteRateLimiter(policy.Requests, policy.InternalRequests, policy.Window, policy.TrustedProxies,
			fmt.Sprintf("too many archive requests from this client: each client may ask for %d archives per %s. "+
				"Wait the number of seconds in Retry-After", policy.Requests, policy.Window)),
		now: func() time.Time { return time.Now().UTC() },
	}
	return s
}

func writeDownloadsOff(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, downloadsOffCode, "bulk-download archives are turned off on this server")
}

// createDownloadBody is CreateDownloadRequest with every value optional and as sent, so a
// missing or malformed value gets a field error instead of a 400.
type createDownloadBody struct {
	SelectionID *string         `json:"selection_id"`
	FileChoice  *string         `json:"file_choice"`
	Prepare     json.RawMessage `json:"prepare"`
}

// CreateDownload asks for an archive of a saved selection's files, or returns the selection's
// archive of the same file choice that is waiting, being built or ready.
func (s *Server) CreateDownload(w http.ResponseWriter, r *http.Request) {
	if !s.bulkDownloads.enabled {
		writeDownloadsOff(w)
		return
	}
	if s.bulkDownloads.limiter.respondOnLimit(w, r) {
		return
	}
	var body createDownloadBody
	_, rawBody, err := decodeRequestBody(r, &body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	keysErr := s.checkSchema(r, requestKeyErrors(rawBody, CreateDownloadRequest{}), nil)
	if s.writeMutationError(r.Context(), w, keysErr, downloadCreateAction) {
		return
	}
	selectionID, choice, prepare, fieldErrors := downloadRequestValues(body)
	if len(fieldErrors) > 0 {
		writeFieldErrors(w, fieldErrors)
		return
	}
	job, created, err := s.database.CreateDownload(r.Context(), db.DownloadCreate{
		ID:              uuid.New(),
		SelectionID:     selectionID,
		FileChoice:      choice,
		MaxArchiveBytes: s.bulkDownloads.maxArchiveBytes,
		Prepare:         prepare,
		DownloadCaps:    s.bulkDownloads.caps(),
		Now:             s.bulkDownloads.now(),
	})
	if err != nil {
		s.writeDownloadCreateError(w, r, err)
		return
	}
	document, err := s.downloadDocument(*job)
	if err != nil {
		slog.ErrorContext(r.Context(), "build download document failed", "err", err, "download_id", job.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+downloadCreateAction)
		return
	}
	if !created {
		writeJSON(w, http.StatusOK, document)
		return
	}
	w.Header().Set("Location", document.Data.Links.Self)
	writeJSON(w, http.StatusAccepted, document)
}

// downloadRequestValues reads an archive request's selection ID, file choice and prepare, or the
// field errors of the values that are missing or invalid. Without prepare (or null) the archive is
// built once planned, as before previews existed.
func downloadRequestValues(
	body createDownloadBody,
) (uuid.UUID, domainmodels.DownloadFileChoice, bool, []validation.FieldError) {
	var fieldErrors []validation.FieldError
	prepare := true
	if raw := strings.TrimSpace(string(body.Prepare)); raw != "" && raw != "null" {
		if err := json.Unmarshal(body.Prepare, &prepare); err != nil {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: validation.Root.Field("prepare"), Code: validation.CodeInvalidValue,
				Detail: "prepare is true (build the archive) or false (stop at its preview)",
			})
		}
	}
	selectionID := uuid.Nil
	switch {
	case body.SelectionID == nil || strings.TrimSpace(*body.SelectionID) == "":
		fieldErrors = append(fieldErrors, validation.FieldError{
			Pointer: validation.Root.Field("selection_id"), Code: validation.CodeRequired,
			Detail: "give the ID of a saved selection",
		})
	default:
		parsed, err := uuid.Parse(strings.TrimSpace(*body.SelectionID))
		if err != nil {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: validation.Root.Field("selection_id"), Code: validation.CodeInvalidValue,
				Detail: "a selection ID is a UUID, as POST /v1/selections returned it",
			})
		} else {
			selectionID = parsed
		}
	}
	var choice domainmodels.DownloadFileChoice
	switch {
	case body.FileChoice == nil || strings.TrimSpace(*body.FileChoice) == "":
		fieldErrors = append(fieldErrors, validation.FieldError{
			Pointer: validation.Root.Field("file_choice"), Code: validation.CodeRequired,
			Detail: "give a file choice: original_mmcif, modified_coordinates or all_files",
		})
	default:
		choice = domainmodels.DownloadFileChoice(strings.TrimSpace(*body.FileChoice))
		if err := choice.Validate(); err != nil {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: validation.Root.Field("file_choice"), Code: validation.CodeInvalidValue, Detail: err.Error(),
			})
		}
	}
	return selectionID, choice, prepare, fieldErrors
}

// writeDownloadCreateError answers an archive request the database refused or could not save.
func (s *Server) writeDownloadCreateError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, db.ErrDownloadSelectionNotFound) {
		writeFieldErrors(w, []validation.FieldError{{
			Pointer: validation.Root.Field("selection_id"), Code: validation.CodeInvalidValue,
			Detail: "no saved selection has this ID",
		}})
		return
	}
	s.writeDownloadBusyError(w, r, err, downloadCreateAction)
}

// PrepareDownload asks for the archive of a request whose preview was shown.
func (s *Server) PrepareDownload(w http.ResponseWriter, r *http.Request, downloadID string) {
	if !s.bulkDownloads.enabled {
		writeDownloadsOff(w)
		return
	}
	if s.bulkDownloads.limiter.respondOnLimit(w, r) {
		return
	}
	id, err := uuid.Parse(downloadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	}
	job, changed, err := s.database.PrepareDownload(r.Context(), db.DownloadPrepare{
		ID:           id,
		DownloadCaps: s.bulkDownloads.caps(),
		Now:          s.bulkDownloads.now(),
	})
	switch {
	case errors.Is(err, db.ErrDownloadNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	case errors.Is(err, db.ErrDownloadNotPreparable):
		writeError(w, http.StatusConflict, downloadNotPreparableCode,
			"this archive is blocked, failed or expired and cannot be built; ask for a new one with POST /v1/downloads")
		return
	case err != nil:
		s.writeDownloadBusyError(w, r, err, downloadPrepareAction)
		return
	}
	document, err := s.downloadDocument(*job)
	if err != nil {
		slog.ErrorContext(r.Context(), "build download document failed", "err", err, "download_id", job.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+downloadPrepareAction)
		return
	}
	if !changed {
		writeJSON(w, http.StatusOK, document)
		return
	}
	writeJSON(w, http.StatusAccepted, document)
}

// writeDownloadBusyError answers a create or prepare the caps refused, or that failed.
func (s *Server) writeDownloadBusyError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, db.ErrDownloadSelectionBusy):
		w.Header().Set("Retry-After", retryAfterSeconds(downloadsBusyRetryAfter))
		writeError(w, http.StatusTooManyRequests, selectionBusyCode, fmt.Sprintf(
			"this selection already has %d archives being built; wait for one to finish and try again "+
				"after Retry-After seconds", s.bulkDownloads.maxUnfinishedPerSelection))
	case errors.Is(err, db.ErrDownloadsBusy):
		slog.WarnContext(r.Context(), "archive requests reached downloads.max_unfinished", "err", err)
		w.Header().Set("Retry-After", retryAfterSeconds(downloadsBusyRetryAfter))
		writeError(w, http.StatusServiceUnavailable, downloadsBusyCode,
			"the server is building as many archives as it takes; try again after Retry-After seconds")
	default:
		slog.ErrorContext(r.Context(), action+" failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+action)
	}
}

// GetDownload returns where an archive request stands.
func (s *Server) GetDownload(w http.ResponseWriter, r *http.Request, downloadID string) {
	if !s.bulkDownloads.enabled {
		writeDownloadsOff(w)
		return
	}
	id, err := uuid.Parse(downloadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	}
	job, err := s.database.Downloads.Get(r.Context(), id)
	switch {
	case errors.Is(err, db.ErrDownloadNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "get download failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read the archive request")
		return
	}
	document, err := s.downloadDocument(*job)
	if err != nil {
		slog.ErrorContext(r.Context(), "build download document failed", "err", err, "download_id", job.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read the archive request")
		return
	}
	writeJSON(w, http.StatusOK, document)
}

// downloadDocument is a job as the API shows it at the server's now. An expired archive keeps
// its size and SHA-256 but no longer offers its URLs.
func (s *Server) downloadDocument(job domainmodels.DownloadJob) (DownloadDocument, error) {
	reported, err := job.ReportedStatus(s.bulkDownloads.now())
	if err != nil {
		return DownloadDocument{}, err
	}
	status, err := downloadStatusResponse(reported)
	if err != nil {
		return DownloadDocument{}, err
	}
	choice, err := downloadFileChoiceResponse(job.FileChoice)
	if err != nil {
		return DownloadDocument{}, err
	}
	preview, err := downloadPreviewResponse(job)
	if err != nil {
		return DownloadDocument{}, err
	}
	attributes := DownloadAttributes{
		SelectionId:      job.SelectionID,
		FileChoice:       choice,
		Status:           status,
		PrepareRequested: job.PrepareRequested,
		Preview:          preview,
		CreatedAt:        job.CreatedAt,
		FinishedAt:       job.FinishedAt,
		ExpiresAt:        job.ExpiresAt,
		MaxArchiveBytes:  job.MaxArchiveBytes,
		PlannedBytes:     job.PlannedBytes,
		ArchiveBytes:     job.ArchiveBytes,
		ArchiveSha256:    job.ArchiveSHA256,
	}
	attributes.Message, err = downloadMessage(job, reported)
	if err != nil {
		return DownloadDocument{}, err
	}
	if reported == domainmodels.DownloadStatusReady {
		attributes.ArchiveUrl = job.ArchiveURL
		attributes.ManifestUrl = job.ManifestURL
	}
	attributes.RetryableFiles = job.RetryableFiles
	attributes.RetryOf = job.RetryOf
	if job.Counts != nil {
		attributes.Counts = &DownloadCounts{
			Included:    job.Counts.Included,
			Linked:      job.Counts.Linked,
			Unavailable: job.Counts.Unavailable,
		}
	}
	self := s.publicBaseURL + "/v1/downloads/" + job.ID.String()
	return DownloadDocument{Data: DownloadData{
		Type:       jsonAPITypeDownloads,
		Id:         job.ID.String(),
		Attributes: attributes,
		Links: DownloadLinks{
			Self:      self,
			Selection: s.publicBaseURL + "/v1/selections/" + job.SelectionID.String(),
			Failures:  ptr(self + "/failures"),
		},
	}}, nil
}

// downloadMessage says why an archive is blocked, failed or expired, and what to do instead; other
// statuses have no message.
func downloadMessage(job domainmodels.DownloadJob, reported domainmodels.DownloadStatus) (*string, error) {
	var message string
	switch reported {
	case domainmodels.DownloadStatusBlocked:
		reason := "the files to include add up to more than one archive may hold"
		if job.BlockedReason != nil {
			reason = *job.BlockedReason
		}
		message = reason + ". " + downloadSmallerSelectionAdvice
	case domainmodels.DownloadStatusFailed:
		reason := "the archive could not be built"
		if job.Error != nil {
			reason = *job.Error
		}
		message = reason + ". Ask again with POST /v1/downloads; the selection is unchanged."
	case domainmodels.DownloadStatusExpired:
		message = "this archive is no longer offered. Ask again with POST /v1/downloads; the selection is unchanged."
	case domainmodels.DownloadStatusQueued, domainmodels.DownloadStatusPlanning, domainmodels.DownloadStatusPlanned,
		domainmodels.DownloadStatusPreparing, domainmodels.DownloadStatusReady:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown download status: %s", reported)
	}
	return &message, nil
}

func downloadStatusResponse(status domainmodels.DownloadStatus) (DownloadStatus, error) {
	switch status {
	case domainmodels.DownloadStatusQueued:
		return DownloadStatusQueued, nil
	case domainmodels.DownloadStatusPlanning:
		return DownloadStatusPlanning, nil
	case domainmodels.DownloadStatusPlanned:
		return DownloadStatusPlanned, nil
	case domainmodels.DownloadStatusPreparing:
		return DownloadStatusPreparing, nil
	case domainmodels.DownloadStatusReady:
		return DownloadStatusReady, nil
	case domainmodels.DownloadStatusBlocked:
		return DownloadStatusBlocked, nil
	case domainmodels.DownloadStatusFailed:
		return DownloadStatusFailed, nil
	case domainmodels.DownloadStatusExpired:
		return DownloadStatusExpired, nil
	default:
		return "", fmt.Errorf("unknown download status: %s", status)
	}
}

func downloadFileChoiceResponse(choice domainmodels.DownloadFileChoice) (DownloadFileChoice, error) {
	switch choice {
	case domainmodels.DownloadFileChoiceOriginalMMCIF:
		return DownloadFileChoiceOriginalMmcif, nil
	case domainmodels.DownloadFileChoiceModifiedCoordinates:
		return DownloadFileChoiceModifiedCoordinates, nil
	case domainmodels.DownloadFileChoiceAllFiles:
		return DownloadFileChoiceAllFiles, nil
	default:
		return "", fmt.Errorf("unknown download file choice: %s", choice)
	}
}
