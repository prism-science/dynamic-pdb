# mmCIF extraction

One parser, `lib/cif`, reads structure files for the CLI upload and for
`POST /v1/artifacts/{id}/extract`. The website preview
(`website/src/lib/parse/structure.ts`) applies the same ligand rule with the same
reference lists (generated from the JSON files, see "Website preview" below).

## What it extracts

| Value | Rule |
| --- | --- |
| `atom_count` | Heavy atoms of the first model (for PDB files, up to the first `ENDMDL` or second `MODEL`); hydrogens, deuterium and water are not counted. |
| `modeled_residues` | Distinct residues (chain, number, insertion code) of `ATOM` records. |
| `unique_protein_chains` | Distinct chains of `ATOM` records. |
| `altloc_fraction` | Residues with at least two alternate positions that each have occupancy >= 0.1, divided by modeled residues. Residues are counted, not atoms. |
| `ligands`, `cofactors` | The ligand rule below. |
| `mutations` | See below. |
| connections | Every `_struct_conn` row, for interaction-set storage. |

The parser handles `;` text blocks, loops whose rows wrap across lines, single-
and double-quoted values (a quote closes only when followed by whitespace, so
`"GUANOSINE-5'-DIPHOSPHATE"` and `'a b'c d'` read correctly) and comments. Only the
first data block is read.

## How a file is read (memory)

`cif.ExtractText` reads a file in one pass: tokens are produced one at a time,
the `_atom_site` loop is reduced to counts as it streams past (its rows are
never stored), `_struct_conn` is reduced to the metal-coordination partners (no
object per row; `Result.Connections` is empty on this path) and the rows of
categories the rules do not use are skipped. A 4 MiB head allocates about 4 MB
this way. `cif.Parse` still builds the full document, for code that needs every
table or the connections.

### Memory bounds for hostile files

What a parse keeps must not depend on what the file contains, because any user
who can create an artifact can point `/extract` at a file of their choosing.
`lib/cif/limits.go` caps everything that grows with the input; when a cap is hit
the rest of that item is ignored, the result carries a `parser limit: ...`
warning, and the parse still succeeds:

| What | Cap | Why that number |
| --- | --- | --- |
| rows / values kept per table | 50,000 rows, 1,000,000 values | a real `_struct_conn` row is at least ~150 bytes, so a 4 MiB head holds under 30,000; the fixtures have under 30 |
| columns in a loop | 1,000 | real categories have under 130; a wider loop is skipped |
| non-looped items | 20,000 | real files have under 700 |
| distinct residues / chains / hetero instances in `_atom_site` or PDB | 100,000 / 20,000 / 100,000 | a 4 MiB head holds under 60,000 atoms |
| alternate-location letters per residue | 16 | they are single letters |
| distinct metalc partner pairs | 50,000 | |
| distinct compounds per list | 2,000 | real entries have well under 200 |
| REMARK fields (PDB) | 5,000 | |
| one line in the streaming scalar scan | 8 MiB | a longer line is an error, not a buffer |

If `_atom_site` runs past a cap, atom counts, residue counts and altloc fraction
are left out with a warning (as for a cut-off read) instead of being wrong. A
head that ends before `_atom_site` begins is reported as cut off, not as "no
atom_site records found".

Measured on hostile four mebibyte files (`lib/cif/hostile_test.go`,
`backend/internal/httpapi/artifact_extraction_limits_test.go`): at worst about 40 MiB
reachable after parsing and about 100 MiB allocated in total per request. Before
the caps, a file of `a a a ...` under `_struct_conn.id` kept about 930 MiB live.
The CLI reads the same 4 MiB head through the same functions, so the same bounds
apply there.

`cif.ScalarValue` / `cif.ScalarValueFromReader` look up one non-looped item (for
example `_refine.ls_R_factor_R_free`) by scanning the stream through a fixed
buffer: they never build a document, so the cost does not grow with the file.

## PDB-format files

Only the first model is counted: records after the first `ENDMDL`, or after a
second `MODEL` record, are ignored. Compounds are also read from `HET` records,
which come before the coordinates. When only the head of a file is read, a
partial last line is dropped, and unless an `ENDMDL`, `CONECT`, `MASTER` or `END`
record shows that the coordinates ended, `atom_count`, `modeled_residues`,
`unique_protein_chains` and `altloc_fraction` are left out (with a warning),
exactly as for mmCIF. PDB files have no `metalc` rows, so every metal is a
cofactor flagged for review. The number of atoms of a `HET` compound comes from
its `HET` record when that has one; a compound whose atoms were cut off by the
read limit is not called single-atom, it is flagged with the reason "atom count
unknown" (the same applies to mmCIF).

