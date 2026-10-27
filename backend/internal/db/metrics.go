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
	query := `insert into metrics(id, key, value, software_name, software_version, run_id, created_at)
			  values (:id, :key, :value, :software_name, :software_version, :run_id, :created_at)
			  returning ` + metricColumns

	var row metricRow
	args := map[string]any{
		"id":               metric.ID,
		"key":              string(metric.Key),
		"value":            metric.Value,
		"software_name":    metric.SoftwareName,
		"software_version": metric.SoftwareVersion,
		"run_id":           metric.RunID,
		"created_at":       metric.CreatedAt,
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

// ListByModelRevisionIDs returns the metrics of each given model revision in one query.
// Revisions without metrics are absent from the map; each slice is ordered like List.
func (r *MetricsRepository) ListByModelRevisionIDs(
	ctx context.Context,
	modelRevisionIDs []uuid.UUID,
) (map[uuid.UUID][]models.Metric, error) {
	metricsByRevision := make(map[uuid.UUID][]models.Metric, len(modelRevisionIDs))
	if len(modelRevisionIDs) == 0 {
		return metricsByRevision, nil
	}

	query := `select model_revision_metrics.model_revision_id, ` + qualifiedMetricColumns + `
			  from metrics
			  join model_revision_metrics on model_revision_metrics.metric_id = metrics.id
			  where model_revision_metrics.model_revision_id = any($1)
			  order by model_revision_metrics.model_revision_id, metrics.created_at asc, metrics.id asc`

	rows := make([]modelRevisionMetricRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, pq.Array(modelRevisionIDs)); err != nil {
		return nil, fmt.Errorf("list metrics by model revision ids: %w", err)
	}

	for _, row := range rows {
		metricsByRevision[row.ModelRevisionID] = append(metricsByRevision[row.ModelRevisionID], *metricFromRow(&row.metricRow))
	}
	return metricsByRevision, nil
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

// CopyModelRevisionLinks links every metric of one model revision to another. The stored
// metric_key travels with the link, so metrics quarantined as duplicates stay quarantined
// instead of colliding on the unique (revision, metric key) index.
func (r *MetricsRepository) CopyModelRevisionLinks(
	ctx context.Context,
	fromRevisionID, toRevisionID uuid.UUID,
) error {
	return r.CopyModelRevisionLinksExcludingKeys(ctx, fromRevisionID, toRevisionID, nil)
}

// CopyModelRevisionLinksExcludingKeys is CopyModelRevisionLinks without the metrics whose key is listed.
func (r *MetricsRepository) CopyModelRevisionLinksExcludingKeys(
	ctx context.Context,
	fromRevisionID, toRevisionID uuid.UUID,
	excludedKeys []models.MetricKey,
) error {
	query := `insert into model_revision_metrics(model_revision_id, metric_id, metric_key)
			  select $2, link.metric_id, link.metric_key
			  from model_revision_metrics link
			  join metrics on metrics.id = link.metric_id
			  where link.model_revision_id = $1
			    and metrics.key <> all($3::text[])
			  on conflict (model_revision_id, metric_id) do nothing`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx, query, fromRevisionID, toRevisionID, pq.Array(metricKeyStrings(excludedKeys)),
	); err != nil {
		return fmt.Errorf("copy model revision metric links: %w", err)
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
	query := `select ` + qualifiedMetricColumns + `
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

// FillMissingSoftware records the software of a metric that names none, and reports
// whether it did. A metric that already names its software keeps it.
func (r *MetricsRepository) FillMissingSoftware(
	ctx context.Context,
	metricID uuid.UUID,
	softwareName string,
	softwareVersion *string,
) (bool, error) {
	query := `update metrics
			  set software_name = $2, software_version = $3
			  where id = $1 and software_name is null`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, metricID, softwareName, softwareVersion)
	if err != nil {
		return false, fmt.Errorf("fill missing metric software: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count metrics with filled software: %w", err)
	}
	return updated == 1, nil
}

const metricColumns = `id, key, value, software_name, software_version, run_id, created_at`

const qualifiedMetricColumns = `metrics.id, metrics.key, metrics.value, metrics.software_name,
			         metrics.software_version, metrics.run_id, metrics.created_at`

func metricKeyStrings(values []models.MetricKey) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func metricFromRow(row *metricRow) *models.Metric {
	return &models.Metric{
		ID:              row.ID,
		Key:             models.MetricKey(row.Key),
		Value:           row.Value,
		SoftwareName:    row.SoftwareName,
		SoftwareVersion: row.SoftwareVersion,
		RunID:           row.RunID,
		CreatedAt:       row.CreatedAt,
	}
}

type metricRow struct {
	ID              uuid.UUID  `db:"id"`
	Key             string     `db:"key"`
	Value           float64    `db:"value"`
	SoftwareName    *string    `db:"software_name"`
	SoftwareVersion *string    `db:"software_version"`
	RunID           *uuid.UUID `db:"run_id"`
	CreatedAt       time.Time  `db:"created_at"`
}

type modelRevisionMetricRow struct {
	ModelRevisionID uuid.UUID `db:"model_revision_id"`
	metricRow
}
