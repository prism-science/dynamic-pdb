# Test fixtures

Every file here is in the parity golden (`parity-golden.json`), so the Go parser
and the website preview are checked against each other on all of them.
`ccd-type-categories.json` holds the Go parser's category for every CCD type,
which the website's own switch is checked against.

## Real mmCIF files

From files.rcsb.org. Every category outside `_atom_site` is verbatim. The
`_atom_site` loop is cut to every `HETATM` row, every row with an alternate
location, and the first 8 residues of each chain, to keep the files small.
Counts in the tests are for these cut files.

| File | Used for |
| --- | --- |
| 4HHB | several entities, heme with `metalc` links, phosphate (ambiguous, flagged) |
| 3CLN | calcium ions with `metalc` links (also the no-`metalc` test, with the rows removed in the test) |
| 6OIM | engineered mutations in UniProt numbering, Mg with `metalc`, covalent ligand |
| 193L | alternate locations, sodium with `metalc`, chloride (ambiguous) |
| 1AKE | `;` text blocks, alternate locations, two chains |
| 184D | DNA with a magnesium whose only `metalc` partners are waters (flagged cofactor), and one with no `metalc` row |
| 4UG1 | imidazole bound to nickel by `metalc` (kept), two unbound imidazoles (left out) |
| 1W53 | xenon, a single non-metal atom on no list (not flagged), glycerol (left out) |
| 4HB1 | UNX, a single atom of unknown element (flagged) |
| 1A62 | selenomethionine (MSE) in the chain: a polymer residue, not a ligand |
| 7LTB | a branched disaccharide with a WURCS descriptor, and depositor flags (SUBJECT OF INVESTIGATION) on ligands, sugars, a formate and the antibiotic's own polymer residues |
| 3CEB | LLP in `_pdbx_struct_mod_residue` (adds PLP as a cofactor), MSE, sulfate and glycerol; its modified residues come after `_atom_site` |
| 9A8B | an integrative (IHM) entry whose `HETATM` residues all belong to a `POLYMER` entity |
| 2VSD | an N-glycoprotein: an NAG-NAG-BMA glycan on an asparagine, with a WURCS descriptor and no depositor flags (rule `branched`) |
| 6TQP | a single connection, so `_struct_conn` is written as `_struct_conn.tag value` items, not a loop: a sodium bound to a glutamate (a cofactor) |
| 1C0R | the whole file (77 KB, not cut): `_atom_site` comes before `_pdbx_nonpoly_scheme` and `_pdbx_branch_scheme`, and `_pdbx_entity_nonpoly` comes last. Its two disaccharides, lactate and three chlorides are all modeled compounds, although RCSB's "bound components" for the entry list none of them |

## Real PDB-format files

The same entries in PDB format, cut the same way: every `HETATM` record, every
`ATOM` with an alternate location, and the `ATOM` records of the first 8
residues of each chain. `SEQRES`, `MODRES`, `LINK` and `TER` records are kept as
they are.

| File | Used for |
| --- | --- |
| 1A62.pdb | MSE in `SEQRES`, `MODRES` and before chain A's `TER`: polymer |
| 3CLN.pdb | calcium bound to aspartates by `LINK` records: a linked cofactor |
| 3CEB.pdb | `MODRES` LLP (adds PLP as a cofactor), MSE, sulfate and glycerol |
| 1II5.pdb | a free glutamate after chain A's `TER` record: a ligand flagged "residue outside the polymer chain" |
| 7LTB.pdb | the antibiotic's residues in `SEQRES` before `TER` (polymer whatever their CCD type), sugars in chains without `SEQRES` (glycans) |
| 6OIM.pdb | GDP ("RNA linking" in the CCD, named in no `SEQRES`): an ordinary ligand; Mg linked by `LINK` records |
| 2HQZ.pdb | a magnesium whose only `LINK` partners are waters (a cofactor flagged "metal linked only to water"); the chromophore CRO, linked into its chain, is polymer; ethylene glycol is left out |

## Synthetic files (`parity/`)

