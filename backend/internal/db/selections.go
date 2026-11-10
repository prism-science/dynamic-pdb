package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

var (
	// ErrSelectionNotFound means no selection has the ID.
	ErrSelectionNotFound = errors.New("db: selection not found")
	// ErrSelectionEmpty means the request matched no model, so nothing was saved.
	ErrSelectionEmpty = errors.New("db: selection matches no model")
	// ErrSelectionTooLarge means the request matched more models than one selection may hold.
	ErrSelectionTooLarge = errors.New("db: selection matches more models than one selection may hold")
	// ErrSelectionsFull means saving the selection would take the rows of all stored selections
	// past their budget.
	ErrSelectionsFull = errors.New("db: stored selections reached their budget")
	// ErrSelectionFreezeTimeout means the freeze ran past its statement timeout.
	ErrSelectionFreezeTimeout = errors.New("db: selection freeze ran past its statement timeout")
)

// UnknownSelectionModelsError names requested models that have no active revision in an active
// entry, in the order they were requested. Nothing was saved.
type UnknownSelectionModelsError struct {
	ModelIDs []string
}

func (e *UnknownSelectionModelsError) Error() string {
	return "db: no active model with ID " + strings.Join(e.ModelIDs, ", ")
}

// SelectionFreeze is one save of a selection.
type SelectionFreeze struct {
	ID uuid.UUID
	// ModelIDs, Searches and ExcludeModelIDs say which models to freeze: each model named, and
	// every model matching one of the searches, minus the excluded models. A search without
	// filters matches every model. A model named in ModelIDs must exist; an excluded one need not.
	ModelIDs        []string
	Searches        []ModelSearch
	ExcludeModelIDs []string
	// Request is saved with the selection as what the client asked for.
	Request models.SelectionRequest
	// MaxModels is the most models the selection may hold.
	MaxModels int
	// MaxStoredModels caps the rows all stored selections are charged together, this one
	// included: each selection's frozen rows plus its stored request (selectionChargedRows).
	MaxStoredModels int
	// StatementTimeout replaces the connection's statement timeout for the freeze.
	StatementTimeout time.Duration
}

type SelectionsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewSelectionsRepository(database *sqlx.DB, queriers *QuerierProvider) *SelectionsRepository {
	return &SelectionsRepository{db: database, queriers: queriers}
}

// selectionFreezeDeadlineMargin is how much longer than its statement timeout a whole freeze may
// take: waiting for a connection, the statement and the commit. The statement timeout fires first
// while the statement runs.
const selectionFreezeDeadlineMargin = 2 * time.Second

// selectableModelsFromWhere is every model a selection can hold: the active revision of an
// active model in an active entry, with the entry's active revision. GET /v1/models lists the
// same models. The where clause continues with "and" conditions.
const selectableModelsFromWhere = `from model_revisions
		join models on models.id = model_revisions.model_id
		join entries on entries.id = models.entry_id
		join entry_revisions on entry_revisions.entry_id = models.entry_id and entry_revisions.state = 'active'
		where model_revisions.state = 'active' and models.state = 'active' and entries.state = 'active'`

