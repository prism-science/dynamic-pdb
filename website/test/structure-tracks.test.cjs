require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  chainsOfEntity,
  readStructure,
  sequenceShift,
  shiftResidues,
  unobservedSpans,
} = require("../src/lib/structure-tracks.ts");

// Residues 1, 2 and 4 of entity 1: residue 3 is missing, residue 2 has two
// conformations, and there is one helix over 1-2.
const CIF = `data_TEST
#
loop_
_struct_conf.conf_type_id
_struct_conf.beg_label_asym_id
_struct_conf.beg_label_seq_id
_struct_conf.end_label_seq_id
HELX_P A 1 2
TURN_P A 3 4
#
loop_
_struct_sheet_range.sheet_id
_struct_sheet_range.beg_label_asym_id
_struct_sheet_range.beg_label_seq_id
_struct_sheet_range.end_label_seq_id
A A 4 4
#
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.label_alt_id
_atom_site.B_iso_or_equiv
ATOM A 1 1 . 10.0
ATOM A 1 1 . 20.0
ATOM A 1 2 A 30.0
ATOM A 1 2 B 50.0
ATOM A 1 4 . 40.0
HETATM B 2 . . 99.0
#
`;

test("should read observed residues, skipping ligands and waters", () => {
  // when
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // then
  assert.deepEqual([...entity.observed].sort((a, b) => a - b), [1, 2, 4]);
});

test("should average the B-factor over each residue's atoms", () => {
  // when
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // then
  assert.equal(entity.bFactor.get(1), 15);
  assert.equal(entity.bFactor.get(2), 40);
  assert.equal(entity.bFactor.get(4), 40);
});

test("should mark a residue modelled in more than one conformation", () => {
  // when
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // then
  assert.deepEqual([...entity.alternates], [2]);
});

test("should count alternate conformers for every residue", () => {
  // when
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // then
  assert.deepEqual([...entity.conformerCount], [
    [1, 1],
    [2, 2],
    [4, 1],
  ]);
});

test("should calculate residue RMSF across ensemble members", () => {
  // given: both residues move one angstrom between two MODEL frames
  const ensemble = `data_ENSEMBLE
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.label_alt_id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
_atom_site.pdbx_PDB_model_num
ATOM A A 1 1 1 . C CA 0 0 0 1
ATOM A A 1 2 2 A C CA 5 0 0 1
ATOM A A 1 2 2 B C CA 6 0 0 1
ATOM A A 1 1 1 . C CA 1 0 0 2
ATOM A A 1 2 2 A C CA 6 0 0 2
ATOM A A 1 2 2 B C CA 7 0 0 2
#
`;

  // when
  const [entity] = readStructure(ensemble);

  // then
  assert.deepEqual([...entity.conformerCount], [
    [1, 1],
    [2, 2],
  ]);
  assert.ok(Math.abs(entity.rmsf.get(1) - 0.5) < 1e-8);
  assert.ok(Math.abs(entity.rmsf.get(2) - 0.5) < 1e-8);
});

test("should take helices from struct_conf and leave turns out of them", () => {
  // when
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // then
  assert.deepEqual(entity.helices, [{ start: 1, end: 2 }]);
  assert.deepEqual(entity.strands, [{ start: 4, end: 4 }]);
});

test("should report the residues the coordinates do not contain", () => {
  // given
  const entity = chainsOfEntity(readStructure(CIF), "1")[0];

  // when
  const missing = unobservedSpans(entity.observed, 6);

  // then
  assert.deepEqual(missing, [
    { start: 3, end: 3 },
    { start: 5, end: 6 },
  ]);
});

test("should return nothing for a file with no atom_site loop", () => {
  // when / then
  assert.deepEqual(readStructure("data_EMPTY\n#\n"), []);
});

test("should read a quoted value without splitting it on its spaces", () => {
  // given
  const cif = `data_Q
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_comp_id
ATOM A 1 1 'MET A'
#
`;

  // when
  const entity = chainsOfEntity(readStructure(cif), "1")[0];

  // then
  assert.deepEqual([...entity.observed], [1]);
});

test("should keep the chains of one entity apart", () => {
  // given: entity 1 in two chains, with a residue modelled in only one of them
  const cif = `data_TWO
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.B_iso_or_equiv
ATOM A A 1 1 10.0
ATOM A A 1 2 20.0
ATOM B B 1 1 30.0
#
`;

  // when
  const chains = chainsOfEntity(readStructure(cif), "1");

  // then
  assert.deepEqual(chains.map((chain) => chain.chainId), ["A", "B"]);
  assert.deepEqual([...chains[0].observed], [1, 2]);
  assert.deepEqual([...chains[1].observed], [1]);
  assert.equal(chains[1].bFactor.get(1), 30);
});

test("should order chains by entity number rather than by text", () => {
  // given
  const cif = `data_ORDER
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
ATOM J J 10 1
ATOM B B 2 1
#
`;

  // when
  const chains = readStructure(cif);

  // then
  assert.deepEqual(chains.map((chain) => chain.entityId), ["2", "10"]);
});

