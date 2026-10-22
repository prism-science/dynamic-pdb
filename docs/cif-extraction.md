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
2. **Metal ion**: kept as a *cofactor* when a `_struct_conn` row with `conn_type_id = metalc` joins it to a protein residue or a ligand (coordination to water does not count).
   - If the file has **no `metalc` rows at all**, its metals are kept as cofactors flagged **needs review**.
   - If the file has `metalc` rows but none involves this metal, it stays a *ligand* flagged **needs review**.
3. **Listed cofactor** (heme, NAD, FAD, ...): kept as a cofactor.
4. Anything else: a ligand.

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

## Reference list (needs researcher approval)

`lib/cif/cofactors.json` holds the CCD IDs for cofactors, metal ions and solvents,
with a `version`. **The list is a draft. Researchers must approve it once**: review
the three lists, then set `"approved": true` and bump `version`. Every extraction
reports the `reference_version` it used, so values can be re-derived after a change.

## Extract endpoint limits

`POST /v1/artifacts/{id}/extract` reads only the first **4 MiB** of the uploaded
object with a ranged S3 GET. Archive files list entities, connections and
mutations before the coordinates, so those are read in full. If the coordinates
run past the limit, atom counts, residue counts and altloc fraction are left out
with a warning. The object must sit in the artifact's own upload folder, and the
caller must be the artifact's creator or a reviewer.
