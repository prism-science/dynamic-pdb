# Schema inventory

`schema_inventory.sql` is one `SELECT` that lists every record breaking a rule added by the
C1 schema migrations. It never writes and is not part of Flyway (it lives outside `db/`).
`internal/db/schema_migrations_test.go` runs it in a read-only transaction against fixture data.
`requarantine_duplicate_metric_keys.sql` is the one script here that writes; see the recovery steps below.

```sh
# repair list: one row per violating record
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/schema_inventory.sql

# counts per check (checks with no violations do not appear)
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -At -c \
  "select check_name, count(*) from ($(grep -v '^--' backend/migrations/inventory/schema_inventory.sql | sed 's/;$//' )) q group by 1 order by 1"
```

`counts.sql` (read-only) prints row counts and checksums that the migrations must not change; run it before
and after and diff the outputs to show no data was lost. `old_shape_fixture.sql` is deliberately awkward
old-shape data for trying the migrations on a scratch database (the Go test uses the same file).

Columns: `check_name`, `object_type`, `object_id`, `details` (jsonb), `check_count`.
Records with missing information stay public. An omitted `model_type` or a NULL `sha256` is not
malformed data. The three provenance checks follow the Go write rules
(`backend/internal/validation/provenance.go`), where an omitted `model_type` counts as computed:
`metric_missing_software` (a metric of a computed model with no software and no `run_id`),
`run_missing_software_version` (a run of a computed model with no version) and
`computed_model_without_run`. Their rows are the repair list for the records the website labels
"information missing" (the model detail GET returns them as `missing_info`).

Records the migrations could not resolve themselves are in `schema_migration_review`
(`object_type, object_id, issue, details`): PDB-backed entries whose ligand/cofactor lists wait for
the archive re-derivation (C2), entries merged from models, mutation texts (strings or lists) holding a token that is not a plain substitution, and
duplicate metric keys. Original model lists are kept in `model_revisions.metadata` and copied there.
A row a later job settled has `resolved_at` and `resolved_by` (see "Archive re-derivation" below);
open rows have both NULL.

The data sync writes rows here too, in `enforce` mode only. When an entry's or a model's latest RCSB
version breaks the schema rules, the sync keeps the current revision, does not record the version
and tries again at its normal 7–14-day cadence. It opens one `sync_validation_failed` row per object
(`object_type` `entry` or `model`, `details` = PDB ID, version and field errors), and resolves it
(`resolved_by = 'data-sync'`) once a later run applies or records a version.

Every check that shipped `NOT VALID` is validated by a later migration:
`entry_revisions_composition_lists_chk` by `V20261103230000`, `run_artifacts_direction_chk` and
`run_artifacts_position_chk` by `V20261104151000` and `V20261104151100`, and the enforce checks below.
`select conname from pg_constraint where contype = 'c' and not convalidated` lists any that are not.
Ligand/cofactor lists are checked in jsonpath `strict` mode (`strict $.ligands[*] ? (@.type() != "string")`)
behind a `jsonb_typeof(...) = 'array'` guard: lax mode would unwrap an inner array, so `[["x"]]` would pass as
all strings. Every element of a merged list must be a JSON string.

## Recovering a failed `V20261023090500` (unique index on revision + metric key)

The index build fails when two links of one model revision still share a metric key, for example a duplicate
attached after the backfill (the trigger gives every new link its real key, so rerunning the backfill
`V20261023090400` finds no NULL keys and changes nothing). The build is not transactional: it can leave an
INVALID index and Flyway records the migration as failed (the same happens after an interrupted
`090400`, `090500` or `090600`). Recover with:

```sh
# 1. drop the leftover index (no-op when the build never created one)
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c 'DROP INDEX CONCURRENTLY IF EXISTS model_revision_metrics_revision_key_idx'
# 2. quarantine the remaining duplicates; idempotent, safe at any time, prints the links it re-keyed
#    (zero rows: nothing was left). Nothing is deleted; review rows are written.
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/requarantine_duplicate_metric_keys.sql
# 3. remove the failed row from the Flyway history, then migrate again
flyway repair
flyway migrate
```

The script is the supported way to find and quarantine duplicates before or after the index exists; the
unique index itself then rejects a new duplicate with a unique violation (the application sees a failed
write, as with any constraint). A trigger that quarantines a would-be duplicate silently was rejected: it
would make the "one metric per key" rule unenforceable, and two concurrent inserts do not see each other.

Quarantined duplicate metric rows carry `metric_key = '<key>#duplicate#<metric_id>'` (still unique, so the
unique index accepts them) and the revision is listed in `schema_migration_review` as
`duplicate_metric_key:<key>`. The data sync and revision creation copy the quarantine verbatim, so a model
with duplicates keeps getting new revisions. After repair (unlink the metrics that should go, keeping
nothing silently), bring the remaining rows under the real key:
`update model_revision_metrics l set metric_key = m.key from metrics m where m.id = l.metric_id and l.metric_key like '%#duplicate#%' and not exists (select 1 from model_revision_metrics o join metrics om on om.id = o.metric_id where o.model_revision_id = l.model_revision_id and om.key = m.key and o.metric_id <> l.metric_id);`

## Checks that shipped with `validation.mode=enforce` (T1-E)

These CHECK constraints were deliberately **not** in the C1 migrations. Adding them `NOT VALID` skips
legacy rows, but an UPDATE of an existing violating row (the app archives the previous
active revision with an UPDATE whenever a new revision activates) is checked too, so a legacy value
such as `model_type = 'Foo'` would block that model from ever getting a new revision. And in
`log_only` mode they would have turned requests the Go validators merely log into an error from the
database. They shipped once production validated in `enforce` mode, after the log-only soak showed
no violations and each inventory check below returned **zero rows** on prod (2026-11-04):