test("should return no chains for an entity the coordinates do not name", () => {
  // when / then
  assert.deepEqual(chainsOfEntity(readStructure(CIF), "7"), []);
  assert.deepEqual(chainsOfEntity(readStructure(CIF), null), []);
});

// The same three residues written by two programs: the deposit numbers them in
// the entity's sequence and by the author's numbering, and a re-refinement
// renumbers its own sequence from 1 while keeping the author's numbers.
const DEPOSITED = `data_DEP
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.label_comp_id
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
ATOM A A 1 4 21 GLU CA . 1.0 0.0 0.0
ATOM A A 1 5 22 LEU CA . 4.8 0.0 0.0
ATOM A A 1 6 23 TRP CA . 8.6 0.0 0.0
#
`;

const REREFINED = `data_REF
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.label_comp_id
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
ATOM A A ? 1 21 GLU CA . 1.0 0.0 0.0
ATOM A A ? 2 22 LEU CA . 4.8 0.0 0.0
ATOM A A ? 3 23 TRP CA . 8.6 0.0 0.0
#
`;

function pdbRecord(fields) {
  const line = Array(80).fill(" ");
  for (const [column, value] of fields) {
    const text = String(value);
    line.splice(column - 1, text.length, ...text);
  }
  return line.join("");
}

function pdbAtom({
  record = "ATOM",
  serial,
  atom,
  alternate = " ",
  residue,
  chain,
  sequence,
  insertion = " ",
  x,
  y,
  z,
  occupancy = 1,
  bFactor,
  element,
}) {
  return pdbRecord([
    [1, record.padEnd(6)],
    [7, String(serial).padStart(5)],
    [13, atom.padStart(4)],
    [17, alternate],
    [18, residue.padStart(3)],
    [22, chain],
    [23, String(sequence).padStart(4)],
    [27, insertion],
    [31, x.toFixed(3).padStart(8)],
    [39, y.toFixed(3).padStart(8)],
    [47, z.toFixed(3).padStart(8)],
    [55, occupancy.toFixed(2).padStart(6)],
    [61, bFactor.toFixed(2).padStart(6)],
    [77, element.padStart(2)],
  ]);
}

const PDB = [
  pdbRecord([
    [1, "HELIX "],
    [20, "A"],
    [22, "  21"],
    [32, "A"],
    [34, "  22"],
  ]),
  pdbRecord([
    [1, "SHEET "],
    [22, "A"],
    [23, "  24"],
    [33, "A"],
    [34, "  24"],
  ]),
  pdbAtom({
    serial: 1, atom: "CA", residue: "GLU", chain: "A", sequence: 21,
    x: 1, y: 0, z: 0, bFactor: 10, element: "C",
  }),
  pdbAtom({
    serial: 2, atom: "CB", residue: "GLU", chain: "A", sequence: 21,
    x: 2, y: 0, z: 0, bFactor: 20, element: "C",
  }),
  pdbAtom({
    serial: 3, atom: "CA", alternate: "A", residue: "LEU", chain: "A",
    sequence: 22, x: 4.8, y: 0, z: 0, bFactor: 30, element: "C",
  }),
  pdbAtom({
    serial: 4, atom: "CA", alternate: "B", residue: "LEU", chain: "A",
    sequence: 22, x: 5, y: 0, z: 0, bFactor: 50, element: "C",
  }),
  pdbAtom({
    serial: 5, atom: "CA", residue: "TRP", chain: "A", sequence: 24,
    x: 8.6, y: 0, z: 0, bFactor: 40, element: "C",
  }),
  pdbAtom({
    record: "HETATM", serial: 6, atom: "O", residue: "HOH", chain: "A",
    sequence: 25, x: 9, y: 0, z: 0, bFactor: 99, element: "O",
  }),
  "END",
].join("\n");

test("should read residue tracks from PDB coordinates", () => {
  // given: a PDB chain with a gap, alternate conformers and secondary structure

  // when
  const [chain] = readStructure(PDB, "pdb");

  // then
  assert.equal(chain.entityId, "");
  assert.equal(chain.chainId, "A");
  assert.deepEqual([...chain.observed], [21, 22, 24]);
  assert.deepEqual([...chain.names.values()], ["GLU", "LEU", "TRP"]);
  assert.equal(chain.bFactor.get(21), 15);
  assert.equal(chain.bFactor.get(22), 40);
  assert.deepEqual([...chain.alternates], [22]);
  assert.deepEqual([...chain.conformerCount], [
    [21, 1],
    [22, 2],
    [24, 1],
  ]);
  assert.deepEqual(chain.alpha.get(22), { x: 4.8, y: 0, z: 0 });
  assert.deepEqual(chain.helices, [{ start: 21, end: 22 }]);
  assert.deepEqual(chain.strands, [{ start: 24, end: 24 }]);
  assert.deepEqual(chainsOfEntity([chain], "1", ["X[auth A]"]), [chain]);
});

