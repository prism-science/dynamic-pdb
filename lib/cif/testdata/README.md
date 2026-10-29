# Test fixtures

Real archive files from files.rcsb.org (4HHB, 3CLN, 6OIM, 193L, 1AKE, 184D,
4UG1, 1W53, 4HB1, 1A62, 7LTB, 3CEB, 9A8B). Every category outside `_atom_site`
is verbatim. The `_atom_site` loop is cut to every `HETATM` row, every row with
an alternate location, and the first 8 residues of each chain, to keep the
files small. Counts in the tests are for these cut files. `parity/` holds small
synthetic files. The `.pdb` files are the same entries in PDB format, cut the
same way (every `HETATM` record, every `ATOM` with an alternate location, the
`ATOM` records of the first 8 residues of each chain); `SEQRES`, `MODRES`,
`LINK` and `TER` records are kept as they are.

| File | Used for |
| --- | --- |
| 4HHB | several entities, heme with `metalc` links, phosphate buffer |
| 3CLN | calcium ions with `metalc` links (also the no-`metalc` test, with the rows removed in the test) |
| 6OIM | engineered mutations in UniProt numbering, Mg with `metalc`, covalent ligand |
| 193L | alternate locations, sodium with `metalc` |
| 1AKE | `;` text blocks, alternate locations, two chains |
| 184D | DNA with a magnesium whose only `metalc` partners are waters (flagged cofactor), and one with no `metalc` row |
| 4UG1 | imidazole bound to nickel by `metalc` (kept), two unbound imidazoles (dropped) |
| 1W53 | xenon, a single non-metal atom on no list (not flagged), glycerol (dropped) |
| 4HB1 | UNX, a single atom of unknown element (flagged) |
| 1A62 | selenomethionine (MSE) in the chain: a polymer residue, not a ligand |
| 7LTB | a branched disaccharide with a WURCS descriptor, and depositor flags (SUBJECT OF INVESTIGATION) on ligands, sugars, a formate and the antibiotic's own polymer residues |
| 3CEB | LLP in `_pdbx_struct_mod_residue` (adds PLP as a cofactor), MSE, sulfate and glycerol; its modified residues come after `_atom_site` |
| 9A8B | an integrative (IHM) entry whose `HETATM` residues all belong to a `POLYMER` entity |
| parity/ihm_probe.cif | synthetic: no released entry had `_ihm_ligand_probe` on 2026-10-29 (all 407 integrative entries checked). Two models; the probe is left out and ATP, only in model 2, is listed |
| 1A62.pdb | MSE in `SEQRES`, `MODRES` and before chain A's `TER`: polymer |
| 3CLN.pdb | calcium bound to aspartates by `LINK` records: a linked cofactor |
| 3CEB.pdb | `MODRES` LLP (adds PLP as a cofactor), MSE, sulfate and glycerol |
| 1II5.pdb | a free glutamate after chain A's `TER` record: a ligand flagged "residue outside the polymer chain" |
| 7LTB.pdb | the antibiotic's residues in `SEQRES` before `TER` (polymer whatever their CCD type), sugars in chains without `SEQRES` (glycans) |
| parity/pdb_rules.pdb | synthetic: a chain without `SEQRES` or `TER` (MSE inside its numbers is polymer, ATP inside them stays a ligand, a glutamate far outside is flagged), `LINK` records binding zinc to a cysteine and magnesium only to water, a calcium with no link |
| parity/pdb_no_metal_link.pdb | synthetic: the only `LINK` joins a sugar to an asparagine, so the zinc is flagged as in a file without metal links |
