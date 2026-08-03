package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

type MetricsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type MetricFilters struct {
	ID              *uuid.UUID
	ModelRevisionID *uuid.UUID
	Keys            []models.MetricKey
	Limit           *int
	Offset          *int
}

func NewMetricsRepository(database *sqlx.DB, queriers *QuerierProvider) *MetricsRepository {
	return &MetricsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *MetricsRepository) Create(ctx context.Context, metric models.Metric) (*models.Metric, error) {
	query := `insert into metrics(id, key, value, created_at)
			  values (:id, :key, :value, :created_at)
			  returning id, key, value, created_at`

	var row metricRow
	args := map[string]any{
		"id":         metric.ID,
		"key":        string(metric.Key),
		"value":      metric.Value,
		"created_at": metric.CreatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind metric insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert metric: %w", err)
	}

	return metricFromRow(&row), nil
}

func (r *MetricsRepository) List(ctx context.Context, filters MetricFilters) ([]models.Metric, error) {
	query, args, err := metricListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build metric list query: %w", err)
	}

	rows := make([]metricRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind metric list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}

	metrics := make([]models.Metric, 0, len(rows))
	for _, row := range rows {
		metrics = append(metrics, *metricFromRow(&row))
	}
	return metrics, nil
}

func (r *MetricsRepository) AttachToModelRevision(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	metricID uuid.UUID,
) error {
	query := `insert into model_revision_metrics(model_revision_id, metric_id)
			  values ($1, $2)
			  on conflict (model_revision_id, metric_id) do nothing`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, modelRevisionID, metricID); err != nil {
		return fmt.Errorf("attach metric to model revision: %w", err)
	}
	return nil
}

func metricListQuery(filters MetricFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select metrics.id, metrics.key, metrics.value, metrics.created_at
			  from metrics`

	if filters.ModelRevisionID != nil {
		query += "\njoin model_revision_metrics on model_revision_metrics.metric_id = metrics.id"
		conditions = append(conditions, "model_revision_metrics.model_revision_id = :model_revision_id")
		args["model_revision_id"] = *filters.ModelRevisionID
	}
	if filters.ID != nil {
		conditions = append(conditions, "metrics.id = :id")
		args["id"] = *filters.ID
	}
	if len(filters.Keys) > 0 {
		conditions = append(conditions, "metrics.key = any(cast(:keys as text[]))")
		args["keys"] = pq.Array(metricKeyStrings(filters.Keys))
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by metrics.created_at asc, metrics.id asc"

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

func metricKeyStrings(values []models.MetricKey) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func metricFromRow(row *metricRow) *models.Metric {
	return &models.Metric{
		ID:        row.ID,
		Key:       models.MetricKey(row.Key),
		Value:     row.Value,
		CreatedAt: row.CreatedAt,
	}
}

type metricRow struct {
	ID        uuid.UUID `db:"id"`
	Key       string    `db:"key"`
	Value     float64   `db:"value"`
	CreatedAt time.Time `db:"created_at"`
}
