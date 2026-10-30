# mmCIF extraction

One parser, `lib/cif`, reads structure files (mmCIF and PDB format) for the CLI
upload and for `POST /v1/artifacts/{id}/extract`. The website preview
(`website/src/lib/parse/structure.ts` and `ligandRule.ts`) applies the same
ligand rule with the same reference lists (generated from the JSON files, see
"Website preview" below).

## What it extracts

| Value | Rule |
| --- | --- |
| `atom_count` | Heavy atoms of the first model (for PDB files, up to the first `ENDMDL` or second `MODEL`); hydrogens, deuterium and water are not counted. |
| `modeled_residues` | Distinct residues (chain, number, insertion code) of `ATOM` records. |
| `unique_protein_chains` | Distinct chains of `ATOM` records. |
| `altloc_fraction` | Residues with at least two alternate positions that each have occupancy >= 0.1, divided by modeled residues. Residues are counted, not atoms. |
| `ligands`, `cofactors` | The ligand rule below. |
| `compound_sources` | The decision record: the rule that placed each compound, including the ones left out (see "Decision record"). |
| `glycans` | One record per branched entity (a glycan): its sugars and its WURCS descriptors. |
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
| distinct chain and residue name pairs in PDB `SEQRES` (and distinct `MODRES` residues) | 100,000 (100,000) | a real chain uses under 30 residue names |
| distinct compounds per list | 2,000 | real entries have well under 200 |
| REMARK fields (PDB) | 5,000 | |
| one line in the streaming scalar scan, or one line or `;` text field in `cif.ReadModeledCompounds` | 8 MiB | a longer line is an error, not a buffer |
| compound copies listed by `cif.ReadModeledCompounds` | 100,000 | 6VXX, a glycosylated spike trimer, has 63; 5HAU has 1,945; past the cap the list is marked incomplete with a warning |
| category values `cif.ReadModeledCompounds` keeps (`_entity`, `_chem_comp`, single-row scheme categories) | 64 MiB | real files keep under 1 MB; items of other categories are not kept at all |
| one field of a compound copy (CCD ID, chain, residue number, insertion code, entity) | 64 bytes, plain UTF-8 text | a longer field, invalid UTF-8 or a control character (PostgreSQL rejects a NUL) skips the copy with a warning |
| time for one extraction (`cif.TimeBudget`) | 5 s | a 4 MiB head parses in under 0.2 s even when hostile; the budget only stops a slow machine or an input nobody anticipated |

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

### Time bounds for hostile files

Reading is linear in the input. The tokenizer reads a quoted value forward from its
opening quote and stops at the closing quote (a quote followed by whitespace or the
end of the line), so a line holding many quoted tokens is read once, not once per
token. `lib/cif/hostile_time_test.go` runs about 40 hostile shapes (one line of
`'a' ` or `"a" ` tokens, quotes that never close, quotes inside unquoted tokens,
prime-like names, `;` blocks, very long lines, CRLF, many tiny tokens, many
`data_`, `loop_` and tag lines, mixes) through every entry point and requires a 4 MiB
file to finish inside 2 s and to cost less than 8x the time of the same file cut to 1 MiB.

Beyond that, every extraction has a **time budget** of `cif.TimeBudget` (5 s).
`ExtractText`, `MMCIFExtraction` and `PDBExtraction` check the clock every 1,024
tokenizer steps or lines (`...Context` variants take the caller's context instead,
which also covers a client that disconnects). When the budget runs out, reading
stops as at the end of a cut-off file: the result carries the warning `parser limit:
time budget exceeded: the rest of the file was not read` and atom counts, residue
counts and altloc fraction are left out; what was read before that is returned.
`ScalarValueFromReaderContext` returns an error wrapping the context's error. The
non-context `ScalarValueFromReader` has no fixed budget, because it scans a whole
file of any size in linear time; the CLI passes its context.

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
exactly as for mmCIF. The number of atoms of a `HET` compound comes from its
`HET` record when that has one; a compound whose atoms were cut off by the read
limit is not called single-atom, it is flagged with the reason "atom count
unknown" (the same applies to mmCIF).

A PDB-format file has no entity types, so the parser reads its chains instead
(`lib/cif/pdbchains.go`, mirrored in `website/src/lib/parse/pdbChains.ts`):