| Constraint | Added `NOT VALID` | Validated | Inventory check that must be empty |
|---|---|---|---|
| `metrics_key_known_chk` | `V20261104150000` | `V20261104150400` | `metric_unknown_key` |
| `model_revisions_model_type_chk` | `V20261104150100` | `V20261104150500` | `model_invalid_type` |
| `model_revisions_purpose_chk` | `V20261104150100` | `V20261104150600` | `model_invalid_purpose` |
| `artifacts_sha256_format_chk` | `V20261104150200` | `V20261104150700` | `artifact_malformed_sha256` |
| `artifacts_size_bytes_chk` | `V20261104150200` | `V20261104150800` | `artifact_negative_size` |
| `polymer_entities_uniprot_mappings_chk` | `V20261104150300` | `V20261104150900` | `polymer_entity_uniprot_mapping_invalid` |

Each table gets its own `NOT VALID` migration, one lock at a time: these take ACCESS EXCLUSIVE for
milliseconds and scan nothing, and `lock_timeout` makes a migration fail cleanly instead of queueing
behind a long transaction (rerun it). Each `VALIDATE CONSTRAINT` is its own migration, so it runs in
its own transaction under SHARE UPDATE EXCLUSIVE: reads and writes continue while it scans. On a
clone of the prod copy (data of 2026-10-23: 843k metrics, 372k model revisions, 568k artifacts,
241k polymer entities, 495k run artifacts) all twelve migrations together took about 9 s, the longest
3 s (`model_revisions`).

If a `VALIDATE` meets a breaking row, its migration fails and applies nothing: the check stays
`NOT VALID` and the migration Job stops later deploys. Repair the rows the inventory check lists, then
rerun the migration Job. PostgreSQL rolls the failed migration back with its history row; if Flyway
still lists it as failed, run `flyway repair` first, as for `V20261023090500` above.

The data sync used to store SIFTS mappings without a release when the release lookup failed
(`lib/archive/polymer.go`). In enforce mode it validates polymer entities before writing and skips
such an RCSB version instead (`sync_validation_failed`). A database check violation (`23514`) that
reaches the API answers 422 `CONSTRAINT_VIOLATION` (`writeMutationError`), not 500.

The migration tests (`backend/internal/db/schema_migrations_test.go`) repair the old-shape fixture's
breaking rows before these migrations (`enforceCheckRepairSQL`), and check on the unrepaired fixture
that each `VALIDATE` fails and leaves its check `NOT VALID`.

## Archive re-derivation (T1-C2)

The archive re-derivation job (`backend/cmd/archive-rederive-job`, CronJob in `deploy/README.md`)
derives the entry lists, `compound_sources` and `glycan_descriptors` of archive entries, and the
UniProt-numbered mutations of their polymer entities, from the head of each RCSB mmCIF. It changes
the active revision in place and logs each revision it looked at in `archive_rederivations`: the
outcome (described in `V20261030150100__archive_rederivations.sql`), and for a change the old values
in `before` and the new ones in `after`. When an entry's lists are derived from the file, filled by
this run or by the sync before it, it resolves the entry's `ligands_cofactors_rederive_from_archive`
row; when an entity's mutations become all parsed it resolves the entity's `mutations_need_review`
row (`resolved_by = 'archive-rederive-job'`). A dry run logs the same outcomes, skips included.

Polymer entities are shared by the revisions of an entry, so a mutation the job fixes also shows on
the entry's archived revisions, as with the C1 migrations.

```sh
# outcomes so far (dry-run rows apart)
psql "$DATABASE_URL" -At -c \
  "select dry_run, outcome, count(*) from archive_rederivations group by 1, 2 order by 1, 2"

# why files failed or were incomplete
psql "$DATABASE_URL" -At -c \
  "select outcome, error, count(*) from archive_rederivations
   where outcome in ('failed', 'skipped_incomplete_file') group by 1, 2 order by 3 desc"

# review rows still open after the job, for the researchers
psql "$DATABASE_URL" -At -c \
  "select issue, count(*) from schema_migration_review where resolved_at is null group by 1 order by 1"

# a local dry run of 1000 revisions (from backend/, against the local config's database)
DYNAMIC_PDB_ARCHIVE_REDERIVE_DRY_RUN=true DYNAMIC_PDB_ARCHIVE_REDERIVE_MAX_ENTRIES_PER_RUN=1000 \
  go run ./cmd/archive-rederive-job
```

`revert_archive_rederivation.sql` puts the `before` values back, with the job switched off. It reverts a
revision only as a whole, and only while the revision is still active and every value the job changed
still holds what the job wrote; it lists the revisions it skipped. It reopens the review rows the job
resolved for them (an entry's row only when its lists are put back) and deletes their log rows, so
the job would derive them again on its next run.
Beyond that, RDS point-in-time restore covers 7 days.

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/revert_archive_rederivation.sql
```

Prod run, after the merge with the job off:

1. Dev: switch the job on, let one run finish, check the outcomes.
2. Prod: one dry run of 1000 (`dryRun: true`, `maxEntriesPerRun: 1000`), then real runs of 5000 per
   hour off-peak (22:00 to 05:00 UTC), about 15 runs for the ~73,000 archive entries. Watch the API
   p95 per minute and RDS; switch the job off if p95 doubles.
3. After the last run: the outcome counts, and the open review rows for the researchers.
4. Once `entry_revision_malformed_composition_lists` is empty in `schema_inventory.sql`, a follow-up
   migration validates the composition check:
   `ALTER TABLE entry_revisions VALIDATE CONSTRAINT entry_revisions_composition_lists_chk;`
   Prod's run finished on 2026-11-03 (73,074 derived, none failed), the inventory check was empty,
   and migration `V20261103230000` validates the check.