// Freeze saves a selection with the revisions active now, or returns why it saved nothing:
// *UnknownSelectionModelsError, ErrSelectionEmpty, ErrSelectionTooLarge, ErrSelectionsFull or
// ErrSelectionFreezeTimeout.
//
// One statement checks the named models, freezes every matching model and checks the limits, so
// all of it sees the catalog at one moment. It runs in a transaction of its own, which raises the
// statement timeout to freeze.StatementTimeout: a selection of the whole catalog takes longer than
// the connection's timeout allows a read. The whole freeze, waiting for a connection included,
// ends selectionFreezeDeadlineMargin after that, with ErrSelectionFreezeTimeout.
func (r *SelectionsRepository) Freeze(ctx context.Context, freeze SelectionFreeze) (_ *models.Selection, err error) {
	if freeze.MaxModels <= 0 || freeze.MaxStoredModels <= 0 || freeze.StatementTimeout <= 0 {
		return nil, errors.New("selection freeze needs positive limits and statement timeout")
	}
	if len(freeze.ModelIDs) == 0 && len(freeze.Searches) == 0 {
		return nil, errors.New("selection freeze needs model IDs or a search")
	}
	query, args, err := selectionFreezeQuery(freeze)
	if err != nil {
		return nil, fmt.Errorf("build selection freeze query: %w", err)
	}

	freezeCtx, cancel := context.WithTimeout(ctx, freeze.StatementTimeout+selectionFreezeDeadlineMargin)
	defer cancel()
	// outOfTime reports whether err came from the freeze running out of time, not from the caller
	// giving up: the deadline passed, or PostgreSQL canceled the statement at its timeout.
	outOfTime := func(err error) bool {
		return ctx.Err() == nil && (errors.Is(freezeCtx.Err(), context.DeadlineExceeded) || isQueryCanceled(err))
	}
	timeoutError := func(step string, err error) error {
		return fmt.Errorf("%w (%s, %s): %w", ErrSelectionFreezeTimeout, freeze.StatementTimeout, step, err)
	}

	tx, err := r.db.BeginTxx(freezeCtx, nil)
	if err != nil {
		if outOfTime(err) {
			return nil, timeoutError("waiting for a connection", err)
		}
		return nil, fmt.Errorf("begin selection freeze: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// A canceled context has already rolled the transaction back (sql.ErrTxDone).
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back selection freeze: %w", rollbackErr))
		}
	}()
	if _, err := tx.ExecContext(freezeCtx, `select set_config('statement_timeout', $1, true)`,
		strconv.FormatInt(freeze.StatementTimeout.Milliseconds(), 10)); err != nil {
		if outOfTime(err) {
			return nil, timeoutError("setting the statement timeout", err)
		}
		return nil, fmt.Errorf("set selection freeze statement timeout: %w", err)
	}
	var result struct {
		MissingModelIDs pq.StringArray `db:"missing_model_ids"`
		ModelCount      int64          `db:"model_count"`
		EntryCount      int64          `db:"entry_count"`
		ChargedRows     int64          `db:"charged_rows"`
		StoredRows      int64          `db:"stored_rows"`
		Saved           bool           `db:"saved"`
		// CreatedAt is the freeze statement's start on the database clock: the moment of the
		// snapshot the statement read the catalog from.
		CreatedAt sql.NullTime `db:"created_at"`
	}
	if err := tx.GetContext(freezeCtx, &result, query, args...); err != nil {
		if outOfTime(err) {
			return nil, timeoutError("freezing", err)
		}
		return nil, fmt.Errorf("freeze selection: %w", err)
	}
	switch {
	case len(result.MissingModelIDs) > 0:
		return nil, &UnknownSelectionModelsError{ModelIDs: result.MissingModelIDs}
	case result.Saved:
	case result.ModelCount == 0:
		return nil, ErrSelectionEmpty
	case result.ModelCount > int64(freeze.MaxModels):
		return nil, fmt.Errorf("%w: more than %d", ErrSelectionTooLarge, freeze.MaxModels)
	default:
		return nil, fmt.Errorf("%w: %d rows charged to stored selections, %d more would pass %d",
			ErrSelectionsFull, result.StoredRows, result.ChargedRows, freeze.MaxStoredModels)
	}
	if err := tx.Commit(); err != nil {
		if outOfTime(err) {
			return nil, timeoutError("committing", err)
		}
		return nil, fmt.Errorf("commit selection freeze: %w", err)
	}
	committed = true
	return &models.Selection{
		ID:         freeze.ID,
		ModelCount: int(result.ModelCount),
		EntryCount: int(result.EntryCount),
		Request:    freeze.Request,
		CreatedAt:  result.CreatedAt.Time,
	}, nil
}

