require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ConformerTally,
  PdbPolymerReader,
  conformerCount,
  isHydrogenAtom,
  occupancyValue,
} = require("../src/lib/parse/conformers.ts");

test("should count a letter only when its highest heavy-atom occupancy reaches a tenth", () => {
  // given
  const letters = new Map([["A", 0.9], ["B", 0.1], ["C", 0.05]]);

  // when
  const count = conformerCount(letters);

  // then
  assert.equal(count, 2);
  assert.equal(conformerCount(new Map()), 1, "a residue without letters has one conformer");
  assert.equal(conformerCount(new Map([["A", 0.07]])), 1, "never below one");
});

test("should tell hydrogens by element, or by name when the file gives no element", () => {
  // then
  assert.equal(isHydrogenAtom("H", "CB"), true);
  assert.equal(isHydrogenAtom("D", "DG"), true);
  assert.equal(isHydrogenAtom("C", "HB2"), false, "the element decides when there is one");
  assert.equal(isHydrogenAtom("", "1HD1"), true);
  assert.equal(isHydrogenAtom("", "CD1"), false);
});

test("should read a missing or unreadable occupancy as full occupancy", () => {
  // then
  assert.equal(occupancyValue(""), 1);
  assert.equal(occupancyValue("x"), 1);
  assert.equal(occupancyValue("0.25"), 0.25);
});

test("should keep two residue types at one position apart and leave hydrogen-only letters out", () => {
  // given: PRO 22 with A, SER 22 with B and C (microheterogeneity); LEU 23 with B only on a hydrogen.
  const tally = new ConformerTally();
  const reader = new PdbPolymerReader(tally);
  const atom = (serial, name, alt, compound, residue, occupancy, element) =>
    `ATOM  ${String(serial).padStart(5)} ${name.padEnd(4)}${alt}${compound} A${String(residue).padStart(4)}    ` +
    `   0.000   0.000   0.000${occupancy.toFixed(2).padStart(6)} 10.00          ${element.padStart(2)}`;
  for (const [index, line] of [
    atom(1, "CA", "A", "PRO", 22, 0.6, "C"),
    atom(2, "CA", "B", "SER", 22, 0.2, "C"),
    atom(3, "CA", "C", "SER", 22, 0.2, "C"),
    atom(4, "CA", "A", "LEU", 23, 0.6, "C"),
    atom(5, "HB2", "B", "LEU", 23, 0.4, "H"),
  ].entries()) {
    assert.ok(line.length >= 78, `record ${index} is a full PDB line`);
    reader.addCoordinate("ATOM", line);
  }
  reader.endModel();

  // when
  const fraction = tally.altlocFraction();

  // then: PRO 1, SER 2, LEU 1 conformers: one residue of three with two or more.
  assert.equal(fraction, 1 / 3);
});
