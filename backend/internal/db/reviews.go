package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

var ErrReviewNotFound = errors.New("db: review not found")

// ReviewsRepository projects the review queue over entry_revisions and
// model_revisions. There is no reviews table: a submission is an entry whose
// own revision is in_review, or that has at least one model revision in_review.
// Either way the queue is keyed on the entry, never on a model.
type ReviewsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewReviewsRepository(database *sqlx.DB, queriers *QuerierProvider) *ReviewsRepository {
	return &ReviewsRepository{db: database, queriers: queriers}
}

// QueueItem is one row of the queue.
type QueueItem struct {
	EntryID     uuid.UUID
	Name        string
	SubmittedAt time.Time
	SubmittedBy []uuid.UUID
}

type QueueFilters struct {
	SubmittedBy *uuid.UUID
	Limit       *int
	Offset      *int
}

type queueRow struct {
	EntryID     uuid.UUID      `db:"entry_id"`
	Name        string         `db:"name"`
	SubmittedAt time.Time      `db:"submitted_at"`
	SubmittedBy pq.StringArray `db:"submitted_by"`
}

// ListQueue returns one row per entry with something waiting, oldest submission
// first. SubmittedBy is filtered inside the union so a caller who is not the
// reviewer sees only entries they contributed to.
func (r *ReviewsRepository) ListQueue(ctx context.Context, filters QueueFilters) ([]QueueItem, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return nil, errors.New("offset must be non-negative")
	}

	query := `select grouped.entry_id as entry_id,
			         coalesce(
			             (select in_review_revision.name
			                from entry_revisions in_review_revision
			               where in_review_revision.entry_id = grouped.entry_id
			                 and in_review_revision.state = 'in_review'
			               order by in_review_revision.created_at desc
			               limit 1),
			             (select active_revision.name
			                from entry_revisions active_revision
			               where active_revision.entry_id = grouped.entry_id
			                 and active_revision.state = 'active'
			               limit 1),
			             ''
			         ) as name,
			         grouped.submitted_at as submitted_at,
			         grouped.submitted_by as submitted_by
			  from (
			      select submission.entry_id as entry_id,
			             min(submission.submitted_at) as submitted_at,
			             array_agg(distinct cast(submission.submitted_by as text)) as submitted_by
			      from (
			          select entry_revision.entry_id as entry_id,
			                 entry_revision.updated_at as submitted_at,
			                 entry_revision.created_by as submitted_by
			          from entry_revisions entry_revision
			          where entry_revision.state = 'in_review'
			            and (cast(:submitted_by as uuid) is null
			                 or entry_revision.created_by = cast(:submitted_by as uuid))
			          union all
			          select model.entry_id as entry_id,
			                 model_revision.updated_at as submitted_at,
			                 model_revision.created_by as submitted_by
			          from model_revisions model_revision
			          join models model on model.id = model_revision.model_id
			          where model_revision.state = 'in_review'
			            and (cast(:submitted_by as uuid) is null
			                 or model_revision.created_by = cast(:submitted_by as uuid))
			      ) as submission
			      group by submission.entry_id
			  ) as grouped
			  order by grouped.submitted_at asc, grouped.entry_id asc`

	args := map[string]any{"submitted_by": filters.SubmittedBy}
	if filters.Limit != nil {
		query += "\nlimit :limit"
		args["limit"] = *filters.Limit
	}
	if filters.Offset != nil {
		query += "\noffset :offset"
		args["offset"] = *filters.Offset
	}

	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind review queue query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	rows := make([]queueRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list review queue: %w", err)
	}

	items := make([]QueueItem, 0, len(rows))
	for _, row := range rows {
		submitters := make([]uuid.UUID, 0, len(row.SubmittedBy))
		for _, raw := range row.SubmittedBy {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("parse review submitter %q: %w", raw, err)
			}
			submitters = append(submitters, parsed)
		}
		items = append(items, QueueItem{
			EntryID:     row.EntryID,
			Name:        row.Name,
			SubmittedAt: row.SubmittedAt,
			SubmittedBy: submitters,
		})
	}
	return items, nil
}