## The ligand rule

Every non-polymer compound (`_pdbx_entity_nonpoly` and `HETATM` rows) is sorted once:

1. **Water** (`HOH`, `DOD`, `WAT`, `H2O`): always dropped. **Other solvents, buffers and cryoprotectants** (glycerol, sulfate, ...): dropped only once both reference lists are approved (see below). Until then they are kept as ligands flagged **needs review**. Nothing else is ever dropped.
2. **Metal ion**, decided **per instance** (one placed copy, identified by chain and residue number): an instance is kept as a *cofactor* when a `_struct_conn` row with `conn_type_id = metalc` joins that instance to a protein residue or a ligand (coordination to water does not count).
   - If the file has **no `metalc` rows at all**, its metals are kept as cofactors flagged **needs review**.
   - If the file has `metalc` rows but none involves a given metal instance, that instance is listed as a *ligand* flagged **needs review**. It is never dropped and never listed unflagged. A compound with a linked and an unlinked instance therefore appears in both lists, the unlinked one flagged.
3. **Listed cofactor** (heme, NAD, FAD, ...): kept as a cofactor.
4. Anything else: a ligand. A single-atom instance of an unlisted compound (a metal missing from `metal_ions`, such as Pd or Ru) is a ligand flagged **needs review**.

There is no separate metals list; kept metals are in `cofactors`.

## Mutations

Each entity keeps its original `_entity.pdbx_mutation` text. Mutations come from
`_struct_ref_seq_dif` rows with `details = engineered mutation` and
`pdbx_seq_db_name = UNP`, written in UniProt numbering (`pdbx_seq_db_seq_num`) as
`C51S` (reference residue, position, modeled residue), with the UniProt accession
stored next to the list. Rows that cannot be mapped (another database, no
accession, nonstandard residue, no entity) are never guessed: they are listed as
`unmapped` and the entity is flagged **needs review**, as is an entity whose
mutation text has no matching rows.

## Reference lists (need researcher approval)

Two versioned data sets, each with its own `version` and `approved` flag:

- `lib/cif/cofactors.json`: CCD IDs for cofactors and metal ions.
- `lib/cif/solvents.json` (`solvent-buffer-cryoprotectant-list`): CCD IDs dropped as water, solvents, buffers, cryoprotectants and crystallization salts.

Both are drafts. **Researchers must approve them once**: review the lists, then set `"approved": true` and bump `version`. Every extraction reports a `reference_version` naming both file versions (`cofactors:<v>+solvents:<v>`), so values can be re-derived after a change.

**What the code does while the lists are drafts.** `Reference.Approved` is true only when *both* files say `"approved": true`. When it is false:

- only water is dropped; every other compound on the draft solvent list (sulfate, phosphate, citrate, chloride, glycerol, ...) is kept in `ligands` and flagged `needs_review` with the reason "compound is on the draft solvent list ...";
- every extraction carries the warning `cif.DraftWarning` ("reference lists are draft and not yet approved by researchers ..."): the extract endpoint returns it in `warnings`, the CLI logs it once per structure file;
- the cofactor and metal list is applied as before (it only decides what goes into the new `cofactors` field).

Once both flags are true the full lists apply and the warning disappears. The website preview parser reads the same lists and the same gate from `website/src/lib/parse/cifReference.gen.ts`, which `npm run gen:cif-reference` generates from the two JSON files: flipping the flags in the JSON and regenerating is the whole change. CI regenerates the file and fails on a diff, and a website test fails when it is stale. Tests use injectable reference data (`parseReference`), so approving the lists does not need test edits beyond the expectations that follow the JSON files.

**Researchers must confirm the solvent list in particular.** It is a science decision, not an engineering one. It classes citrate (CIT), sulfate (SO4), phosphate (PO4), chloride, bromide and iodide as solvents. Those compounds are often real ligands, so a wrong entry silently removes a ligand from the record. Engineers do not change this list without researcher sign-off.

## Website preview

