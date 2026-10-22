# mmCIF extraction

One parser, `lib/cif`, reads structure files for the CLI upload and for
`POST /v1/artifacts/{id}/extract`. The website preview
(`website/src/lib/parse/structure.ts`) follows the same ligand rule as far as a
browser can without the cofactor list.

## What it extracts

| Value | Rule |
| --- | --- |
| `atom_count` | Heavy atoms of the first model; hydrogens, deuterium and water are not counted. |
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

## The ligand rule

Every non-polymer compound (`_pdbx_entity_nonpoly` and `HETATM` rows) is sorted once:

1. **Solvent, buffer, cryoprotectant** (water, glycerol, sulfate, ...): dropped. Nothing else is ever dropped.
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

**Researchers must confirm the solvent list in particular.** It is a science decision, not an engineering one. It classes citrate (CIT), sulfate (SO4), phosphate (PO4), chloride, bromide and iodide as solvents, and the parser drops them. Those compounds are often real ligands, so a wrong entry silently removes a ligand from the record. Engineers do not change this list without researcher sign-off.

## CLI upload

The CLI uploads `ligands` and `cofactors` for coordinate files. It reads the first 4 MiB of the file; when the file is longer, the truncation-aware parser is used, so atom counts, residue counts and altloc fraction are left out instead of being undercounted. As with `ligands`, an empty list is not uploaded: a missing `cofactors` field means none were found (or the coordinates were cut off), while `POST /v1/artifacts/{id}/extract` reports `[]` when the coordinates were read in full and held none.

## Extract endpoint limits

`POST /v1/artifacts/{id}/extract` reads only the first **4 MiB** of the uploaded
object with a ranged S3 GET. Archive files list entities, connections and
mutations before the coordinates, so those are read in full. If the coordinates
run past the limit, atom counts, residue counts and altloc fraction are left out
with a warning. The object must sit in the artifact's own upload folder, and the
caller must be the artifact's creator or a reviewer.
