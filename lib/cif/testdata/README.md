# Test fixtures

Real archive files from files.rcsb.org (4HHB, 3CLN, 6OIM, 193L, 1AKE). Every
category outside `_atom_site` is verbatim. The `_atom_site` loop is cut to every
`HETATM` row, every row with an alternate location, and the first 8 residues of
each chain, to keep the files small. Counts in the tests are for these cut files.

| File | Used for |
| --- | --- |
| 4HHB | several entities, heme with `metalc` links, phosphate buffer |
| 3CLN | calcium ions with `metalc` links (also the no-`metalc` test, with the rows removed in the test) |
| 6OIM | engineered mutations in UniProt numbering, Mg with `metalc`, covalent ligand |
| 193L | alternate locations, sodium with `metalc` |
| 1AKE | `;` text blocks, alternate locations, two chains |
