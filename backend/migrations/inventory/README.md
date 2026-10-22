# Schema inventory (read-only)

`schema_inventory.sql` is one `SELECT` that lists every record breaking a rule added by the
C1 schema migrations. It never writes and is not part of Flyway (it lives outside `db/`).
`internal/db/schema_migrations_test.go` runs it in a read-only transaction against fixture data.

```sh
# repair list: one row per violating record
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/schema_inventory.sql

# counts per check (checks with no violations do not appear)
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -At -c \
  "select check_name, count(*) from ($(grep -v '^--' backend/migrations/inventory/schema_inventory.sql | sed 's/;$//' )) q group by 1 order by 1"
```

Columns: `check_name`, `object_type`, `object_id`, `details` (jsonb), `check_count`.
`metric_missing_software` is always reported as `n/a until Phase D` (no provenance column yet).
Records with missing information (omitted `model_type`, NULL `sha256`) are not violations and stay public.

Records the migrations could not resolve themselves are in `schema_migration_review`
(`object_type, object_id, issue, details`): PDB-backed entries whose ligand/cofactor lists wait for
the archive re-derivation (C2), entries merged from models, mutation strings needing review, and
duplicate metric keys. Original model lists are kept in `model_revisions.metadata` and copied there.

Validate a constraint once its violations are repaired:
`ALTER TABLE <table> VALIDATE CONSTRAINT <name>;` (names are listed by
`select conname from pg_constraint where contype = 'c' and not convalidated`).
To bring quarantined duplicate metric rows under the unique index after repair:
`update model_revision_metrics l set metric_key = m.key from metrics m where m.id = l.metric_id and l.metric_key is null;`