- **Chain residues.** A `HETATM` residue is part of a polymer chain (and so not a
  compound) when `MODRES` lists it, or when its name is in its chain's `SEQRES`
  (columns 20 to 70, the 13 residue slots of the record)
  and it sits in the chain's polymer block: before the chain's `TER` record, or,
  for a chain without one, within the residue numbers of the chain's `ATOM`
  records. Selenomethionine (MSE) in a chain is therefore not a ligand, and a
  free glutamate after the chain's `TER` record is. A residue named in `SEQRES`
  counts whatever its CCD type (the antibiotic residues of 7LTB are
  "non-polymer" in the CCD).
- **Chains without `SEQRES`** (common in depositor-made files): when the file
  has no `SEQRES` records at all, or the residue's chain has `ATOM` records but
  no `SEQRES`, nothing says which residues the chain holds, so the parser does
  not guess. A residue that could be a chain residue is kept and flagged "the
  file does not say which residues belong to its polymer chains: confirm this
  residue is not part of one": a peptide or nucleotide residue anywhere (MSE in
  the chain, a free glutamate), and any other residue in the chain's polymer
  block that `MODRES` does not list (an ATP numbered inside the protein's
  range). It goes to the list the reference lists choose (rule `list`, or
  `branched` for a sugar), and it is kept even when the lists would leave it
  out. Other residues outside the chains are sorted as usual. A chain with
  neither `SEQRES` nor `ATOM` records (the glycan and ligand chains of an
  archive file such as 7LTB) is not a polymer chain, so this does not apply to
  it.
- **Everything else goes by its CCD type** (`lib/cif/ccd_types.json`): a sugar
  is a glycan (Ligands, rule `branched`); a non-polymer goes by the reference
  lists. A peptide or nucleotide residue outside any chain is a ligand flagged
  "residue outside the polymer chain: confirm it is a ligand" (rule `ccd_type`)
  when the file's `SEQRES` records use its name (a free glutamate next to a
  chain of glutamates); otherwise it goes by the lists like any non-polymer.
  This keeps GDP, which the CCD types as "RNA linking", an ordinary ligand.
- **`LINK` records are the metal links.** A `LINK` that joins a metal ion to
  another residue counts like an mmCIF `metalc` row, and a file with at least one
  such record is treated like an mmCIF file with `metalc` rows. `LINK` records
  between other residues (glycosidic or peptide bonds) do not count. A file
  without metal `LINK` records keeps its metals as cofactors flagged for review.
- **`MODRES`** lists the modified residues, so a covalently bound cofactor (LLP)
  adds its cofactor (PLP), as `_pdbx_struct_mod_residue` does in mmCIF.

`SEQRES`, `MODRES` and `LINK` are header records, so a 4 MiB head always holds
them. A chain whose `TER` record falls after the cut uses the residue-number
rule.

## The ligand rule

Every compound instance (`_pdbx_entity_nonpoly` rows and `HETATM` residues) is
sorted once (`classifyCompounds` in `lib/cif/sorting.go`):

1. **Water and polymer residues are not compounds.** In mmCIF the instance's
   `_entity.type` decides: `water` is dropped, a `polymer` residue (MSE in a
   chain) is skipped. An mmCIF file without `_entity` uses its own
   `_chem_comp.type`, then the CCD table (`ccd_types.json`), as a PDB-format file
   does (see above). Nothing in such a file says which residues its chains
   hold, so it follows the rule for PDB chains without `SEQRES`: a peptide or
   nucleotide residue anywhere, and any residue with a `label_seq_id`, is kept
   and flagged instead of guessed. `_pdbx_struct_mod_residue` still adds a
   covalently bound cofactor (PLP for LLP) but, unlike `MODRES`, does not make
   a residue a chain member. Water and
   polymer residues are never recorded.
2. **The depositor's flag** (`_pdbx_entity_instance_feature`) overrides the
   lists: SUBJECT OF INVESTIGATION keeps the instance as a ligand (even a listed
   cofactor such as biotin, or a buffer), NO FUNCTIONAL ROLE leaves it out, OTHER
   changes nothing. A flag without a location applies to every instance of the
   compound; one with `asym_id` and `auth_seq_num` to that instance.
3. **IHM probes** (`_ihm_ligand_probe` entities of an integrative file) are left
   out.
