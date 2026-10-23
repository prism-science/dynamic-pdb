-- Polymer entity mutations were stored as comma-joined text or, since the sync began splitting
-- it, as a list of tokens. Convert both to a list of plain substitutions (e.g. C54T) and keep
-- the original text and a parse status, so ambiguous tokens such as 'ENGINEERED MUTATION',
-- 'DELETION' or 'A(-3)G' are flagged and never guessed. UniProt renumbering is not done here.
--
-- A string is the original text. A list of strings is turned into text first, its tokens joined
-- with ', ' in their original order, so no token (substitution or not) is lost; then the same
-- rule applies to both. Only plain substitutions stay in mutations, the text goes to
-- mutations_raw.
--
-- mutations_status: 'parsed'       every token is a substitution
--                   'needs_review' at least one token is not; only the plain ones are listed
--                   'none'         the text has no token
-- Entities without a mutations key (unknown) or with a JSON null are left alone. Only rows
-- whose mutations value is a string or an array are rewritten (row locks on those rows
-- only). A list with an element that is not a string is not converted: it gets status
-- 'needs_review' and is flagged in schema_migration_review with the original list. Any other
-- value (number, boolean, object) is not converted and not dropped: it is flagged
-- 'mutations_unsupported_type' in schema_migration_review with the original value.
-- The token rule here (split on , or ;, trim whitespace, plain substitution pattern) is the
-- same as models.ParseMutations in the Go code, so the data sync derives identical values and
-- the first sync after this migration finds nothing to change (models.SameMutationText treats
-- raw texts with the same tokens as equal).
--
-- One transaction over every polymer entity whose mutations is a string or array: it holds
-- row locks on those rows until it commits, so run it off-peak.

-- The whitespace class is exactly what Go's strings.TrimSpace (unicode.IsSpace) trims:
-- U+0009-U+000D, space, U+0085, U+00A0, U+1680, U+2000-U+200A, U+2028, U+2029, U+202F, U+205F,
-- U+3000. btrim only trims the characters it is given, so it would miss NBSP and friends.
CREATE TEMPORARY TABLE polymer_entity_mutation_tokens AS
WITH mutation_texts AS (
    SELECT polymer_entities.id, polymer_entities.metadata ->> 'mutations' AS raw
    FROM polymer_entities
    WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'string'
    UNION ALL
    SELECT polymer_entities.id,
           coalesce((SELECT string_agg(element.value #>> '{}', ', ' ORDER BY element.position)
                     FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations')
                          WITH ORDINALITY AS element(value, position)), '')
    FROM polymer_entities
    WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
      AND NOT (polymer_entities.metadata ? 'mutations_status')
      AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
                      WHERE jsonb_typeof(element) <> 'string')
)
SELECT
    mutation_texts.id AS polymer_entity_id,
    mutation_texts.raw,
    coalesce(jsonb_agg(tokens.trimmed ORDER BY tokens.position)
             FILTER (WHERE tokens.trimmed ~ '^[A-Z][1-9][0-9]*[A-Z]$'), '[]'::jsonb) AS accepted,
    count(*) FILTER (WHERE tokens.trimmed <> '') AS token_count,
    count(*) FILTER (WHERE tokens.trimmed <> '' AND tokens.trimmed !~ '^[A-Z][1-9][0-9]*[A-Z]$') AS rejected_count
FROM mutation_texts
CROSS JOIN LATERAL (
    SELECT regexp_replace(split.token,
                          '^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$',
                          '', 'g') AS trimmed,
           split.position
    FROM regexp_split_to_table(mutation_texts.raw, '[,;]')
         WITH ORDINALITY AS split(token, position)
) tokens
GROUP BY mutation_texts.id, mutation_texts.raw;

-- An empty text yields one empty token, so it is present above with token_count = 0.
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

-- Lists with an element that is not a string cannot be read by the Go decoder and have no text
-- to derive: classify only.
INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'polymer_entity', polymer_entities.id::text, 'mutations_need_review',
       jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations')
FROM polymer_entities
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
  AND NOT (polymer_entities.metadata ? 'mutations_status')
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
              WHERE jsonb_typeof(element) <> 'string')
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

UPDATE polymer_entities
SET metadata = polymer_entities.metadata || jsonb_build_object('mutations_status', 'needs_review')
WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
  AND NOT (polymer_entities.metadata ? 'mutations_status')
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
              WHERE jsonb_typeof(element) <> 'string');
