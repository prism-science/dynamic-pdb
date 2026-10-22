-- Polymer entity mutations were stored as comma-joined text. Convert them to a list of
-- plain substitutions (e.g. C54T) and keep the original text and a parse status, so
-- ambiguous strings such as 'ENGINEERED MUTATION', 'DELETION' or 'A(-3)G' are flagged and
-- never guessed. UniProt renumbering is not done here.
--
-- mutations_status: 'parsed'       every token is a plain substitution
--                   'needs_review' at least one token is not; only the plain ones are listed
--                   'none'         the text is empty
-- Entities without a mutations key (unknown) or with a JSON null are left alone. Only rows
-- whose mutations value is a string or an array are rewritten (row locks on those rows
-- only). Any other value (number, boolean, object) is not converted and not dropped: it is
-- flagged 'mutations_unsupported_type' in schema_migration_review with the original value.
-- The token rule here (split on , or ;, trim whitespace, plain substitution pattern) is the
-- same as models.ParseMutations in the Go code, so the data sync derives identical values.

CREATE TEMPORARY TABLE polymer_entity_mutation_tokens AS
SELECT
    polymer_entities.id AS polymer_entity_id,
    polymer_entities.metadata ->> 'mutations' AS raw,
    coalesce(jsonb_agg(btrim(tokens.token, E' \t\r\n\f\v') ORDER BY tokens.position)
             FILTER (WHERE btrim(tokens.token, E' \t\r\n\f\v') ~ '^[A-Z][1-9][0-9]*[A-Z]$'), '[]'::jsonb) AS accepted,
    count(*) FILTER (WHERE btrim(tokens.token, E' \t\r\n\f\v') <> '') AS token_count,
    count(*) FILTER (WHERE btrim(tokens.token, E' \t\r\n\f\v') <> ''
                       AND btrim(tokens.token, E' \t\r\n\f\v') !~ '^[A-Z][1-9][0-9]*[A-Z]$') AS rejected_count
FROM polymer_entities
CROSS JOIN LATERAL regexp_split_to_table(polymer_entities.metadata ->> 'mutations', '[,;]')
     WITH ORDINALITY AS tokens(token, position)
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'string'
GROUP BY polymer_entities.id;

-- An empty string yields one empty token, so it is present above with token_count = 0.
INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'polymer_entity', polymer_entity_id::text, 'mutations_need_review',
       jsonb_build_object('mutations_raw', raw, 'mutations', accepted)
FROM polymer_entity_mutation_tokens
WHERE rejected_count > 0
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

UPDATE polymer_entities
SET metadata = polymer_entities.metadata || jsonb_build_object(
    'mutations', polymer_entity_mutation_tokens.accepted,
    'mutations_raw', polymer_entity_mutation_tokens.raw,
    'mutations_status', CASE
        WHEN polymer_entity_mutation_tokens.token_count = 0 THEN 'none'
        WHEN polymer_entity_mutation_tokens.rejected_count > 0 THEN 'needs_review'
        ELSE 'parsed' END)
FROM polymer_entity_mutation_tokens
WHERE polymer_entities.id = polymer_entity_mutation_tokens.polymer_entity_id;

DROP TABLE polymer_entity_mutation_tokens;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'polymer_entity', polymer_entities.id::text, 'mutations_unsupported_type',
       jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations',
                          'json_type', jsonb_typeof(polymer_entities.metadata -> 'mutations'))
FROM polymer_entities
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') IN ('number', 'boolean', 'object')
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

-- Entities that already hold a list: classify only, nothing to split.
INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'polymer_entity', polymer_entities.id::text, 'mutations_need_review',
       jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations')
FROM polymer_entities
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
              WHERE jsonb_typeof(element) <> 'string' OR element #>> '{}' !~ '^[A-Z][1-9][0-9]*[A-Z]$')
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

UPDATE polymer_entities
SET metadata = polymer_entities.metadata || jsonb_build_object('mutations_status', CASE
        WHEN jsonb_array_length(polymer_entities.metadata -> 'mutations') = 0 THEN 'none'
        WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
                     WHERE jsonb_typeof(element) <> 'string' OR element #>> '{}' !~ '^[A-Z][1-9][0-9]*[A-Z]$')
            THEN 'needs_review'
        ELSE 'parsed' END)
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
  AND NOT (polymer_entities.metadata ? 'mutations_status');
