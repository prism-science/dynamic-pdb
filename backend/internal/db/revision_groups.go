package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

// RevisionGroupFilters selects the entries that have an entry revision or a model
// revision in State, optionally only revisions created by CreatedBy.
type RevisionGroupFilters struct {
	State     models.RevisionState
	CreatedBy *uuid.UUID
	Limit     *int
	Offset    *int
	// After continues the list (ordered by latest_created_at desc, entry_id asc) past
	// this position. It cannot be combined with Offset.
	After *RevisionGroupPosition
}

// RevisionGroup is one entry in the revision group list. LatestCreatedAt is the
// creation time of its newest matching entry or model revision.
type RevisionGroup struct {
	EntryID         string    `db:"entry_id"`
	LatestCreatedAt time.Time `db:"latest_created_at"`
}

// ListRevisionGroups lists one page of entries with matching revisions, newest first.
// The grouping, ordering and paging run in SQL, so a page costs the same at any depth
// and only the page's entries are loaded afterwards.
func (r *EntriesRepository) ListRevisionGroups(
	ctx context.Context,
	filters RevisionGroupFilters,
) ([]RevisionGroup, error) {
	query, args, err := revisionGroupListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build revision group list query: %w", err)
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind revision group list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	groups := make([]RevisionGroup, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &groups, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list revision groups: %w", err)
	}
	return groups, nil
}

func revisionGroupListQuery(filters RevisionGroupFilters) (string, map[string]any, error) {
	if filters.State == "" {
		return "", nil, errors.New("state is required")
	}
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}
	if filters.After != nil && filters.Offset != nil {
		return "", nil, errors.New("after and offset cannot be combined")
	}

	args := map[string]any{"state": string(filters.State)}
	entryRevisionConditions := []string{"entry_revisions.state = :state"}
	modelRevisionConditions := []string{"model_revisions.state = :state"}
	if filters.CreatedBy != nil {
		entryRevisionConditions = append(entryRevisionConditions, "entry_revisions.created_by = :created_by")
		modelRevisionConditions = append(modelRevisionConditions, "model_revisions.created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}

	query := `with matching_revisions as (
			    select entry_revisions.entry_id, entry_revisions.created_at
			    from entry_revisions
			    where ` + strings.Join(entryRevisionConditions, " and ") + `
			    union all
			    select models.entry_id, model_revisions.created_at
			    from model_revisions
			    join models on models.id = model_revisions.model_id
			    where ` + strings.Join(modelRevisionConditions, " and ") + `
			  ),
			  revision_groups as (
			    select entry_id, max(created_at) as latest_created_at
			    from matching_revisions
			    group by entry_id
			  )
			  select entry_id, latest_created_at
			  from revision_groups`
	if filters.After != nil {
		query += `
			  where latest_created_at < :after_created_at
			     or (latest_created_at = :after_created_at and entry_id > :after_entry_id)`
		args["after_created_at"] = filters.After.LatestCreatedAt
		args["after_entry_id"] = filters.After.EntryID
	}
	query += "\norder by latest_created_at desc, entry_id asc"

	if filters.Limit != nil {
		query += "\nlimit :limit"
		args["limit"] = *filters.Limit
	}
	if filters.Offset != nil {
		query += "\noffset :offset"
		args["offset"] = *filters.Offset
	}
	return query, args, nil
}
