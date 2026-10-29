package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// chemComp is the part of one chemical component dictionary (CCD) record the
// refresh checks.
type chemComp struct {
	ID            string
	Type          string
	Formula       string
	ReleaseStatus string
	ReplacedBy    string
	AtomCount     int
}

// dictionary is what the refresh reads from the CCD. The RCSB implementation
// needs the network; tests use a fake.
type dictionary interface {
	// countWithType returns how many CCD IDs have a chem_comp.type at all.
	countWithType(ctx context.Context) (int, error)
	// countOfType returns how many CCD IDs have exactly this chem_comp.type.
	countOfType(ctx context.Context, ccdType string) (int, error)
	// idsOfType returns every CCD ID whose chem_comp.type is exactly ccdType.
	idsOfType(ctx context.Context, ccdType string) ([]string, error)
	// singleHeavyAtomNonPolymers returns the non-polymer IDs with one heavy atom.
	singleHeavyAtomNonPolymers(ctx context.Context) ([]string, error)
	// chemComps returns the records of the IDs the CCD has; an unknown ID is
	// absent from the map.
	chemComps(ctx context.Context, ids []string) (map[string]chemComp, error)
}

const (
	defaultSearchURL  = "https://search.rcsb.org/rcsbsearch/v2/query"
	defaultGraphQLURL = "https://data.rcsb.org/graphql"
	// chemCompBatchSize keeps one GraphQL request well under the API's limits.
	chemCompBatchSize = 300
	requestAttempts   = 4
	defaultRetryDelay = 2 * time.Second
)

// rcsbDictionary reads the CCD through the RCSB search and data APIs, the
// provider the archive sync already uses.
type rcsbDictionary struct {
	httpClient *http.Client
	searchURL  string
	graphqlURL string
	// retryDelay is the wait before the first retry; it grows with each attempt.
	retryDelay time.Duration
}

func newRCSBDictionary() *rcsbDictionary {
	return &rcsbDictionary{
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		searchURL:  defaultSearchURL,
		graphqlURL: defaultGraphQLURL,
		retryDelay: defaultRetryDelay,
	}
}

type searchResponse struct {
	TotalCount int `json:"total_count"`
	ResultSet  []struct {
		Identifier string `json:"identifier"`
	} `json:"result_set"`
}

func terminal(attribute string, operator string, value any) map[string]any {
	parameters := map[string]any{"attribute": attribute, "operator": operator}
	if value != nil {
		parameters["value"] = value
	}
	return map[string]any{"type": "terminal", "service": "text_chem", "parameters": parameters}
}

func (d *rcsbDictionary) countWithType(ctx context.Context) (int, error) {
	response, err := d.search(ctx, terminal("chem_comp.type", "exists", nil), map[string]any{"return_counts": true})
	if err != nil {
		return 0, fmt.Errorf("count CCD IDs with a type: %w", err)
	}
	return response.TotalCount, nil
}

func (d *rcsbDictionary) countOfType(ctx context.Context, ccdType string) (int, error) {
	response, err := d.search(ctx, terminal("chem_comp.type", "exact_match", ccdType), map[string]any{"return_counts": true})
	if err != nil {
		return 0, fmt.Errorf("count CCD IDs of type %q: %w", ccdType, err)
	}
	return response.TotalCount, nil
}

func (d *rcsbDictionary) idsOfType(ctx context.Context, ccdType string) ([]string, error) {
	ids, err := d.searchAll(ctx, terminal("chem_comp.type", "exact_match", ccdType))
	if err != nil {
		return nil, fmt.Errorf("list CCD IDs of type %q: %w", ccdType, err)
	}
	return ids, nil
}

func (d *rcsbDictionary) singleHeavyAtomNonPolymers(ctx context.Context) ([]string, error) {
	query := map[string]any{
		"type":             "group",
		"logical_operator": "and",
		"nodes": []any{
			terminal("rcsb_chem_comp_info.atom_count_heavy", "equals", 1),
			terminal("chem_comp.type", "exact_match", "non-polymer"),
		},
	}
	ids, err := d.searchAll(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list single-heavy-atom non-polymers: %w", err)
	}
	return ids, nil
}

// searchAll returns every identifier a query matches. The search API answers
// 204 No Content when nothing matches.
func (d *rcsbDictionary) searchAll(ctx context.Context, query any) ([]string, error) {
	response, err := d.search(ctx, query, map[string]any{"return_all_hits": true})
	if err != nil {
		return nil, err
	}
	if len(response.ResultSet) != response.TotalCount {
		return nil, fmt.Errorf("search returned %d of %d hits", len(response.ResultSet), response.TotalCount)
	}
	ids := make([]string, 0, len(response.ResultSet))
	for _, result := range response.ResultSet {
		ids = append(ids, result.Identifier)
	}
	return ids, nil
}

