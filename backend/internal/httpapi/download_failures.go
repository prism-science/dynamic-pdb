package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// ListDownloadFailures lists the files a ready archive could not vouch for, in selection order.
func (s *Server) ListDownloadFailures(
	w http.ResponseWriter,
	r *http.Request,
	downloadID string,
	params ListDownloadFailuresParams,
) {
	if !s.bulkDownloads.enabled {
		writeDownloadsOff(w)
		return
	}
	page, err := listPageFromParams(params.Limit, nil, params.Cursor, listCursorKindDownloadFailures)
	if err != nil {
		writeListParamsError(w, err, "invalid limit")
		return
	}
	id, err := uuid.Parse(downloadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	}
	if _, err := s.database.Downloads.Get(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrDownloadNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
			return
		}
		slog.ErrorContext(r.Context(), "get download failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the archive's failures")
		return
	}
	var after *db.DownloadFailurePosition
	if page.Cursor != nil {
		after = &db.DownloadFailurePosition{Ordinal: page.Cursor.Ordinal, FileID: page.Cursor.ID}
	}
	failures, err := s.database.Downloads.Failures(r.Context(), id, after, *page.fetchLimit())
	if err != nil {
		slog.ErrorContext(r.Context(), "list download failures failed", "err", err, "download_id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the archive's failures")
		return
	}
	failures, hasNextPage := trimToPage(failures, page)
	links, meta, err := nextPageLinksAfter(s, r, failures, hasNextPage, func(last domainmodels.DownloadFailure) listCursor {
		return listCursor{Kind: listCursorKindDownloadFailures, Ordinal: last.Ordinal, ID: last.FileID}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build download failure links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the archive's failures")
		return
	}
	data := make([]DownloadFailureData, 0, len(failures))
	for _, failure := range failures {
		item, err := downloadFailureData(failure)
		if err != nil {
			slog.ErrorContext(r.Context(), "build download failure failed", "err", err, "download_id", id)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the archive's failures")
			return
		}
		data = append(data, item)
	}
	writeJSON(w, http.StatusOK, DownloadFailureCollectionDocument{Data: data, Links: links, Meta: meta})
}

// downloadFailureData is one failure as the API shows it.
func downloadFailureData(failure domainmodels.DownloadFailure) (DownloadFailureData, error) {
	code, err := downloadReasonCodeResponse(failure.ReasonCode)
	if err != nil {
		return DownloadFailureData{}, err
	}
	availability, err := failure.ReasonCode.Availability()
	if err != nil {
		return DownloadFailureData{}, err
	}
	status, err := downloadAvailabilityResponse(availability)
	if err != nil {
		return DownloadFailureData{}, err
	}
	reason, err := failure.ReasonCode.Text()
	if err != nil {
		return DownloadFailureData{}, err
	}
	return DownloadFailureData{
		Type: jsonAPITypeDownloadFailures,
		Id:   failure.FileID.String(),
		Attributes: DownloadFailureAttributes{
			FileId: failure.FileID, FileName: failure.FileName, EntryId: failure.EntryID, ModelId: failure.ModelID,
			Status: status, ReasonCode: code, Reason: reason, Retryable: failure.Retryable,
		},
	}, nil
}

// RetryDownload asks again for the files a ready archive could not read.
func (s *Server) RetryDownload(w http.ResponseWriter, r *http.Request, downloadID string) {
	if !s.bulkDownloads.enabled {
		writeDownloadsOff(w)
		return
	}
	if s.bulkDownloads.limiter.respondOnLimit(w, r) {
		return
	}
	parentID, err := uuid.Parse(downloadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
		return
	}
	job, created, err := s.database.CreateDownloadRetry(r.Context(), db.DownloadRetry{
		ID:           uuid.New(),
		ParentID:     parentID,
		DownloadCaps: s.bulkDownloads.caps(),
		Now:          s.bulkDownloads.now(),
	})
	if err != nil {
		s.writeDownloadRetryError(w, r, err)
		return
	}
	document, err := s.downloadDocument(*job)
	if err != nil {
		slog.ErrorContext(r.Context(), "build download document failed", "err", err, "download_id", job.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+downloadRetryAction)
		return
	}
	if !created {
		writeJSON(w, http.StatusOK, document)
		return
	}
	w.Header().Set("Location", document.Data.Links.Self)
	writeJSON(w, http.StatusAccepted, document)
}

// writeDownloadRetryError answers a retry the database refused or could not save.
func (s *Server) writeDownloadRetryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, db.ErrDownloadNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", downloadNotFoundDetail)
	case errors.Is(err, db.ErrDownloadNotRetryable):
		writeError(w, http.StatusConflict, downloadNotRetryableCode,
			"only a ready archive's failed files can be asked for again; this one is not ready or has expired")
	case errors.Is(err, db.ErrDownloadNothingToRetry):
		writeError(w, http.StatusConflict, nothingToRetryCode, fmt.Sprintf(
			"this archive has no failed file a retry can fetch: only files whose copy could not be read (%s) "+
				"are asked for again; links.failures lists every file it could not vouch for",
			domainmodels.DownloadReasonReadFailed))
	default:
		s.writeDownloadBusyError(w, r, err, downloadRetryAction)
	}
}