4. **Glycans** (`branched` entities) and **macrolides** are ligands.
5. **Everything else goes by the reference lists:**
   - **Solvents, buffers and cryoprotectants** (glycerol, ethylene glycol, Tris,
     ...) are left out. **Imidazole** is the exception: kept as a ligand when a
     `metalc` connection binds it to a metal (a His-tag nickel), left out
     otherwise.
   - **Ambiguous compounds** (sulfate, phosphate, citrate, fluoride, chloride,
     bromide, iodide) are kept as ligands flagged "often from the buffer: confirm
     it is part of the sample". Only the depositor's NO FUNCTIONAL ROLE flag
     leaves them out.
   - **Metal ions**, decided per instance (one placed copy, by chain and residue
     number; a PDB-format residue on a blank chain by its number alone; a
     `metalc` row that names no location applies to every instance): a
     cofactor when a `metalc` connection joins the instance to a kept
     partner (a protein residue, a ligand, a cofactor, another metal, or the
     imidazole it keeps). Water does not count as a partner, and neither does a
     compound left out (glycerol). The partner is kept or left out by the same
     steps that place it: an ATP the depositor marks NO FUNCTIONAL ROLE, or an
     integrative model's probe, does not count; a glycerol the depositor marks
     SUBJECT OF INVESTIGATION does. A partner the file declares in
     `_pdbx_entity_nonpoly` but gives no coordinates (they were cut off) is
     placed by that declaration and the depositor's flags; a residue of the
     chain always counts, whatever flag it carries.
     - Joined only to water: a cofactor flagged "metal linked only to water:
       confirm the site".
     - The file has **no `metalc` rows at all**: a cofactor flagged for review.
     - The file has `metalc` rows but none involves this instance: a ligand
       flagged for review. It is never dropped and never listed unflagged. A
       compound with a linked and an unlinked instance therefore appears in both
       lists, the unlinked one flagged.
   - **Listed cofactors** (heme, NAD, FAD, ...): cofactors.
   - **Anything else** is a ligand. A single-atom instance of an unlisted
     compound is flagged when its element (`type_symbol`) is a metal (a metal
     missing from `metal_ions`) or unknown (no element, or X for UNX); a single
     non-metal atom (xenon) is not.

A modified residue that is a covalently bound cofactor (`_pdbx_struct_mod_residue`
in mmCIF, `MODRES` in PDB format) adds that cofactor: LLP (PLP on a lysine) adds
PLP, LYR (retinal on a lysine) adds RET.

An integrative (IHM) file (one with `_ihm_model_list`, or an `ihm_model_id`
column in `_atom_site`) lists the compounds of every model, not only the first;
atom counts stay first-model only.

There is no separate metals list; kept metals are in `cofactors`. A value the
parser does not know in a category it reads (an entity type, a depositor flag, a
glycan descriptor type, a `_chem_comp.type`) never fails the extraction: the
compound is sorted as above and flagged for review, and a warning names the
value. A compound the lists would leave out (glycerol in an entity of an
unknown type) is kept as a flagged ligand instead, since the lists may be wrong
about it; a depositor's NO FUNCTIONAL ROLE or an IHM probe still leaves it out.
A `_chem_comp.type` is read only for a compound whose entity the file does not
type (step 1), so an unknown one is flagged only there.

## Decision record

Every extraction also returns `compound_sources`: per group (`ligands`,
`cofactors`, `excluded`), each CCD ID with the rule that put it there and the
reference version in force (`list_version`, the same string as
`reference_version`). One ID can be in two groups, such as a zinc linked to the
protein (cofactor) and one linked to nothing (ligand), or a glycerol the
depositor flagged (ligand) next to plain ones (excluded).

| Rule | Meaning |
| --- | --- |
| `list` | The reference lists placed it, including "on no list, so a ligand" and the solvents left out. |
| `metalc` | The metal rule placed a metal ion, or the imidazole bound to one. |
| `branched` | A sugar of a branched entity, or a saccharide by CCD type in a file without entity types. |
| `macrolide` | A macrolide entity. |
| `ccd_type` | A peptide or nucleotide residue outside any polymer chain whose name the file's `SEQRES` records use (flagged). |
| `depositor_flag` | The depositor's SUBJECT OF INVESTIGATION or NO FUNCTIONAL ROLE flag. |
| `covalent_map` | A cofactor bound in a modified residue (PLP from LLP). |
| `ihm_probe` | An experimental probe of an integrative model (left out). |
| `depositor` | Only in a stored record: a compound a person added to an entry's list (see below). The parser never gives it. |

When one ID is placed in the same group by two rules (a NAG in a glycan and a
lone NAG), the record keeps the more specific rule, in this order:
`depositor_flag`, `ihm_probe`, `covalent_map`, `branched`, `macrolide`,
`ccd_type`, `metalc`, `list`.

