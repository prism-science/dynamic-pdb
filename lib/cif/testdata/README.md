# Test fixtures

Every file here is in the parity golden (`parity-golden.json`), so the Go parser
and the website preview are checked against each other on all of them.

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

## Synthetic files (`parity/`)

| File | Used for |
| --- | --- |
| instances.cif | per-instance rules: a zinc linked to the protein and one only to water, sulfate, a compound seen only in `_pdbx_entity_nonpoly`, a single carbon atom (ZZ9, not flagged) |
| no_metalc.cif | metals in a file without `metalc` rows (flagged cofactors), chloride and phosphate (ambiguous), glycerol (left out) |
| two_models.cif | only the first model counts |
| wrapped_rows.cif | loop rows that wrap across lines |
| ihm_probe.cif | no released entry had `_ihm_ligand_probe` on 2026-10-29 (all 407 integrative entries checked). Two models; the probe is left out, and ATP, only in model 2, is listed |
| pdb_rules.pdb | a chain without `SEQRES` or `TER` (MSE inside its numbers is polymer, ATP inside them stays a ligand, a glutamate far outside is an ordinary ligand), `LINK` records binding zinc to a cysteine and magnesium only to water, a calcium with no link |
| pdb_no_metal_link.pdb | the only `LINK` joins a sugar to an asparagine, so the zinc is flagged as in a file without metal links |
