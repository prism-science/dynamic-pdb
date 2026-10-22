# Dynamic PDB Schema v0.2.0

This document defines the records Dynamic PDB accepts. The backend enforces it on every write:
the API (`POST /v1/entries`, `POST /v1/entries/{entry_id}/models`, and the entry and model revision
endpoints) and the RCSB data sync jobs validate a record before anything is written to the database,
so a rejected record leaves no rows behind.

The Go source of truth is `backend/internal/models` (vocabularies) and `backend/internal/validation`
(rules). `backend/api/openapi.yaml` describes the same rules for API clients.

## Vocabularies

Values are case-sensitive and must match exactly.

| Field | Allowed values |
| --- | --- |
| Model `metadata.purpose` | `Model Building`, `Refinement`, `Reference` |
| Model `metadata.model_type` | `Single Conformer`, `Multiconformer`, `Ensemble`, `Deposited` |
| Model `metadata.external_refs` key (model source) | `pdb`, `ext` |
| Metric `key` | `r_work`, `r_free`, `cc`, `rscc`, `clashscore`, `ramachandran_outliers`, `side_chain_outliers`, `molprobity_score`, `wilson_b`, `rmsf` |
| Artifact `level` | `L0`, `L1`, `L2`, `L3` |
| Artifact `type` | `model`, `structure_factors`, `fasta`, `other` |
| Run artifact `direction` | `input`, `output` |
| UniProt mapping `source` | `sifts`, `struct_ref`, `alignment` |
| Revision `state` | `pending`, `in_review`, `active`, `rejected`, `archived` |
| Entry and model `state` | `new`, `active`, `deleted` |

Entry `method` and artifact `format` are deliberately open vocabularies.

## Rules

### Entry

- `title` is required and must not be blank.
- `method` is required and must not be blank. It is free text (for example `X-ray crystallography`,
  `CryoEM`, `SOLUTION NMR`).
- `resolution`, when set, must be positive.
- An entry revision change validates only the fields it contains; clearing `title` or `method` is rejected.

### Model

- `title` is required and must not be blank.
- `metadata.purpose`, `metadata.model_type` and the keys of `metadata.external_refs` must be in their
  vocabularies when set.

### Metric

- `key` must be in the metric vocabulary.
- `value` is required. An omitted value is rejected; it is never stored as `0`.
- A model revision has at most one metric per key.

### Artifact

- `id`, `name`, `level` and `type` are required; `level` and `type` must be in their vocabularies.
- `format`, when set, must be lowercase. Any format name is accepted.
- `size_bytes`, when set, must not be negative.
- `sha256`, when set, must be 64 lowercase hexadecimal characters. Only the shape is checked; the
  registry does not yet recompute checksums.
- Top-level `metadata` keys starting with `__` are reserved for the registry and rejected.

### Run

- `id` and `name` are required.
- Every run artifact needs an `artifact_id` and a `direction` from the vocabulary.
- A run artifact must be one of the artifacts of the same model revision. This is checked whenever
  the request carries the model revision's artifacts (model creation, or a model revision change that
  replaces `artifacts`).

### Polymer entity UniProt mapping

- `accession` and `source` are required; `source` must be in the vocabulary.
- `unp_release` is required for `sifts` and `alignment` mappings. The RCSB sync maps RCSB provenance
  `SIFTS` to `sifts`, `PDB` to `struct_ref` and `RCSB` to `alignment`, and fails on any other provenance.

## Errors

A request that violates the schema gets `422 Unprocessable Entity` listing every violation at once.
Each error object carries a `code`, a `detail` and `source.pointer`, a JSON Pointer (RFC 6901) to the
offending field of the request body:

```json
{
  "errors": [
    {
      "status": "422",
      "code": "NOT_IN_SCHEMA",
      "title": "Schema validation failed",
      "detail": "unknown metric key: r_factor",
      "source": { "pointer": "/model/metrics/1/key" }
    }
  ]
}
```

| Code | Meaning |
| --- | --- |
| `REQUIRED` | A required field is missing, null or blank. |
| `NOT_IN_SCHEMA` | A value is outside its vocabulary. |
| `OUT_OF_RANGE` | A number is outside its allowed range. |
| `INVALID_FORMAT` | A string does not have the required shape. |
| `DUPLICATE` | A value that must be unique on the record repeats. |
| `RESERVED_KEY` | A metadata key uses the reserved `__` prefix. |
| `UNKNOWN_REFERENCE` | A reference points outside the record it must belong to. |

Malformed JSON and other request errors keep their existing status codes.

## Rollout

`validation.mode` in the backend config (env `DYNAMIC_PDB_VALIDATION_MODE`) selects the behaviour:

- `enforce` (default) rejects violating writes as described above.
- `log_only` lets violating writes through and logs every violation with its pointer, request id and
  path (or entry and model ids for the sync jobs). It exists only for rolling the schema out.
