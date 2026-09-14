require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  chainAgreement,
  chainCounterpart,
  DISAGREEMENT_FLOOR,
} = require("../src/lib/model-agreement.ts");

// A straight run of alpha carbons 3.8 A apart, which is what a backbone is.
function chain(count, options = {}) {
  const alpha = new Map();
  const observed = new Set();
  const names = new Map();
  for (let seq = 1; seq <= count; seq += 1) {
    if (options.skip?.includes(seq)) {
      continue;
    }
    observed.add(seq);
    names.set(seq, "ALA");
    alpha.set(seq, { x: seq * 3.8, y: 0, z: 0 });
  }
  return {
    entityId: options.entityId ?? "1",
    chainId: options.chainId ?? "A",
    observed,
    names,
    bFactor: new Map(),
    alternates: new Set(options.alternates ?? []),
    alpha,
    helices: options.helices ?? [],
    strands: [],
  };
}

/** Push a run of residues sideways, the way a differently built loop looks. */
function bend(residues, from, to, by) {
  const moved = { ...residues, alpha: new Map(residues.alpha) };
  for (let seq = from; seq <= to; seq += 1) {
    const point = moved.alpha.get(seq);
    if (point) {
      moved.alpha.set(seq, { ...point, y: point.y + by });
    }
  }
  return moved;
}

// The whole chain put somewhere else entirely: a different origin and a
// quarter turn, which is what a differently deposited model looks like.
function displaced(residues) {
  const moved = new Map();
  for (const [seq, point] of residues.alpha) {
    moved.set(seq, { x: -point.y + 40, y: point.x - 17, z: point.z + 8.5 });
  }
  return { ...residues, alpha: moved };
}

function other(residues, modelId = "m2", title = "qFit model") {
  return { modelId, title, residues };
}

test("should measure the widest gap between any two models at each residue", () => {
  // given -- three models, two of which agree; the third bends 6-9 aside
  const base = chain(20);
  const agreement = chainAgreement(base, [
    other(chain(20), "m2", "A"),
    other(bend(chain(20), 6, 9, 1.2), "m3", "B"),
  ]);

  // then -- every model counts, this one included
  assert.equal(agreement.models, 3);
  assert.equal(agreement.placed.get(6), 3);
  // the gap is the widest pair, not an average over pairs
  assert.ok(Math.abs(agreement.disagreement.get(7) - 1.2) < 0.2);
  assert.ok(agreement.disagreement.get(1) < 0.2);
  assert.equal(agreement.worst.value, agreement.disagreement.get(agreement.worst.seq));
});

test("should compare shape rather than position, whatever frame the file is in", () => {
  // given -- the same chain, rotated and translated somewhere far away
  const agreement = chainAgreement(chain(20), [other(displaced(chain(20)))]);

  // then -- the fit absorbs the move, so nothing reads as disagreement
  assert.ok(agreement.fitted[0].rmsd < 1e-6);
  for (const value of agreement.disagreement.values()) {
    assert.ok(value < 1e-6);
  }
  assert.deepEqual(agreement.regions, []);
});

test("should name a run over the floor, and say which model stands apart in it", () => {
  // given -- four models; one of them builds 6-10 a long way off
  const base = chain(20);
  const agreement = chainAgreement(base, [
    other(chain(20), "m2", "Rerefined"),
    other(chain(20), "m3", "Ensemble"),
    other(bend(chain(20), 6, 10, 1.4), "m4", "qFit model"),
  ]);

  // then -- one region, covering the run and nothing else
  assert.equal(agreement.regions.length, 1);
  const [region] = agreement.regions;
  assert.equal(region.start, 6);
  assert.equal(region.end, 10);
  assert.ok(region.peak.value > DISAGREEMENT_FLOOR);
  assert.ok(region.peak.seq >= 6 && region.peak.seq <= 10);

  // the model that moved is the one furthest from where the others average out
  assert.equal(region.standouts[0].title, "qFit model");
  assert.ok(region.standouts[0].value > region.standouts[1].value);

  // and with no helix or strand over it, it is called a loop
  assert.equal(region.loop, true);
  assert.deepEqual(region.split, []);
});

test("should not name two residues a region", () => {
  // given -- a bend only two residues wide
  const agreement = chainAgreement(chain(20), [
    other(bend(chain(20), 9, 10, 1.4)),
  ]);

  // then -- it is on the chart, but it is not somewhere to send anyone
  assert.ok(agreement.disagreement.get(9) > DISAGREEMENT_FLOOR);
  assert.deepEqual(agreement.regions, []);
});

test("should not call a run inside a helix a loop", () => {
  // given
  const base = chain(20, { helices: [{ start: 4, end: 12 }] });
  const agreement = chainAgreement(base, [
    other(bend(chain(20), 6, 10, 1.4)),
  ]);

  // then
  assert.equal(agreement.regions[0].loop, false);
});

test("should say which models split residues inside a region", () => {
  // given -- the moving model also modelled 7 in two conformations
  const agreement = chainAgreement(chain(20, { alternates: [15] }), [
    other(bend(chain(20, { alternates: [7] }), 6, 10, 1.4)),
  ]);

  // then -- named in the region, and collected for the chain as a whole
  assert.deepEqual(
    agreement.regions[0].split.map((model) => model.title),
    ["qFit model"],
  );
  assert.deepEqual([...agreement.alternates].sort((a, b) => a - b), [7, 15]);
  assert.deepEqual(
    agreement.alternatesByModel.map((row) => [row.title, row.positions]),
    [["qFit model", [7]]],
  );
});

test("should say which residues exist in one model and not the other", () => {
  // given -- we skip 5-6, they skip 15
  const agreement = chainAgreement(chain(20, { skip: [5, 6] }), [
    other(chain(20, { skip: [15] })),
  ]);

  // then
  assert.deepEqual([...agreement.onlyOthers].sort((a, b) => a - b), [5, 6]);
  assert.deepEqual([...agreement.onlyMine], [15]);
  // a residue only one model places has no gap to report
  assert.equal(agreement.disagreement.has(15), false);
  assert.equal(agreement.placed.get(15), 1);
});

test("should count a model it cannot line up instead of comparing it anyway", () => {
  // given -- two alpha carbons in common is not a superposition
  const agreement = chainAgreement(chain(20), [other(chain(2))]);

  // then
  assert.equal(agreement.fitted.length, 0);
  assert.equal(agreement.unfitted, 1);
  assert.equal(agreement.models, 1);
  assert.equal(agreement.disagreement.size, 0);
});

test("should have nothing to say when there is no other model", () => {
  assert.equal(chainAgreement(chain(20), []), null);
});

test("should match the same chain across models, and refuse to guess", () => {
  const a = chain(5, { entityId: "1", chainId: "A" });
  const b = chain(5, { entityId: "1", chainId: "B" });
  const lone = chain(5, { entityId: "2", chainId: "X" });

  // exact match on entity and chain
  assert.equal(chainCounterpart([a, b, lone], "1", "B"), b);
  // an entity with one chain named differently is still that chain
  assert.equal(chainCounterpart([a, b, lone], "2", "Z"), lone);
  // two chains and no match: comparing A with B would be worse than nothing
  assert.equal(chainCounterpart([a, b], "1", "C"), null);
  assert.equal(chainCounterpart([a, b], "9", "A"), null);
});

test("should keep the floor a fixed number of angstroms", () => {
  // Backbone noise is a couple of tenths; half an angstrom is clear of it.
  assert.equal(DISAGREEMENT_FLOOR, 0.5);
});
