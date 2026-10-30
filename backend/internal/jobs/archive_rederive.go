package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

const (
	// archiveRederiveResolver is the resolved_by value of the review rows the job resolves.
	archiveRederiveResolver = "archive-rederive-job"
	// archiveRederiveMaxAttempts is how many real runs try a revision whose file keeps failing.
	archiveRederiveMaxAttempts = 3
	// Review rows the C1 migrations wrote that the job can resolve.
	entryListsReviewIssue         = "ligands_cofactors_rederive_from_archive"
	polymerEntityMutationsIssue   = "mutations_need_review"
	entryReviewObjectType         = "entry"
	polymerEntityReviewObjectType = "polymer_entity"
)

// ArchiveRederiveConfig configures the archive re-derivation job (T1-C2).
type ArchiveRederiveConfig struct {
	// MaxEntriesPerRun caps the revisions one run looks at; 0 means no cap.
	MaxEntriesPerRun int `mapstructure:"max_entries_per_run"`
	// PageSize is how many revisions one page lists and derives before writing them.
	PageSize int `mapstructure:"page_size"`
	// DownloadConcurrency caps the RCSB downloads in flight.
	DownloadConcurrency int `mapstructure:"download_concurrency"`
	// RunFor is how long after its start a run keeps taking new pages.
	RunFor time.Duration `mapstructure:"run_for"`
	// DryRun logs what the job would change, in rows marked dry_run, and changes nothing else.
	DryRun bool `mapstructure:"dry_run"`
}

func (c ArchiveRederiveConfig) validate() error {
	switch {
	case c.MaxEntriesPerRun < 0:
		return errors.New("max_entries_per_run must not be negative")
	case c.PageSize <= 0:
		return errors.New("page_size must be positive")
	case c.DownloadConcurrency <= 0:
		return errors.New("download_concurrency must be positive")
	case c.RunFor <= 0:
		return errors.New("run_for must be positive")
	}
	return nil
}

// ArchiveRederiveJob derives, for every active archive entry revision, the entry's ligands,
// cofactors, decision record and glycans, and its polymer entities' UniProt-numbered mutations,
// from the head of the entry's RCSB mmCIF, as the entry sync does when it applies a new archive
// version. It changes the active revision in place, without a new revision, and logs the old
// and new values in archive_rederivations so the change can be reverted. Lists a person set
// are kept.
type ArchiveRederiveJob struct {
	database   *db.DB
	rcsbClient *rcsb.RemoteClient
	cfg        ArchiveRederiveConfig
	logger     *slog.Logger
	now        func() time.Time
	sleep      func(ctx context.Context, pause time.Duration) error
}

