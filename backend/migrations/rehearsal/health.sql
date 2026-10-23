-- One line per finding after the migrations ran: kind|detail. No output means nothing to report.
--
-- invalid_index         an index left behind by a failed CREATE INDEX CONCURRENTLY (a failure)
-- failed_migration      a Flyway history row marked as failed (a failure)
-- not_valid_constraint  a constraint added NOT VALID; often intended, listed so the report shows it
SELECT 'invalid_index', format('%s on %s', i.indexrelid::regclass, i.indrelid::regclass)
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE NOT i.indisvalid
  AND n.nspname = 'public'
UNION ALL
SELECT 'failed_migration', format('%s %s', coalesce(version, '(repeatable)'), description)
FROM flyway_schema_history
WHERE NOT success
UNION ALL
SELECT 'not_valid_constraint', format('%s on %s', con.conname, con.conrelid::regclass)
FROM pg_constraint con
JOIN pg_namespace n ON n.oid = con.connamespace
WHERE NOT con.convalidated
  AND n.nspname = 'public'
ORDER BY 1, 2;