| File | Used for |
| --- | --- |
| instances.cif | per-instance rules: a zinc linked to the protein and one only to water, sulfate, a compound seen only in `_pdbx_entity_nonpoly`, a single carbon atom (ZZ9, not flagged) |
| no_metalc.cif | metals in a file without `metalc` rows (flagged cofactors), chloride and phosphate (ambiguous), glycerol (left out) |
| two_models.cif | only the first model counts |
| wrapped_rows.cif | loop rows that wrap across lines |
| ihm_probe.cif | no released entry had `_ihm_ligand_probe` on 2026-10-29 (all 407 integrative entries checked). Two models; the probe is left out, and ATP, only in model 2, is listed |
| pdb_rules.pdb | a chain without `SEQRES` or `TER`: MSE and ATP inside its numbers and a glutamate far outside could be chain residues, so all three are kept and flagged; `LINK` records binding zinc to a cysteine and magnesium only to water, a calcium with no link. Archive PDB-format files always have `SEQRES`; files from refinement programs may not (the magnesium case is also in the real 2HQZ.pdb) |
| entity_rules.cif | a glycan without depositor flags (`branched`), a macrolide, an unknown entity type (flagged; its ethylene glycol, which the lists leave out, is kept and flagged, and a magnesium linked only to it counts as linked), a depositor flag with a location (only one glycerol kept), NO FUNCTIONAL ROLE on sulfate (left out). None of 4,000 random prod entries (2026-10-29) has a `macrolide` entity, and archive files use no unknown entity type |
| no_entity_chem_comp.cif | a file without `_entity`: its own `_chem_comp.type` (NAG as non-polymer, an unknown type flagged), MSE with a `label_seq_id` and a free MSE (both kept and flagged: nothing says what the chains hold), BGC a sugar by the CCD table |
| pdb_no_metal_link.pdb | the only `LINK` joins a sugar to an asparagine, so the zinc is flagged as in a file without metal links |
| metal_partner_flags.cif | a zinc whose only `metalc` partner is an ATP marked NO FUNCTIONAL ROLE (unlinked: a flagged ligand), a magnesium whose only partner is a glycerol marked SUBJECT OF INVESTIGATION (a cofactor), a calcium whose only partner is an ADP declared without coordinates and marked NO FUNCTIONAL ROLE (unlinked), a nickel bound to a cysteine that carries a flag (a cofactor). No metal in 4,000 random prod entries (2026-10-29) has only such partners |
| pair_form_layout.cif | a single depositor flag written as indented items with `#` comments (RCSB files never indent tags) |
| two_data_blocks.cif | a second data block after the structure, holding a flag on ATP that is not applied (RCSB files have one block) |
| no_atom_site.cif | no coordinates: ATP from `_pdbx_entity_nonpoly`, a glycan record and PLP from LLP are still sorted (RCSB files always have `_atom_site`) |
| unlocated_metalc.cif | a `metalc` row with compound IDs only, and coordinates with a residue number but no asym ID: the row applies to both calcium ions, both cofactors (RCSB files always name both sides of a connection) |
| pdb_blank_chain.pdb | a blank chain ID: zinc 101, bound to a histidine by `LINK`, is a cofactor; zinc 102, with no link, is a flagged ligand. Archive files give every residue a chain ID; files from refinement programs may not |
| no_entity_3CEB.cif | 3CEB.cif with its `_entity` category removed, as in a refinement program's output: MSE and LLP (with a `label_seq_id`) are flagged; sulfate and glycerol are sorted by the lists; PLP still comes from LLP through `_pdbx_struct_mod_residue`, which does not count as chain membership. Archive files always have `_entity` |
| no_seqres_3CEB.pdb | 3CEB.pdb with its `SEQRES` and `MODRES` records removed, as in a refinement program's output: MSE and LLP in the chain are flagged, and no PLP is added without `MODRES`. Archive files always have `SEQRES` |
| pdb_seqres_columns.pdb | a `SEQRES` record with a residue name past column 70, which is not read, so the free glutamate after `TER` is an ordinary ligand |