Glycans also get their WURCS descriptors (`glycans` in `Result`,
`glycan_descriptors` on the extract endpoint): one record per branched entity
with `entity_id`, `compound_ids` (from `_pdbx_entity_branch_list`) and the WURCS
rows of `_pdbx_entity_branch_descriptor` (`descriptor`, `type`, `program`,
`program_version`). LINUCS and Glycam rows are not kept; a descriptor type the
parser does not know flags the glycan's compounds.

### Stored on entry revisions

Entry revisions keep the record in `compound_sources` and the glycans in
`glycan_descriptors`, next to the `ligands` and `cofactors` lists in the
metadata. Both are null when the lists were not derived from a file, and entry
revision responses return them read-only. The stored record has one optional
field more than the extract endpoint's: `review_reason`, the reason the parser
flagged the compound (the extract endpoint returns it on the compound in
`ligands` or `cofactors`). It is left out when nothing was flagged.

Archive entries get both from the first 4 MiB of their RCSB mmCIF, read by
`readArchiveFile` (`backend/internal/jobs/archive_file.go`), the same head the
extract endpoint reads:

- The archive re-derivation job (`deploy/README.md`) fills them once, in place,
  for entries whose lists are unknown.
- The entry sync takes them from the file again when it applies a new archive
  version, so they follow the archive.

Both keep the lists, record and glycans as stored when the lists are a person's,
or when the head could not be parsed or ends before the coordinates. Lists are a
person's when `field_sources` marks `/metadata/ligands` or `/metadata/cofactors`
as `depositor`, or when they are set without a record: lists the job or the sync
derived always carry one.

When a person changes a list in a new revision, the server keeps the record in
step with it: a compound they add gets rule `depositor` (with no `list_version`),
one they remove leaves its group, and the others, the excluded group and the
glycans stay as they were. A revision whose lists were never derived keeps a
null record.

## Compounds modeled in the file

`cif.ReadModeledCompounds` (`lib/cif/modeled.go`) lists every copy of every
compound a file models outside its polymer chains, water (`HOH`, `DOD`, `WAT`,
`H2O`) excepted. It is a record of the coordinates, not a judgment: the
reference lists are not applied, so buffers, ions and an integrative model's
probes are listed, and nothing is split into ligands and cofactors. The entry's
Ligands and Cofactors lists carry those judgments. Go only; the website does not
read it.