// selectionFreezeQuery is the statement Freeze runs. It returns the requested models that cannot
// be selected, how many models matched (counting to one past MaxModels at most) and from how many
// entries, how many rows the stored selections hold, and whether it saved the selection.
//
// The budget check reads the selections committed when the statement starts, so saves running at
// the same time can each pass it: the charged rows can exceed MaxStoredModels by at most the saves
// in flight times MaxModels. Locking saves one after another would make each wait for a whole
// catalog's freeze instead.
//
// Each search's conditions are model search conditions, the SQL GET /v1/models runs for the same
// filters. They name their arguments the same way in every search, so each search's fragment is
// bound by itself to positional arguments, and the statement is numbered once at the end.
func selectionFreezeQuery(freeze SelectionFreeze) (string, []any, error) {
	request, err := marshalJSON(freeze.Request)
	if err != nil {
		return "", nil, fmt.Errorf("encode selection request: %w", err)
	}
	statement := positionalStatement{}
	if err := statement.add(`with missing as (
			select requested.model_id, requested.position
			from unnest(cast(:model_ids as text[])) with ordinality as requested(model_id, position)
			where not exists (select 1 `+selectableModelsFromWhere+`
				and models.id = requested.model_id)
		), frozen as materialized (
			select row_number() over (order by model_revisions.created_at, model_revisions.id) as ordinal,
			       model_revisions.id as model_revision_id, entry_revisions.id as entry_revision_id, models.entry_id
			`+selectableModelsFromWhere+`
			  and (`, map[string]any{"model_ids": textArray(freeze.ModelIDs)}); err != nil {
		return "", nil, err
	}
	// alternatives counts the parts of the selection; each part after the first starts with "or".
	alternatives := 0
	separator := func() string {
		alternatives++
		if alternatives == 1 {
			return ""
		}
		return " or "
	}
	if len(freeze.ModelIDs) > 0 {
		if err := statement.add(separator()+"models.id = any(cast(:model_ids as text[]))",
			map[string]any{"model_ids": textArray(freeze.ModelIDs)}); err != nil {
			return "", nil, err
		}
	}
	for index, search := range freeze.Searches {
		searchArgs := map[string]any{}
		conditions, err := modelSearchConditions(search, searchArgs)
		if err != nil {
			return "", nil, fmt.Errorf("build conditions of search %d: %w", index, err)
		}
		fragment := "(" + strings.Join(append([]string{"true"}, conditions...), "\n  and ") + ")"
		if err := statement.add(separator()+fragment, searchArgs); err != nil {
			return "", nil, fmt.Errorf("bind conditions of search %d: %w", index, err)
		}
	}
	if err := statement.add(`)
			  and not (models.id = any(cast(:exclude_model_ids as text[])))
			order by model_revisions.created_at, model_revisions.id
			limit :row_limit
		), counted as (
			select count(*) as model_count, count(distinct entry_id) as entry_count from frozen
		), charged as (
			select counted.model_count + :request_rows as row_count from counted
		), stored as (
			select coalesce(sum(charged_rows), 0) as row_count from selections
		), saved as (
			insert into selections (id, model_count, entry_count, charged_rows, request, created_at)
			select :id, counted.model_count, counted.entry_count, charged.row_count, cast(:request as jsonb), statement_timestamp()
			from counted cross join charged cross join stored
			where not exists (select 1 from missing)
			  and counted.model_count between 1 and :max_models
			  and stored.row_count + charged.row_count <= :max_stored_models
			returning id, created_at
		), saved_models as (
			insert into selection_models (selection_id, ordinal, model_revision_id, entry_revision_id)
			select saved.id, frozen.ordinal, frozen.model_revision_id, frozen.entry_revision_id
			from frozen cross join saved
		)
		select array(select missing.model_id from missing order by missing.position) as missing_model_ids,
		       counted.model_count, counted.entry_count, charged.row_count as charged_rows, stored.row_count as stored_rows,
		       exists (select 1 from saved) as saved, (select saved.created_at from saved) as created_at
		from counted cross join charged cross join stored`, map[string]any{
		"exclude_model_ids": textArray(freeze.ExcludeModelIDs),
		"row_limit":         freeze.MaxModels + 1,
		"id":                freeze.ID,
		"request":           request,
		"max_models":        freeze.MaxModels,
		"max_stored_models": freeze.MaxStoredModels,
		"request_rows":      selectionChargedRows(len(request)),
	}); err != nil {
		return "", nil, err
	}
	if alternatives == 0 {
		return "", nil, errors.New("selection freeze needs model IDs or a search")
	}
	return sqlx.Rebind(sqlx.DOLLAR, statement.text.String()), statement.args, nil
}