`website/src/lib/parse/structure.ts` implements the same rule (per instance,
organic cofactors, metal links, single-atom flag, draft gate, first model only,
altloc by occupancy) and uses the generated reference lists. The Go parser's
answers for the fixtures and for three small files in `lib/cif/testdata/parity/`
are committed as `lib/cif/testdata/parity-golden.json` (refresh with
`UPDATE_PARITY_GOLDEN=1 go test ./lib/cif -run parity_golden`); `website/test/cif-parity.test.cjs`
checks that the preview gives the same ligands, cofactors, review flags (with
reasons) and counts. The preview sends the flags as `extensions.compound_review`
on the model metadata, like the CLI.

## CLI upload

The CLI uploads `ligands` and `cofactors` for coordinate files. It reads the first 4 MiB of the file and parses it **once per artifact**: the parsed fields are cached on the uploader, so the ten or so manifest fields of one artifact cost one parse. When the file is longer, the truncation-aware parser is used, so atom counts, residue counts and altloc fraction are left out instead of being undercounted (for PDB files too). As with `ligands`, an empty list is not uploaded: a missing `cofactors` field means none were found (or the coordinates were cut off), while `POST /v1/artifacts/{id}/extract` reports `[]` when the coordinates were read in full and held none.

Scalar fields read from a data-folder file (for example R-free from `models/5amf_model.cif`) are streamed with `cif.ScalarValueFromReader`; the file is never loaded whole.

**Review flags.** A compound the parser flags (a metal in a PDB-format file, a compound on the draft solvent list, a metal with no `metalc` link) is uploaded in the model's `extensions` object, which the backend declares as open data on `ModelMetadata`:

```json
"extensions": {"compound_review": [
  {"list": "cofactors", "id": "ZN", "reason": "metal ion kept for review: the file has no metalc connection rows"}
]}
```

`ligands` and `cofactors` stay plain lists of IDs, so the declared fields do not change. The CLI also logs a warning per flagged compound while it reads the file (`compound needs review`). The manifest key is `extensions.compound_review`; any manifest model-metadata key starting with `extensions.` is uploaded under `extensions`. These keys are merged into an `extensions` object the manifest sets itself; a key the manifest sets is never replaced.

**Manifests saved before cofactors existed.** Such a manifest has `ligands` read from the coordinates but no `cofactors` entry; with the new extractor HEM and metal ions would have left `ligands` and gone nowhere. When the upload reads `ligands` from the coordinates and the manifest has no `cofactors` entry, it reads `cofactors` and `extensions.compound_review` from the same coordinates as well, and logs one warning per run saying so. Regenerate the manifest (`dynamic-pdb upload manifest init <data-folder>`) to make it permanent.

A manifest field that names a PDB or mmCIF rule on an artifact of another format (for example `.mmtf`), or a PDB field the file extractor does not read, is an error, not a silent "not found". The draft-list warning is logged once per run, not once per artifact.

## Extract endpoint limits

`POST /v1/artifacts/{id}/extract` reads only the first **4 MiB** of the uploaded
object with a ranged S3 GET. Archive files list entities, connections and
mutations before the coordinates, so those are read in full. If the coordinates
run past the limit, atom counts, residue counts and altloc fraction are left out
with a warning. The object must sit in the artifact's own upload folder, and the
caller must be the artifact's creator or a reviewer.

### Concurrency and rate limits

A request holds up to 4 MiB of file plus the parser state: about 5 MB allocated
for a normal file and, bounded by the caps above, at most about 100 MB allocated
(under 50 MB reachable) for a hostile one, so two in flight stay far inside a
512 MiB pod. The cost scales with a user-supplied file and is still bounded
explicitly. Both limits are per server instance and in memory, like the
global per-IP limit (`GlobalRateLimitMiddleware`, `httprate`):

- **At most 2 extractions at once** per instance. A third concurrent request gets `503 SERVICE_UNAVAILABLE` with `Retry-After: 5`. The slot is taken after authorization and validation (so 401/403/404/422 never use one) and released as soon as the response is built, before it is written to the client.
- **10 extraction requests per minute per user** per instance (`httprate` keyed by user ID). Past that: `429 TOO_MANY_REQUESTS` with `Retry-After` (seconds). The count includes requests that end in 403/404/422.

With N pods the effective per-user budget is up to 10 x N per minute, because the counter is not shared. Change the defaults with `Server.WithExtractionLimits`; they are constants in `backend/internal/httpapi/artifact_extraction_limits.go`.

The endpoint warns (`warnings`) while the reference lists are drafts; see above.
