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
`metric_missing_software` is always reported as `n/a until Phase D` (no provenance column yet).
Records with missing information (omitted `model_type`, NULL `sha256`) are not violations and stay public.

Records the migrations could not resolve themselves are in `schema_migration_review`
(`object_type, object_id, issue, details`): PDB-backed entries whose ligand/cofactor lists wait for
the archive re-derivation (C2), entries merged from models, mutation texts (strings or lists) holding a token that is not a plain substitution, and
duplicate metric keys. Original model lists are kept in `model_revisions.metadata` and copied there.

Validate a constraint (the NOT VALID ones that ship with the C1 migrations: `run_artifacts_direction_chk`,
`run_artifacts_position_chk`, `entry_revisions_composition_lists_chk`) once its violations are repaired:
`ALTER TABLE <table> VALIDATE CONSTRAINT <name>;` (names are listed by
`select conname from pg_constraint where contype = 'c' and not convalidated`).
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

## Constraints to ship with `validation.mode=enforce`

These CHECK constraints are deliberately **not** in the C1 migrations. Adding them `NOT VALID` skips
legacy rows, but an UPDATE of an existing violating row (the app archives the previous
active revision with an UPDATE whenever a new revision activates) is checked too, so a legacy value
such as `model_type = 'Foo'` would block that model from ever getting a new revision, even in enforce
mode. And in `log_only` mode they would turn requests the Go validators merely log into a 500 from the
database. Ship them in the rollout that switches validation to `enforce`, after (a) the CLI release,
(b) the log-only soak shows no violations, and (c) each inventory check below returns **zero rows**
(`psql -v ON_ERROR_STOP=1 -At -c` on the single SELECT, filtered by `check_name`):

| Constraint | Inventory check that must be empty first |
|---|---|
| `metrics_key_known_chk` | `metric_unknown_key` |
| `model_revisions_model_type_chk` | `model_invalid_type` |
| `model_revisions_purpose_chk` | `model_invalid_purpose` |
| `artifacts_sha256_format_chk` | `artifact_malformed_sha256` |
| `artifacts_size_bytes_chk` | `artifact_negative_size` |
| `polymer_entities_uniprot_mappings_chk` | `polymer_entity_uniprot_mapping_invalid` |

The UniProt check can trip on writes made today: the data sync stores SIFTS mappings without a release
when the release lookup fails (`lib/archive/polymer.go`). Make the sync skip or retry such mappings
before shipping it.

Ship each table in its own migration, one lock at a time (these take ACCESS EXCLUSIVE for milliseconds
and scan nothing; `lock_timeout` makes the migration fail cleanly instead of queueing behind a long
transaction, rerun it). Validate afterwards (`VALIDATE CONSTRAINT` only takes SHARE UPDATE EXCLUSIVE).

```sql
-- V..._metrics_key_check.sql
SET LOCAL lock_timeout = '5s';
ALTER TABLE metrics
    ADD CONSTRAINT metrics_key_known_chk CHECK (key IN (
        'r_work', 'r_free', 'cc', 'rscc', 'clashscore', 'ramachandran_outliers',
        'side_chain_outliers', 'molprobity_score', 'wilson_b', 'rmsf')) NOT VALID;

-- V..._model_revisions_checks.sql
SET LOCAL lock_timeout = '5s';
ALTER TABLE model_revisions
    ADD CONSTRAINT model_revisions_model_type_chk CHECK (
        metadata ->> 'model_type' IS NULL
        OR metadata ->> 'model_type' IN ('Single Conformer', 'Multiconformer', 'Ensemble', 'Deposited')) NOT VALID,
    ADD CONSTRAINT model_revisions_purpose_chk CHECK (
        metadata ->> 'purpose' IS NULL
        OR metadata ->> 'purpose' IN ('Model Building', 'Refinement', 'Reference')) NOT VALID;

-- V..._artifacts_checks.sql
SET LOCAL lock_timeout = '5s';
ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_sha256_format_chk CHECK (sha256 IS NULL OR sha256 ~ '^[0-9a-f]{64}$') NOT VALID,
    ADD CONSTRAINT artifacts_size_bytes_chk CHECK (size_bytes IS NULL OR size_bytes >= 0) NOT VALID;

-- V..._polymer_entities_uniprot_check.sql
-- Sources: sifts, struct_ref, alignment. sifts and alignment need unp_release.
SET LOCAL lock_timeout = '5s';
ALTER TABLE polymer_entities
    ADD CONSTRAINT polymer_entities_uniprot_mappings_chk CHECK (NOT jsonb_path_exists(
        metadata,
        '$.uniprot_mappings[*] ? (
            !exists(@.accession) || @.accession.type() != "string"
            || !exists(@.source) || @.source.type() != "string"
            || !(@.source like_regex "^(sifts|struct_ref|alignment)$")
            || ((@.source == "sifts" || @.source == "alignment")
                && (!exists(@.unp_release) || @.unp_release.type() != "string" || @.unp_release == "")))'
    )) NOT VALID;

-- after the data is clean, one statement per transaction:
ALTER TABLE metrics VALIDATE CONSTRAINT metrics_key_known_chk;
ALTER TABLE model_revisions VALIDATE CONSTRAINT model_revisions_model_type_chk;
ALTER TABLE model_revisions VALIDATE CONSTRAINT model_revisions_purpose_chk;
ALTER TABLE artifacts VALIDATE CONSTRAINT artifacts_sha256_format_chk;
ALTER TABLE artifacts VALIDATE CONSTRAINT artifacts_size_bytes_chk;
ALTER TABLE polymer_entities VALIDATE CONSTRAINT polymer_entities_uniprot_mappings_chk;
```

Also map a `23514` (check violation) to a 4xx in `writeMutationError` in the same change, so a client
that bypasses validation gets an error it can act on instead of a 500.