// selectionRowBytes is about what one frozen row costs on disk, with its primary key entry.
const selectionRowBytes = 120

// selectionChargedRows is what a stored request of requestBytes counts against the storage budget:
// at least one row, so every selection's charge is more than its frozen rows.
func selectionChargedRows(requestBytes int) int {
	return requestBytes/selectionRowBytes + 1
}

// positionalStatement joins SQL fragments whose named arguments are bound fragment by fragment,
// so two fragments may use the same name for different values.
type positionalStatement struct {
	text strings.Builder
	args []any
}

func (s *positionalStatement) add(fragment string, args map[string]any) error {
	bound, boundArgs, err := sqlx.Named(fragment, args)
	if err != nil {
		return fmt.Errorf("bind statement fragment: %w", err)
	}
	s.text.WriteString(bound)
	s.args = append(s.args, boundArgs...)
	return nil
}

// textArray is values as a SQL text array. A nil slice is an empty array, not NULL: "= any(NULL)"
// is never true and "not (… = any(NULL))" never true either.
func textArray(values []string) pq.StringArray {
	if values == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(values)
}

// isQueryCanceled reports whether PostgreSQL canceled the statement (SQLSTATE 57014): its
// statement timeout fired, or the client canceled it.
func isQueryCanceled(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "57014"
}