test("should calculate residue RMSF across PDB model frames", () => {
  // given: one alpha carbon moves one angstrom between two MODEL frames
  const ensemble = [
    "MODEL        1",
    pdbAtom({
      serial: 1, atom: "CA", residue: "GLU", chain: "A", sequence: 21,
      x: 0, y: 0, z: 0, bFactor: 10, element: "C",
    }),
    "ENDMDL",
    "MODEL        2",
    pdbAtom({
      serial: 2, atom: "CA", residue: "GLU", chain: "A", sequence: 21,
      x: 1, y: 0, z: 0, bFactor: 10, element: "C",
    }),
    "ENDMDL",
  ].join("\n");

  // when
  const [chain] = readStructure(ensemble, "pdb");

  // then
  assert.ok(Math.abs(chain.rmsf.get(21) - 0.5) < 1e-8);
});

test("should number residues the way the author does, not the way the file's own sequence does", () => {
  // when -- two files that disagree about every label_seq_id in them
  const [deposited] = readStructure(DEPOSITED);
  const [rerefined] = readStructure(REREFINED);

  // then -- both come out in author numbering, which is the one they share
  assert.deepEqual([...deposited.observed], [21, 22, 23]);
  assert.deepEqual([...rerefined.observed], [21, 22, 23]);
  assert.deepEqual([...deposited.names.values()], ["GLU", "LEU", "TRP"]);
});

test("should read one alpha carbon per residue", () => {
  // when
  const [deposited] = readStructure(DEPOSITED);

  // then
  assert.deepEqual([...deposited.alpha.keys()], [21, 22, 23]);
  assert.deepEqual(deposited.alpha.get(22), { x: 4.8, y: 0, z: 0 });
});

test("should still read a file that does not say which entity its chains belong to", () => {
  // when -- qFit writes label_entity_id as unknown
  const [rerefined] = readStructure(REREFINED);

  // then -- the chain is kept, with an empty entity id, and found by its name
  assert.equal(rerefined.entityId, "");
  assert.equal(rerefined.chainId, "A");
  assert.deepEqual(chainsOfEntity([rerefined], "1", ["A"]), [rerefined]);
  // ...and not claimed by an entity that names other chains
  assert.deepEqual(chainsOfEntity([rerefined], "1", ["B"]), []);
});

test("should find where a chain's numbering sits on the entry's sequence", () => {
  // given -- the entry's sequence, with the chain's three residues at 21-23
  const sequence = `${"A".repeat(20)}ELW${"A".repeat(10)}`;
  const [deposited] = readStructure(DEPOSITED);

  // when
  const shift = sequenceShift(deposited, sequence);

  // then -- author numbering already lands on the sequence here
  assert.equal(shift, 0);

  // and a chain numbered from 1 is moved onto the same positions
  const renumbered = { ...deposited, observed: new Set([1, 2, 3]),
    names: new Map([[1, "GLU"], [2, "LEU"], [3, "TRP"]]),
    alpha: new Map([[1, { x: 1, y: 0, z: 0 }]]), bFactor: new Map(),
    conformerCount: new Map([[2, 2]]), rmsf: new Map([[2, 0.5]]),
    alternates: new Set([2]), helices: [{ start: 1, end: 2 }], strands: [] };
  const moved = shiftResidues(renumbered, sequenceShift(renumbered, sequence));
  assert.deepEqual([...moved.observed], [21, 22, 23]);
  assert.deepEqual([...moved.alternates], [22]);
  assert.equal(moved.conformerCount.get(22), 2);
  assert.equal(moved.rmsf.get(22), 0.5);
  assert.deepEqual(moved.helices, [{ start: 21, end: 22 }]);
  assert.ok(moved.alpha.has(21));
});

test("should leave a chain it cannot line up on its own numbering", () => {
  // given -- a sequence the residues do not occur in
  const [deposited] = readStructure(DEPOSITED);

  // when / then
  assert.equal(sequenceShift(deposited, "A".repeat(40)), 0);
  assert.equal(sequenceShift(deposited, null), 0);
});

test("should read two records that meet end to end as one span", () => {
  // given -- a sheet recorded strand by strand, two of which touch, and a
  // helix in two records that overlap
  const cif = `data_JOIN
loop_
_struct_sheet_range.sheet_id
_struct_sheet_range.beg_label_asym_id
_struct_sheet_range.beg_label_seq_id
_struct_sheet_range.end_label_seq_id
A A 14 20
B A 21 24
C A 30 33
#
loop_
_struct_conf.conf_type_id
_struct_conf.beg_label_asym_id
_struct_conf.beg_label_seq_id
_struct_conf.end_label_seq_id
HELX_P A 40 46
HELX_P A 44 50
#
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
ATOM A A 1 14
ATOM A A 1 50
#
`;

  // when
  const [chain] = readStructure(cif);

  // then -- 14-20 and 21-24 are one bar; 30-33 stays its own
  assert.deepEqual(chain.strands, [
    { start: 14, end: 24 },
    { start: 30, end: 33 },
  ]);
  assert.deepEqual(chain.helices, [{ start: 40, end: 50 }]);
});