func (d *rcsbDictionary) search(ctx context.Context, query any, options map[string]any) (searchResponse, error) {
	body, err := json.Marshal(map[string]any{
		"query":           query,
		"return_type":     "mol_definition",
		"request_options": options,
	})
	if err != nil {
		return searchResponse{}, fmt.Errorf("encode search query: %w", err)
	}
	contents, status, err := d.post(ctx, d.searchURL, body)
	if err != nil {
		return searchResponse{}, err
	}
	if status == http.StatusNoContent {
		return searchResponse{}, nil
	}
	var response searchResponse
	if err := json.Unmarshal(contents, &response); err != nil {
		return searchResponse{}, fmt.Errorf("decode search response: %w", err)
	}
	return response, nil
}

const chemCompQuery = `query($ids: [String!]!) {
  chem_comps(comp_ids: $ids) {
    chem_comp { id type formula pdbx_release_status pdbx_replaced_by }
    rcsb_chem_comp_info { atom_count }
  }
}`

type chemCompResponse struct {
	Data struct {
		ChemComps []*struct {
			ChemComp struct {
				ID            string  `json:"id"`
				Type          string  `json:"type"`
				Formula       *string `json:"formula"`
				ReleaseStatus string  `json:"pdbx_release_status"`
				ReplacedBy    *string `json:"pdbx_replaced_by"`
			} `json:"chem_comp"`
			Info *struct {
				AtomCount int `json:"atom_count"`
			} `json:"rcsb_chem_comp_info"`
		} `json:"chem_comps"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (d *rcsbDictionary) chemComps(ctx context.Context, ids []string) (map[string]chemComp, error) {
	records := map[string]chemComp{}
	for start := 0; start < len(ids); start += chemCompBatchSize {
		batch := ids[start:min(start+chemCompBatchSize, len(ids))]
		body, err := json.Marshal(map[string]any{"query": chemCompQuery, "variables": map[string]any{"ids": batch}})
		if err != nil {
			return nil, fmt.Errorf("encode chem_comps query: %w", err)
		}
		contents, _, err := d.post(ctx, d.graphqlURL, body)
		if err != nil {
			return nil, fmt.Errorf("read chem_comps: %w", err)
		}
		var response chemCompResponse
		if err := json.Unmarshal(contents, &response); err != nil {
			return nil, fmt.Errorf("decode chem_comps response: %w", err)
		}
		if len(response.Errors) > 0 {
			return nil, fmt.Errorf("chem_comps query failed: %s", response.Errors[0].Message)
		}
		for _, entry := range response.Data.ChemComps {
			if entry == nil {
				continue
			}
			record := chemComp{
				ID:            entry.ChemComp.ID,
				Type:          entry.ChemComp.Type,
				ReleaseStatus: entry.ChemComp.ReleaseStatus,
			}
			if entry.ChemComp.Formula != nil {
				record.Formula = *entry.ChemComp.Formula
			}
			if entry.ChemComp.ReplacedBy != nil {
				record.ReplacedBy = *entry.ChemComp.ReplacedBy
			}
			if entry.Info != nil {
				record.AtomCount = entry.Info.AtomCount
			}
			records[record.ID] = record
		}
	}
	return records, nil
}

var errRetryable = errors.New("retryable response")

// post sends a JSON body and returns the response body and status. Server
// errors and rate limits are retried a few times.
func (d *rcsbDictionary) post(ctx context.Context, url string, body []byte) ([]byte, int, error) {
	var lastErr error
	for attempt := 1; attempt <= requestAttempts; attempt++ {
		contents, status, err := d.postOnce(ctx, url, body)
		if err == nil {
			return contents, status, nil
		}
		lastErr = err
		if !errors.Is(err, errRetryable) || attempt == requestAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-time.After(d.retryDelay * time.Duration(attempt)):
		}
	}
	return nil, 0, lastErr
}

func (d *rcsbDictionary) postOnce(ctx context.Context, url string, body []byte) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("POST %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s response: %w", url, err)
	}
	switch {
	case response.StatusCode == http.StatusOK, response.StatusCode == http.StatusNoContent:
		return contents, response.StatusCode, nil
	case response.StatusCode == http.StatusTooManyRequests, response.StatusCode >= http.StatusInternalServerError:
		return nil, 0, fmt.Errorf("POST %s: HTTP %d: %w", url, response.StatusCode, errRetryable)
	default:
		return nil, 0, fmt.Errorf("POST %s: HTTP %d: %s", url, response.StatusCode, bytes.TrimSpace(contents))
	}
}