// Get returns a stored selection.
func (r *SelectionsRepository) Get(ctx context.Context, id uuid.UUID) (*models.Selection, error) {
	var row struct {
		ID         uuid.UUID `db:"id"`
		ModelCount int       `db:"model_count"`
		EntryCount int       `db:"entry_count"`
		Request    []byte    `db:"request"`
		CreatedAt  time.Time `db:"created_at"`
	}
	err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row,
		`select id, model_count, entry_count, request, created_at from selections where id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSelectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get selection: %w", err)
	}
	selection := models.Selection{
		ID:         row.ID,
		ModelCount: row.ModelCount,
		EntryCount: row.EntryCount,
		CreatedAt:  row.CreatedAt,
	}
	if err := unmarshalJSON(row.Request, &selection.Request); err != nil {
		return nil, fmt.Errorf("decode selection request: %w", err)
	}
	return &selection, nil
}

// ListModels returns up to limit models of a selection with their frozen revisions and the
// files of those revisions, in ordinal order, starting after afterOrdinal (0 for the first page).
// It does not check that the selection exists.
func (r *SelectionsRepository) ListModels(
	ctx context.Context,
	selectionID uuid.UUID,
	afterOrdinal int,
	limit int,
) ([]models.SelectionModel, error) {
	if afterOrdinal < 0 || limit < 0 {
		return nil, errors.New("selection model list needs a non-negative position and limit")
	}
	var rows []struct {
		Ordinal             int           `db:"ordinal"`
		ModelID             string        `db:"model_id"`
		EntryID             string        `db:"entry_id"`
		ModelRevisionID     uuid.UUID     `db:"model_revision_id"`
		ModelRevisionNumber sql.NullInt64 `db:"model_revision_number"`
		EntryRevisionID     uuid.UUID     `db:"entry_revision_id"`
		EntryRevisionNumber sql.NullInt64 `db:"entry_revision_number"`
	}
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows,
		`select selection_models.ordinal,
		        model_revisions.model_id, entry_revisions.entry_id,
		        model_revisions.id as model_revision_id, model_revisions.revision_number as model_revision_number,
		        entry_revisions.id as entry_revision_id, entry_revisions.revision_number as entry_revision_number
		 from selection_models
		 join model_revisions on model_revisions.id = selection_models.model_revision_id
		 join entry_revisions on entry_revisions.id = selection_models.entry_revision_id
		 where selection_models.selection_id = $1 and selection_models.ordinal > $2
		 order by selection_models.ordinal
		 limit $3`, selectionID, afterOrdinal, limit); err != nil {
		return nil, fmt.Errorf("list selection models: %w", err)
	}
	items := make([]models.SelectionModel, 0, len(rows))
	for _, row := range rows {
		// Only active revisions are frozen, and every revision that was active has a number.
		if !row.ModelRevisionNumber.Valid || !row.EntryRevisionNumber.Valid {
			return nil, fmt.Errorf("selection %s model %s: frozen revision has no number", selectionID, row.ModelID)
		}
		items = append(items, models.SelectionModel{
			Ordinal:             row.Ordinal,
			ModelID:             row.ModelID,
			EntryID:             row.EntryID,
			ModelRevisionID:     row.ModelRevisionID,
			ModelRevisionNumber: int(row.ModelRevisionNumber.Int64),
			EntryRevisionID:     row.EntryRevisionID,
			EntryRevisionNumber: int(row.EntryRevisionNumber.Int64),
		})
	}
	if len(items) == 0 {
		return items, nil
	}
	files, err := r.listModelFiles(ctx, selectionID, items[0].Ordinal, items[len(items)-1].Ordinal)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Files = files[items[index].Ordinal]
		if items[index].Files == nil {
			items[index].Files = []models.SelectionFile{}
		}
	}
	return items, nil
}

// selectionModelFilesQuery lists the files of the pinned revisions of a range of a selection's
// models, one row per way a file belongs to a model:
//   - primary: the model revision's primary file;
//   - model: its other files;
//   - run: a file of one of its runs, with the run's direction;
//   - entry: a file of the entry revision.
//
// The triggers of V20261110170100 keep a published revision's files and each file's location,
// size and declared SHA-256 as they were, so these are the files the selection pinned. The
// checksum columns are the checksum job's latest check of the bytes; a file without a checksum
// row reads as pending, as GET /v1/entries/{id}/artifacts reads it.
const selectionModelFilesQuery = `
with page as (
    select selection_models.ordinal, selection_models.model_revision_id, selection_models.entry_revision_id
    from selection_models
    where selection_models.selection_id = $1 and selection_models.ordinal between $2 and $3
), links as (
    select page.ordinal, model_revisions.primary_artifact_id as artifact_id, 'primary' as link, null::text as direction
    from page
    join model_revisions on model_revisions.id = page.model_revision_id
    where model_revisions.primary_artifact_id is not null
    union all
    select page.ordinal, model_revision_artifacts.artifact_id, 'model', null
    from page
    join model_revision_artifacts on model_revision_artifacts.model_revision_id = page.model_revision_id
    join model_revisions on model_revisions.id = page.model_revision_id
    where model_revisions.primary_artifact_id is distinct from model_revision_artifacts.artifact_id
    union all
    select page.ordinal, run_artifacts.artifact_id, 'run', run_artifacts.direction
    from page
    join model_revision_runs on model_revision_runs.model_revision_id = page.model_revision_id
    join run_artifacts on run_artifacts.run_id = model_revision_runs.run_id
    union all
    select page.ordinal, entry_revision_artifacts.artifact_id, 'entry', null
    from page
    join entry_revision_artifacts on entry_revision_artifacts.entry_revision_id = page.entry_revision_id
)
select links.ordinal, links.link, links.direction,
       artifacts.id, artifacts.name, artifacts.level, artifacts.type, artifacts.format, artifacts.uri,
       artifacts.size_bytes, artifacts.sha256,
       coalesce(artifact_checksums.status, 'pending') as checksum_status,
       artifact_checksums.sha256 as checksum_sha256,
       artifact_checksums.size_bytes as checksum_size_bytes,
       artifact_checksums.stored_uri as checksum_stored_uri,
       artifact_checksums.copy_status as checksum_copy_status,
       artifact_checksums.checked_at as checksum_checked_at,
       selections.created_at as catalog_date
from links
join artifacts on artifacts.id = links.artifact_id
join selections on selections.id = $1
left join artifact_checksums on artifact_checksums.artifact_id = artifacts.id
order by links.ordinal, artifacts.id`

type selectionModelFileRow struct {
	Ordinal            int            `db:"ordinal"`
	Link               string         `db:"link"`
	Direction          sql.NullString `db:"direction"`
	ID                 uuid.UUID      `db:"id"`
	Name               string         `db:"name"`
	Level              string         `db:"level"`
	Type               string         `db:"type"`
	Format             sql.NullString `db:"format"`
	URI                sql.NullString `db:"uri"`
	SizeBytes          sql.NullInt64  `db:"size_bytes"`
	SHA256             sql.NullString `db:"sha256"`
	ChecksumStatus     string         `db:"checksum_status"`
	ChecksumSHA256     sql.NullString `db:"checksum_sha256"`
	ChecksumSizeBytes  sql.NullInt64  `db:"checksum_size_bytes"`
	ChecksumStoredURI  sql.NullString `db:"checksum_stored_uri"`
	ChecksumCopyStatus sql.NullString `db:"checksum_copy_status"`
	ChecksumCheckedAt  sql.NullTime   `db:"checksum_checked_at"`
	CatalogDate        time.Time      `db:"catalog_date"`
}

// listModelFiles returns the pinned files of the selection's models from firstOrdinal to
// lastOrdinal, by ordinal. A file that belongs to a model in several ways is listed once with
// every role. Files are ordered by their first role, then name, then ID.
func (r *SelectionsRepository) listModelFiles(
	ctx context.Context,
	selectionID uuid.UUID,
	firstOrdinal, lastOrdinal int,
) (map[int][]models.SelectionFile, error) {
	var rows []selectionModelFileRow
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, selectionModelFilesQuery,
		selectionID, firstOrdinal, lastOrdinal); err != nil {
		return nil, fmt.Errorf("list selection model files: %w", err)
	}
	type fileKey struct {
		ordinal int
		id      uuid.UUID
	}
	files := make(map[fileKey]*models.SelectionFile)
	order := make([]fileKey, 0, len(rows))
	for _, row := range rows {
		role, err := selectionFileRole(row.Link, row.Direction)
		if err != nil {
			return nil, fmt.Errorf("selection %s model %d file %s: %w", selectionID, row.Ordinal, row.ID, err)
		}
		key := fileKey{ordinal: row.Ordinal, id: row.ID}
		if file, found := files[key]; found {
			file.Roles = append(file.Roles, role)
			continue
		}
		file, err := selectionFileFromRow(row, role)
		if err != nil {
			return nil, fmt.Errorf("selection %s model %d file %s: %w", selectionID, row.Ordinal, row.ID, err)
		}
		files[key] = file
		order = append(order, key)
	}
	byOrdinal := make(map[int][]models.SelectionFile)
	for _, key := range order {
		file := files[key]
		file.Roles = sortedSelectionFileRoles(file.Roles)
		byOrdinal[key.ordinal] = append(byOrdinal[key.ordinal], *file)
	}
	for ordinal := range byOrdinal {
		sort.SliceStable(byOrdinal[ordinal], func(i, j int) bool {
			left, right := byOrdinal[ordinal][i], byOrdinal[ordinal][j]
			leftRank, rightRank := selectionFileRoleRank(left.Roles[0]), selectionFileRoleRank(right.Roles[0])
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.ID.String() < right.ID.String()
		})
	}
	return byOrdinal, nil
}

func selectionFileFromRow(row selectionModelFileRow, role models.SelectionFileRole) (*models.SelectionFile, error) {
	checksum := models.ArtifactChecksum{
		Status:     models.ChecksumStatus(row.ChecksumStatus),
		SHA256:     stringPtrFromSQL(row.ChecksumSHA256),
		SizeBytes:  int64PtrFromSQL(row.ChecksumSizeBytes),
		StoredURI:  stringPtrFromSQL(row.ChecksumStoredURI),
		CopyStatus: copyStatusFromSQL(row.ChecksumCopyStatus),
		CheckedAt:  timePtrFromSQL(row.ChecksumCheckedAt),
	}
	recorded, source, afterSave, err := recordedSelectionSHA256(stringPtrFromSQL(row.SHA256), checksum, row.CatalogDate)
	if err != nil {
		return nil, err
	}
	return &models.SelectionFile{
		ID:                   row.ID,
		Roles:                []models.SelectionFileRole{role},
		Name:                 row.Name,
		Level:                models.ArtifactLevel(row.Level),
		Type:                 models.ArtifactType(row.Type),
		Format:               stringPtrFromSQL(row.Format),
		URI:                  stringPtrFromSQL(row.URI),
		SizeBytes:            int64PtrFromSQL(row.SizeBytes),
		RecordedSHA256:       recorded,
		RecordedSHA256Source: source,
		RecordedAfterSave:    afterSave,
		Checksum:             checksum,
	}, nil
}

// recordedSelectionSHA256 is the SHA-256 a selection names for a file:
//   - the declared SHA-256, when the file was added with one;
//   - otherwise the checksum job's SHA-256 once it verified the bytes, with afterSave set when
//     it read them after the selection was saved (catalogDate);
//   - otherwise none: the job has not read the bytes (pending), could not (failed), or found
//     them differing from the declared size (mismatch), so no SHA-256 describes the file.
func recordedSelectionSHA256(
	declared *string,
	checksum models.ArtifactChecksum,
	catalogDate time.Time,
) (sha256 *string, source models.SelectionSHA256Source, afterSave bool, err error) {
	if declared != nil {
		return declared, models.SelectionSHA256SourceDeclared, false, nil
	}
	switch checksum.Status {
	case models.ChecksumStatusVerified:
		if checksum.SHA256 == nil || checksum.CheckedAt == nil {
			return nil, "", false, errors.New("verified checksum has no sha256 or check time")
		}
		return checksum.SHA256, models.SelectionSHA256SourceComputed, checksum.CheckedAt.After(catalogDate), nil
	case models.ChecksumStatusPending, models.ChecksumStatusFailed, models.ChecksumStatusMismatch:
		return nil, models.SelectionSHA256SourceNotRecorded, false, nil
	default:
		return nil, "", false, fmt.Errorf("unknown checksum status: %s", checksum.Status)
	}
}

// selectionFileRole reads how a selectionModelFilesQuery row links its file to the model.
func selectionFileRole(link string, direction sql.NullString) (models.SelectionFileRole, error) {
	switch link {
	case "primary":
		return models.SelectionFileRolePrimary, nil
	case "model":
		return models.SelectionFileRoleModel, nil
	case "entry":
		return models.SelectionFileRoleEntry, nil
	case "run":
		switch models.RunArtifactDirection(direction.String) {
		case models.RunArtifactDirectionInput:
			return models.SelectionFileRoleRunInput, nil
		case models.RunArtifactDirectionOutput:
			return models.SelectionFileRoleRunOutput, nil
		default:
			return "", fmt.Errorf("unknown run file direction: %q", direction.String)
		}
	default:
		return "", fmt.Errorf("unknown selection file link: %s", link)
	}
}

// selectionFileRoles is every SelectionFileRole in the order files and roles are listed.
var selectionFileRoles = []models.SelectionFileRole{
	models.SelectionFileRolePrimary,
	models.SelectionFileRoleModel,
	models.SelectionFileRoleRunInput,
	models.SelectionFileRoleRunOutput,
	models.SelectionFileRoleEntry,
}

// selectionFileRoleRank is role's place in selectionFileRoles. Every role comes from
// selectionFileRole, so it is always listed there.
func selectionFileRoleRank(role models.SelectionFileRole) int {
	return slices.Index(selectionFileRoles, role)
}

// sortedSelectionFileRoles returns roles in selectionFileRoles order, each once.
func sortedSelectionFileRoles(roles []models.SelectionFileRole) []models.SelectionFileRole {
	sorted := make([]models.SelectionFileRole, 0, len(roles))
	for _, known := range selectionFileRoles {
		if slices.Contains(roles, known) {
			sorted = append(sorted, known)
		}
	}
	return sorted
}
