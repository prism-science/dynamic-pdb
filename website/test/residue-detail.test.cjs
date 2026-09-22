require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { residueDetail } = require("../src/lib/residue-detail.ts");
const {
  readStructure,
  shiftResidues,
} = require("../src/lib/structure-tracks.ts");

// One chain, written the way a real file writes one: the author numbering
// starts at 7, residue 9 is split in two conformations, residue 11 is left
// unmodelled and the file records a helix over 7-9.
const CIF = `data_TEST
#
loop_
_struct_conf.conf_type_id
_struct_conf.beg_label_asym_id
_struct_conf.beg_auth_seq_id
_struct_conf.end_auth_seq_id
HELX_P A 7 9
#
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
_atom_site.occupancy
_atom_site.B_iso_or_equiv
_atom_site.type_symbol
ATOM A A 1 1 7 MET CA . 1.00 15.0 C
ATOM A A 1 2 8 LYS CA . 1.00 20.0 C
ATOM A A 1 3 9 ILE CA A 0.60 18.0 C
ATOM A A 1 3 9 ILE CB A 0.60 19.0 C
ATOM A A 1 3 9 ILE CA B 0.40 22.0 C
ATOM A A 1 4 10 GLY CA . 1.00 25.0 C
ATOM A A 1 6 12 VAL CA . 1.00 30.0 C
#
`;

// The entry numbers this chain from 1, so the file's residue 7 is position 1.
const STRUCTURE = shiftResidues(readStructure(CIF)[0], -6);

const CONTEXT = {
  sequence: "MKIGLV",
  structure: STRUCTURE,
  residueData: [{ label_asym_id: "A", label_seq_id: 3, rscc: 0.97 }],
  agreement: null,
};

function group(detail, key) {
  return detail.groups.find((item) => item.key === key);
}

function fact(detail, groupKey, factKey) {
  const found = group(detail, groupKey);
  assert.ok(found, `no group ${groupKey}`);
  const item = found.facts.find((each) => each.key === factKey);
  assert.ok(item, `no fact ${factKey} in ${groupKey}`);
  return item;
}

function labels(detail) {
  return detail.groups.flatMap((item) => item.facts.map((each) => each.label));
}

test("should name the residue in the header and nowhere else", () => {
  // when
  const detail = residueDetail(CONTEXT, 3);

  // then -- the name is the heading, the chain is chosen above the board and
  // the position is on the ruler, so none of the three is a row
  assert.equal(detail.title, "ILE 3");
  for (const gone of ["Residue", "Chain", "Position"]) {
    assert.equal(labels(detail).includes(gone), false, gone);
  }
});

test("should list each alternate conformation with what it holds", () => {
  // when
  const detail = residueDetail(CONTEXT, 3);

  // then
  assert.equal(fact(detail, "model", "conformer_count").value, "2");
  assert.equal(fact(detail, "model", "alt-A").value, "occupancy 0.60");
  assert.equal(fact(detail, "model", "alt-B").value, "occupancy 0.40");
});

test("should average the B-factor over every atom of the residue", () => {
  // when -- 18, 19 and 22 across the two conformations
  const detail = residueDetail(CONTEXT, 3);

  // then
  assert.equal(fact(detail, "model", "b_iso").value, "19.7 Å²");
});

test("should place the residue in its secondary structure", () => {
  // when
  const detail = residueDetail(CONTEXT, 2);

  // then -- the file's 7-9 read in the entry's numbering
  assert.equal(fact(detail, "model", "secondary").value, "Helix 1–3");
});

test("should leave a field out rather than draw it empty", () => {
  // when -- residue 1 is modelled, but nothing validates or compares it
  const detail = residueDetail(CONTEXT, 1);

  // then -- only the fields that have something in them
  assert.deepEqual(
    group(detail, "model").facts.map((item) => item.key),
    ["secondary", "conformer_count", "b_iso"],
  );
  // a column of dashes and apologies is a column to work through, not to read
  assert.equal(
    detail.groups.every((item) =>
      item.facts.every((each) => each.value.length > 0),
    ),
    true,
  );
});

test("should give a residue this model left out nothing but its name", () => {
  // when -- position 5 is in the sequence and not in the coordinates
  const detail = residueDetail(CONTEXT, 5);

  // then -- the heading is the whole answer; a panel of empty groups is not
  assert.equal(detail.title, "L 5");
  assert.deepEqual(detail.groups, []);
});

test("should fall back to the entry's record with no coordinates read", () => {
  // given -- a chain we know only from the entry
  const detail = residueDetail(
    {
      ...CONTEXT,
      structure: null,
      residueData: [
        {
          label_asym_id: "A",
          label_seq_id: 3,
          label_comp_id: "ILE",
          b_iso: 18.4,
          conformer_count: 2,
          rscc: 0.97,
        },
      ],
    },
    3,
  );

  // then
  assert.equal(detail.title, "ILE 3");
  assert.equal(fact(detail, "model", "b_iso").value, "18.4 Å²");
  assert.equal(fact(detail, "model", "conformer_count").value, "2");
  // and nothing on the panel claims the coordinates were read
  assert.equal(
    group(detail, "model").facts.some((item) => item.key === "secondary"),
    false,
  );
});

test("should read the stored validation value for the residue", () => {
  // when
  const detail = residueDetail(CONTEXT, 3);

  // then
  assert.equal(fact(detail, "validation", "rscc").value, "0.970");
  // and a residue with none has no Validation group at all
  assert.equal(group(residueDetail(CONTEXT, 1), "validation"), undefined);
});

test("should report how far apart the models put the residue", () => {
  // given
  const detail = residueDetail(
    {
      ...CONTEXT,
      agreement: {
        fitted: [],
        unfitted: 0,
        models: 3,
        disagreement: new Map([[3, 0.42]]),
        placed: new Map([[3, 3]]),
        worst: { seq: 3, value: 0.42 },
        regions: [{ start: 2, end: 4 }],
        alternates: new Set([3]),
      },
    },
    3,
  );

  // then -- the spread and nothing else: how many models place it, which of
  // them split it and which run it falls in were all noise beside it
  assert.deepEqual(
    group(detail, "between").facts.map((item) => item.key),
    ["spread"],
  );
  assert.equal(fact(detail, "between", "spread").value, "0.42 Å");
});

test("should drop the comparison group when there is nothing to compare with", () => {
  // when
  const detail = residueDetail(CONTEXT, 3);

  // then
  assert.equal(group(detail, "between"), undefined);
});

test("should refuse a position that is not one", () => {
  // then
  assert.equal(residueDetail(CONTEXT, 0), null);
  assert.equal(residueDetail(CONTEXT, 1.5), null);
});

test("should label every field in words, not in schema names", () => {
  // when
  const detail = residueDetail(CONTEXT, 3);

  // then -- the panel is read by people looking at a protein
  for (const label of labels(detail)) {
    assert.doesNotMatch(label, /_/, `"${label}" reads as a column name`);
    assert.match(label, /^[A-Z]/, `"${label}" should start as a sentence`);
  }
  // and the schema's own name is still there for whoever queries the API
  assert.equal(fact(detail, "model", "b_iso").field, "b_iso");
  assert.equal(fact(detail, "model", "b_iso").label, "B-factor");
});