Each copy has its CCD ID, the chain, residue number and insertion code viewers
show (`_atom_site.auth_*`, the schemes' `pdb_*` items, PDB columns 22 to 27),
the mmCIF label chain and entity, and where it was read:

- **`nonpoly_scheme`, `branch_scheme`.** An mmCIF file's
  `_pdbx_nonpoly_scheme` and `_pdbx_branch_scheme` rows (glycans of two or more
  sugars are only in the branch scheme). The schemes' own `auth_*` items are the
  depositor's original numbering and are not used.
- **`atom_site`.** A `HETATM` residue whose entity is not a polymer (or water),
  when the file has no schemes or they leave it out. A copy the schemes leave out
  adds the warning "N compound copies in the coordinates are missing from the
  scheme categories".
- **`pdb_hetatm`.** The `HETATM` residues of a PDB-format file that `SEQRES`
  and `MODRES` do not make chain residues, as for the ligand rule.

A file that does not say what its chains hold (no `_entity`, or a PDB-format
file or chain without `SEQRES`) gets the ligand rule's treatment: a residue
that could be a chain residue is listed and marked for review, never left out by
a guess. A copy whose entity type, or chemical component type in a file without
`_entity`, is unknown is marked for review too.

Copies are de-duplicated by position across models, so an ensemble lists a
compound that only a later model holds. The chain positions of a PDB-format
file come from its first model.

**Whole file, bounded memory.** Unlike extraction, the read is not limited to
the 4 MiB head: program outputs often have no schemes and write their `HETATM`
rows at the end of `_atom_site`. The stream is read line by line through a
64 KiB buffer (`lib/cif/stream.go`, the same tokens as the text tokenizer; a test
checks them on every fixture), and only the keys of distinct copies are kept, up
to 100,000. On a laptop a 315 MB mmCIF file took 2.3 s and a 315 MB PDB file
0.4 s, both with a 13 MB peak (2026-10-30). There is no fixed time budget: the
caller's context bounds the read, and a done context or a stream that cannot be
read is an error, not a partial list. A list stopped by the copy cap or the 64 MiB
budget on kept values, a loop that ends inside a row, or a copy skipped because a
field is not plain text has `Complete` false and a warning. A review reason that
quotes a file's value is cut to 240 bytes.

## Mutations

Each entity keeps its original `_entity.pdbx_mutation` text. Mutations come from
`_struct_ref_seq_dif` rows with `details = engineered mutation` and
`pdbx_seq_db_name = UNP`, written in UniProt numbering (`pdbx_seq_db_seq_num`) as
`C51S` (reference residue, position, modeled residue), with the UniProt accession
stored next to the list. Rows that cannot be mapped (another database, no
accession, nonstandard residue, no entity) are never guessed: they are listed as
`unmapped` and the entity is flagged **needs review**, as is an entity whose
mutation text has no matching rows.

## Reference lists

Five versioned files in `lib/cif`, each with `version`, `approved` and
`ccd_checked` (the date the refresh tool last checked it against the chemical
component dictionary, CCD). Researchers approved them on 2026-10-28 and
2026-10-29 (version `2026.10.0`):

| File | Holds | Edited by |
| --- | --- | --- |
| `cofactors.json` | 57 cofactors (heme, NAD, FAD, ubiquinones, ...), and the element lists the metal rule uses (`metal_elements`, `nonmetal_elements`; the metalloids B, Si, Ge, As, Sb, Te count as metals) | people, after researcher review |
| `metal_ions.json` | every released single-atom non-polymer of the CCD whose element is a metal (83 IDs: the 60 the researchers reviewed and 23 the rule adds) | the refresh tool |
| `solvents.json` | `solvents`: the drop list (36 IDs and 4 water IDs); `ambiguous`: kept and flagged (8 IDs); `keep_when_metal_linked`: imidazole | people, after researcher review |
| `covalent_cofactors.json` | modified residue → bound cofactor (LLP → PLP, LYR → RET) | people, after researcher review |
| `ccd_types.json` | the CCD IDs whose type is a peptide, nucleotide or saccharide type, by category (about 4,200 IDs); every other ID is a non-polymer | the refresh tool |

Every extraction reports `reference_version`, naming every file version
(`cofactors:<v>+metals:<v>+solvents:<v>+covalent:<v>+ccd-types:<v>`), so values
can be re-derived after a change. Paired IDs get the same decision (CIT/FLC,
ACT/ACY, MPD/MRD); a test checks it, and that every ID sits in exactly one group.

**Researchers decide what is on the lists.** It is a science decision, not an
engineering one: a wrong drop-list entry silently removes a ligand from the
record. Engineers do not add, remove or move an ID without researcher sign-off,
and never add IDs by pattern (UQ3, MQ4 and MQ6 are not quinones).

**A future list version starts as a draft.** `Reference.Approved` is true only
when every file says `"approved": true`. While any file is a draft:

- only water is dropped; every other compound on the drop list is kept in
  `ligands` and flagged "compound is on the draft solvent list ...";
- every extraction carries the warning `cif.DraftWarning`: the extract endpoint
  returns it in `warnings`, the CLI logs it once per run.

Tests use injectable reference data (`parseReference`), so the draft path stays
tested while the lists are approved.

## Refreshing the lists

`lib/cif/cmd/refresh-reference` checks every list against the CCD on RCSB and
regenerates the two files made by rule. It needs the network, so it runs by
hand, never in CI:

```bash
go run ./lib/cif/cmd/refresh-reference --check
```

`--check` reports drift and writes nothing (exit 1 on drift or a problem).
Without it the tool rewrites `metal_ions.json`, `ccd_types.json` and the
`ccd_checked` date of every file. It stops, and writes nothing, when:

- a listed ID is not in the CCD, is not released, or has a `pdbx_replaced_by`:
  a reviewed ID is never rewritten silently; the report names the replacement
  for the researchers to approve;
- a list ID is not a non-polymer, or a covalent-map residue is not a peptide
  residue;
- the metal rule would drop a metal that is on `metal_ions.json` now;
- the CCD has a type the parser does not know (the known types no longer cover
  every ID with a type): add it to `cif.CCDTypeCategory` and
  `cif.KnownCCDTypes`, then to the website's `ccdTypeCategory` (the parity
  test `testdata/ccd-type-categories.json` checks both).

IDs the metal rule adds are listed as notes, for the researchers to see in the
PR. BIRD molecule IDs (`PRD_...`) the search returns are not CCD IDs and are
skipped. After a change, bump the version of every file that changed, then run
`npm run gen:cif-reference` in `website/` (CI fails on a stale generated file).

