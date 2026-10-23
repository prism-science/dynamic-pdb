-- One line per table in the public schema: table|rows|checksum.
--
-- rehearsal.sh runs this before and after the migrations and compares the two.
-- The checksum is a sum of per-row hashes, so it does not depend on row order and
-- needs no memory beyond one row at a time. It changes when any value in the table
-- changes. flyway_schema_history is left out because migrating changes it on purpose.
SELECT format(
           $query$SELECT %L, count(*), coalesce(sum(('x' || left(md5(t::text), 16))::bit(64)::bigint), 0) FROM %I.%I t$query$,
           c.relname, n.nspname, c.relname)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relkind IN ('r', 'p')
  AND NOT c.relispartition
  AND c.relname <> 'flyway_schema_history'
ORDER BY c.relname
\gexec
