package rcsb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Holdings statuses and the status codes of removed entries.
const (
	HoldingsCurrent    = "CURRENT"
	HoldingsRemoved    = "REMOVED"
	HoldingsUnreleased = "UNRELEASED"

	StatusCodeObsolete  = "OBS"
	StatusCodeWithdrawn = "WDRN"
)

// HoldingsStatus is where an ID stands in the PDB archive. Status is CURRENT, REMOVED or
// UNRELEASED; StatusCode is the finer wwPDB code, for example REL, OBS or WDRN.
// ReplacedByLatest is the newest entry that replaced a removed one, if any.
type HoldingsStatus struct {
	Status           string `json:"status"`
	StatusCode       string `json:"status_code"`
	ReplacedByLatest string `json:"id_code_replaced_by_latest"`
}

// RemovedEntry describes an entry the archive removed: the entries that replaced it, the
// RFC 3339 date it was removed, and the title it had.
type RemovedEntry struct {
	ReplacedBy []string `json:"id_codes_replaced_by"`
	RemoveDate string   `json:"remove_date"`
	Title      string   `json:"title"`
}

// GetHoldingsStatus returns where pdbID stands in the archive. It returns ErrNotFound for an ID
// the archive does not know.
func (c *RemoteClient) GetHoldingsStatus(ctx context.Context, pdbID string) (HoldingsStatus, error) {
	var document struct {
		Status HoldingsStatus `json:"rcsb_repository_holdings_combined"`
	}
	if err := c.getHoldings(ctx, "status", pdbID, &document); err != nil {
		return HoldingsStatus{}, err
	}
	return document.Status, nil
}

// GetRemovedEntry returns how the archive removed pdbID. It returns ErrNotFound when the
// archive has not removed it.
func (c *RemoteClient) GetRemovedEntry(ctx context.Context, pdbID string) (RemovedEntry, error) {
	var document struct {
		Removed RemovedEntry `json:"rcsb_repository_holdings_removed"`
	}
	if err := c.getHoldings(ctx, "removed", pdbID, &document); err != nil {
		return RemovedEntry{}, err
	}
	return document.Removed, nil
}

func (c *RemoteClient) getHoldings(ctx context.Context, kind string, pdbID string, document any) error {
	url := c.dataBaseURL + "/rest/v1/holdings/" + kind + "/" + strings.ToUpper(strings.TrimSpace(pdbID))
	contents, err := c.get(ctx, url)
	if err != nil {
		return fmt.Errorf("get RCSB holdings %s of %s: %w", kind, pdbID, err)
	}
	if err := json.Unmarshal(contents, document); err != nil {
		return fmt.Errorf("decode RCSB holdings %s of %s: %w", kind, pdbID, err)
	}
	return nil
}