func NewArchiveRederiveJob(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	cfg ArchiveRederiveConfig,
	logger *slog.Logger,
) (*ArchiveRederiveJob, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if rcsbClient == nil {
		return nil, errors.New("RCSB client is nil")
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("archive re-derivation config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ArchiveRederiveJob{
		database:   database,
		rcsbClient: rcsbClient,
		cfg:        cfg,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
		sleep:      sleepContext,
	}, nil
}

// sleepContext waits for pause, or returns the context's error if it ends first.
func sleepContext(ctx context.Context, pause time.Duration) error {
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// entryDerivation is what the job derived for one revision: the log row, and the changes a real
// run writes.
type entryDerivation struct {
	row       models.ArchiveRederivation
	compounds *models.ArchiveCompoundValues
	entities  []models.ArchivePolymerEntityMutations
	// parsedEntities are the changed polymer entities whose mutations are now all parsed.
	parsedEntities []string
}

func (d entryDerivation) changes() bool {
	return d.compounds != nil || len(d.entities) > 0
}

// Run takes pages of unfinished revisions until none is left, MaxEntriesPerRun is reached or
// RunFor is over. Each page downloads and derives its revisions, then writes them, each in its
// own transaction. After each page it pauses as long as that page's writes took, so the job
// keeps the database at most about half busy however slow it is.
func (j *ArchiveRederiveJob) Run(ctx context.Context) error {
	started := j.now()
	runUntil := started.Add(j.cfg.RunFor)
	j.logger.Info("archive re-derivation started", "dry_run", j.cfg.DryRun)
	counts := map[models.ArchiveRederivationOutcome]int{}
	revisions := 0
	after := uuid.Nil
	for {
		limit := j.cfg.PageSize
		if j.cfg.MaxEntriesPerRun > 0 {
			limit = min(limit, j.cfg.MaxEntriesPerRun-revisions)
		}
		if limit <= 0 || !j.now().Before(runUntil) {
			break
		}
		candidates, err := j.database.ArchiveRederivations.ListCandidates(ctx, after, limit, archiveRederiveMaxAttempts)
		if err != nil {
			return fmt.Errorf("list candidates: %w", err)
		}
		if len(candidates) == 0 {
			break
		}
		derivations, err := j.deriveAll(ctx, candidates)
		if err != nil {
			return fmt.Errorf("derive page after revision %s: %w", after, err)
		}
		writesStarted := j.now()
		for _, derivation := range derivations {
			outcome, err := j.write(ctx, derivation)
			if err != nil {
				return fmt.Errorf("write revision %s: %w", derivation.row.EntryRevisionID, err)
			}
			counts[outcome]++
		}
		revisions += len(candidates)
		after = candidates[len(candidates)-1].EntryRevisionID
		if err := j.sleep(ctx, j.now().Sub(writesStarted)); err != nil {
			return fmt.Errorf("pause after revision %s: %w", after, err)
		}
	}
	j.logger.Info("archive re-derivation finished",
		"dry_run", j.cfg.DryRun,
		"revisions", revisions,
		"derived", counts[models.ArchiveRederivationDerived],
		"unchanged", counts[models.ArchiveRederivationUnchanged],
		"skipped_person_set", counts[models.ArchiveRederivationSkippedPersonSet],
		"skipped_in_review", counts[models.ArchiveRederivationSkippedInReview],
		"skipped_not_active", counts[models.ArchiveRederivationSkippedNotActive],
		"skipped_incomplete_file", counts[models.ArchiveRederivationSkippedIncompleteFile],
		"failed", counts[models.ArchiveRederivationFailed],
		"duration", j.now().Sub(started).String())
	return nil
}

// deriveAll derives the candidates DownloadConcurrency at a time and returns them in order. It
// fails only when a read of our database fails or the run is cancelled.
func (j *ArchiveRederiveJob) deriveAll(
	ctx context.Context,
	candidates []models.ArchiveRederivationCandidate,
) ([]entryDerivation, error) {
	derivations := make([]entryDerivation, len(candidates))
	errs := make([]error, len(candidates))
	slots := make(chan struct{}, j.cfg.DownloadConcurrency)
	var wait sync.WaitGroup
	for index, candidate := range candidates {
		wait.Add(1)
		go func() {
			defer wait.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			derivations[index], errs[index] = j.derive(ctx, candidate)
		}()
	}
	wait.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return derivations, nil
}

// derive reads one candidate's revision and polymer entities and the head of its RCSB mmCIF,
// and works out what changes. A file that cannot be downloaded or derived gives a failed row.
func (j *ArchiveRederiveJob) derive(
	ctx context.Context,
	candidate models.ArchiveRederivationCandidate,
) (entryDerivation, error) {
	row := models.ArchiveRederivation{
		EntryRevisionID: candidate.EntryRevisionID,
		EntryID:         candidate.EntryID,
		PDBID:           candidate.PDBID,
		DryRun:          j.cfg.DryRun,
	}
	revision, err := j.database.Entries.Get(ctx, db.EntryRevisionFilters{ID: &candidate.EntryRevisionID})
	if err != nil {
		return entryDerivation{}, fmt.Errorf("get revision %s: %w", candidate.EntryRevisionID, err)
	}
	entities, err := j.database.PolymerEntities.List(ctx, db.PolymerEntityFilters{EntryRevisionID: &candidate.EntryRevisionID})
	if err != nil {
		return entryDerivation{}, fmt.Errorf("list polymer entities of revision %s: %w", candidate.EntryRevisionID, err)
	}
	file, err := readArchiveFile(ctx, j.rcsbClient, j.logger, candidate.PDBID)
	if ctx.Err() != nil {
		return entryDerivation{}, fmt.Errorf("read %s: %w", candidate.PDBID, ctx.Err())
	}
	if err != nil {
		j.logger.Warn("archive re-derivation failed", "pdb_id", candidate.PDBID, "err", err)
		row.Outcome = models.ArchiveRederivationFailed
		row.Error = new(err.Error())
		return entryDerivation{row: row}, nil
	}
	row.ReferenceVersion = optionalString(file.referenceVersion)
	if file.composition == nil {
		row.Outcome = models.ArchiveRederivationSkippedIncompleteFile
		row.Error = new(file.incompleteReason)
		return entryDerivation{row: row}, nil
	}

	derivation := entryDerivation{}
	before, after := models.ArchiveRederivationValues{}, models.ArchiveRederivationValues{}
	personSet := entryListsPersonSet(*revision)
	if !personSet {
		current := compoundValuesOf(*revision)
		derived := compoundValuesFromComposition(*file.composition)
		if !reflect.DeepEqual(current, derived) {
			before.Compounds, after.Compounds = &current, &derived
			derivation.compounds = &derived
		}
	}
	desired := applyArchiveMutations(entities, entities, file.mutations)
	desired = applyDepositorPolymerEntities(desired, revision.Metadata.DepositorPolymerEntities)
	for index, entity := range entities {
		current, next := entityMutationsOf(entity), entityMutationsOf(desired[index])
		if reflect.DeepEqual(current, next) {
			continue
		}
		before.PolymerEntities = append(before.PolymerEntities, current)
		after.PolymerEntities = append(after.PolymerEntities, next)
		derivation.entities = append(derivation.entities, next)
		if next.MutationsStatus != nil && *next.MutationsStatus == models.MutationsStatusParsed {
			derivation.parsedEntities = append(derivation.parsedEntities, entity.ID.String())
		}
	}

	switch {
	case personSet:
		row.Outcome = models.ArchiveRederivationSkippedPersonSet
	case derivation.changes():
		row.Outcome = models.ArchiveRederivationDerived
	default:
		row.Outcome = models.ArchiveRederivationUnchanged
	}
	if derivation.changes() {
		row.Before, row.After = &before, &after
	}
	derivation.row = row
	return derivation, nil
}

// write logs one derivation and, in a real run, applies its changes, in one transaction that
// holds the entry's lock. Changes are applied only while the derived revision is still the
// active one and no revision of the entry is in review: a draft made before the change would
// drop it when it is activated. It returns the outcome it logged.
func (j *ArchiveRederiveJob) write(ctx context.Context, derivation entryDerivation) (models.ArchiveRederivationOutcome, error) {
	row := derivation.row
	if j.cfg.DryRun || !derivation.changes() {
		if err := j.database.ArchiveRederivations.Record(ctx, row); err != nil {
			return "", fmt.Errorf("record: %w", err)
		}
		return row.Outcome, nil
	}
	err := j.database.Do(ctx, func(ctx context.Context) error {
		if err := j.database.Entries.Lock(ctx, row.EntryID); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		skipped, err := j.skipReason(ctx, row)
		if err != nil {
			return err
		}
		if skipped != "" {
			row.Outcome, row.Before, row.After = skipped, nil, nil
			return j.database.ArchiveRederivations.Record(ctx, row)
		}
		if derivation.compounds != nil {
			if err := j.database.Entries.UpdateComposition(ctx, row.EntryRevisionID, *derivation.compounds); err != nil {
				return fmt.Errorf("update composition: %w", err)
			}
			if _, err := j.database.ArchiveRederivations.ResolveMigrationReviews(ctx,
				entryReviewObjectType, entryListsReviewIssue, []string{row.EntryID}, archiveRederiveResolver,
			); err != nil {
				return fmt.Errorf("resolve entry review rows: %w", err)
			}
		}
		for _, mutations := range derivation.entities {
			if err := j.database.PolymerEntities.UpdateMutations(ctx, mutations); err != nil {
				return fmt.Errorf("update mutations: %w", err)
			}
		}
		if _, err := j.database.ArchiveRederivations.ResolveMigrationReviews(ctx,
			polymerEntityReviewObjectType, polymerEntityMutationsIssue, derivation.parsedEntities, archiveRederiveResolver,
		); err != nil {
			return fmt.Errorf("resolve polymer entity review rows: %w", err)
		}
		return j.database.ArchiveRederivations.Record(ctx, row)
	})
	if err != nil {
		return "", fmt.Errorf("apply changes: %w", err)
	}
	return row.Outcome, nil
}

// skipReason returns the outcome that keeps the job from changing row's revision now, or "" when
// it may change it.
func (j *ArchiveRederiveJob) skipReason(ctx context.Context, row models.ArchiveRederivation) (models.ArchiveRederivationOutcome, error) {
	active, err := j.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID: &row.EntryID,
		State:   new(models.RevisionStateActive),
	})
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		return models.ArchiveRederivationSkippedNotActive, nil
	}
	if err != nil {
		return "", fmt.Errorf("get active revision: %w", err)
	}
	if active.ID != row.EntryRevisionID {
		return models.ArchiveRederivationSkippedNotActive, nil
	}
	inReview, err := j.database.Entries.List(ctx, db.EntryRevisionFilters{
		EntryID: &row.EntryID,
		State:   new(models.RevisionStateInReview),
		Limit:   new(1),
	})
	if err != nil {
		return "", fmt.Errorf("list revisions in review: %w", err)
	}
	if len(inReview) > 0 {
		return models.ArchiveRederivationSkippedInReview, nil
	}
	return "", nil
}

func compoundValuesOf(revision models.EntryRevision) models.ArchiveCompoundValues {
	return models.ArchiveCompoundValues{
		Ligands:           revision.Metadata.Ligands,
		Cofactors:         revision.Metadata.Cofactors,
		CompoundSources:   revision.CompoundSources,
		GlycanDescriptors: revision.GlycanDescriptors,
	}
}

// entityMutationsOf returns an entity's mutation fields. An empty list is nil, as stored: the
// metadata leaves an empty list out.
func entityMutationsOf(entity models.PolymerEntity) models.ArchivePolymerEntityMutations {
	var mutations []string
	if len(entity.Metadata.Mutations) > 0 {
		mutations = entity.Metadata.Mutations
	}
	return models.ArchivePolymerEntityMutations{
		ID:              entity.ID,
		Mutations:       mutations,
		MutationsRaw:    entity.Metadata.MutationsRaw,
		MutationsStatus: entity.Metadata.MutationsStatus,
	}
}
