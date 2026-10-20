package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"dynamic-pdb/backend/internal/db"
)

// listPage is the window one list request asks for: Limit rows, starting either at
// Offset (deprecated) or right after Cursor.
type listPage struct {
	Limit  int
	Offset *int
	Cursor *listCursor
}

func listPageFromParams(limit, offset *int, cursor *string, kind listCursorKind) (listPage, error) {
	if limit != nil && *limit < 0 {
		return listPage{}, errors.New("limit must be non-negative")
	}
	if offset != nil && *offset < 0 {
		return listPage{}, errors.New("offset must be non-negative")
	}
	page := listPage{Limit: *limitOrDefault(limit), Offset: offset}
	// An empty cursor means the first page, so clients may send `cursor=` unconditionally.
	if cursor == nil || *cursor == "" {
		return page, nil
	}
	if offset != nil {
		return listPage{}, fmt.Errorf("%w: cannot be combined with offset", errInvalidListCursor)
	}
	decoded, err := decodeListCursor(*cursor, kind)
	if err != nil {
		return listPage{}, fmt.Errorf("decode cursor: %w", err)
	}
	page.Cursor = &decoded
	return page, nil
}

// fetchLimit asks the database for one row past the page, so the handler learns
// whether a next page exists without counting rows.
func (p listPage) fetchLimit() *int {
	return ptr(p.Limit + 1)
}

func (p listPage) after() *db.ListPosition {
	if p.Cursor == nil {
		return nil
	}
	return p.Cursor.position()
}

// trimToPage drops the extra row requested by fetchLimit and reports whether it was there.
func trimToPage[T any](rows []T, page listPage) ([]T, bool) {
	if len(rows) > page.Limit {
		return rows[:page.Limit], true
	}
	return rows, false
}

// nextPageLinks builds links.next and meta.next_cursor from the next page's cursor. The
// next URL keeps the request's path and filters, swaps offset for the cursor, and
// starts from the public base URL because the ingress strips its /api prefix before
// the request reaches this server.
func (s *Server) nextPageLinks(r *http.Request, next listCursor) (*CollectionLinks, *CursorPageMeta, error) {
	encoded, err := encodeListCursor(next)
	if err != nil {
		return nil, nil, fmt.Errorf("encode next cursor: %w", err)
	}
	query := r.URL.Query()
	query.Del("offset")
	query.Set("cursor", encoded)
	nextURL := s.publicBaseURL + r.URL.EscapedPath() + "?" + query.Encode()
	return &CollectionLinks{Next: nextURL}, &CursorPageMeta{NextCursor: encoded}, nil
}

// writeListParamsError rejects a list request. Cursor problems get their own message
// because the fix is in how the client pages, not in its filters.
func writeListParamsError(w http.ResponseWriter, err error, filtersMessage string) {
	if errors.Is(err, errInvalidListCursor) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"invalid cursor: pass meta.next_cursor from the previous page, without offset")
		return
	}
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", filtersMessage)
}