## Website preview

The preview applies the same rule in TypeScript: `website/src/lib/parse/ligandRule.ts`
(the categories and the rule), `pdbChains.ts` (PDB-format chains) and
`structure.ts` (reading the file), with the review texts in `reviewReasons.ts`.
`npm run gen:cif-reference` writes the lists into `cifReference.gen.ts` and the
CCD categories into `ccdCategories.gen.ts`; the second is a module of its own
so the entry form, which only needs the lists, does not load the 4,000-ID table.
The parser is imported only when a structure file is dropped.

The Go parser's answers for the fixtures and for small synthetic files in
`lib/cif/testdata/parity/` are committed as `lib/cif/testdata/parity-golden.json`
(refresh with `UPDATE_PARITY_GOLDEN=1 go test ./lib/cif -run parity_golden`);
`website/test/cif-parity.test.cjs` checks that the preview gives the same
ligands, cofactors, review flags (with reasons), rules (including the compounds
left out), glycans and counts, for mmCIF and PDB-format files. The preview sends
the flags as `extensions.compound_review` on the model metadata, like the CLI;
it does not send the decision record or the glycans yet.

The preview reads the first 4 MiB of a file, like the CLI, but does not know when
it was cut: it applies what it read, where the Go parser skips a rule whose
category may have been cut off. Otherwise it reads a file as the Go parser does:
only the first data block, single-row categories as tokens (indented tags and
`#` comments allowed; a file with one connection writes `_struct_conn` that
way, as 6TQP does), and compounds sorted even when the file has no
coordinates. Its CCD type switch is checked against the Go parser's by
`lib/cif/testdata/ccd-type-categories.json`.

## CLI upload

The CLI uploads `ligands` and `cofactors` for coordinate files. It reads the first 4 MiB of the file and parses it **once per artifact**: the parsed fields are cached on the uploader, so the ten or so manifest fields of one artifact cost one parse. When the file is longer, the truncation-aware parser is used, so atom counts, residue counts and altloc fraction are left out instead of being undercounted (for PDB files too). As with `ligands`, an empty list is not uploaded: a missing `cofactors` field means none were found (or the coordinates were cut off), while `POST /v1/artifacts/{id}/extract` reports `[]` when the coordinates were read in full and held none.

Scalar fields read from a data-folder file (for example R-free from `models/5amf_model.cif`) are streamed with `cif.ScalarValueFromReader`; the file is never loaded whole.

**Review flags.** A compound the parser flags (an ambiguous compound such as sulfate, a metal with no `metalc` link, a residue outside the polymer chain, a residue that could be a chain residue in a file without `SEQRES` or `_entity`) is uploaded in the model's `extensions` object, which the backend declares as open data on `ModelMetadata`:

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
object with a ranged S3 GET. If the coordinates run past the limit, atom counts,
residue counts and altloc fraction are left out with a warning. The object must
sit in the artifact's own upload folder, and the caller must be the artifact's
creator or a reviewer.

Archive files do not write their categories in a fixed order: the depositor
flags (`_pdbx_entity_instance_feature`, after `_atom_site` in 5ZCK), the glycan
descriptors (1C0R) and the modified residues (3CEB) can all come after the
coordinates. A category that is missing, or open at the end, in a cut-off read is
never guessed: the rule it feeds is not applied, and one warning names each such
category. A category is open at the end when no `#` line closed it before the
cut, whether it is a loop or a single row written as items (its `asym_id` could
follow in the part not read), and when the time budget stopped the read inside
it, even between two rows. The warning reads, for example "the read limit may
have cut these categories, so the rules they feed were not applied:
_pdbx_entity_instance_feature (depositor flags), _pdbx_struct_mod_residue
(covalently bound cofactors)". `_chem_comp` is the one exception: the rows read
are used, and a compound whose row was cut off falls back to the CCD table,
which gives an archive file's own types.

### Time budget

Parsing stops after 5 s (`Server.WithExtractionTimeout`, default `cif.TimeBudget`) or when the client disconnects. A timeout is reported like a truncated read, not as an error: the response is `200` with the values found so far and the warning `parser limit: time budget exceeded: the rest of the file was not read` (counts that need every atom are left out). The slot is released when the handler returns, so a slot is held for at most the ranged GET plus 5 s.

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

The endpoint warns (`warnings`) while a reference list is a draft; see "Reference lists".
