require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { superpose, apply, distance } = require("../src/lib/superpose.ts");

// A rotation of 37 degrees about an arbitrary axis, plus a shift. Anything the
// fit is worth it has to undo exactly.
function moved(points) {
  const angle = (37 * Math.PI) / 180;
  const [ax, ay, az] = normalise([0.3, -0.7, 0.65]);
  const c = Math.cos(angle);
  const s = Math.sin(angle);
  return points.map((p) => {
    const dot = ax * p.x + ay * p.y + az * p.z;
    return {
      x: p.x * c + (ay * p.z - az * p.y) * s + ax * dot * (1 - c) + 12.5,
      y: p.y * c + (az * p.x - ax * p.z) * s + ay * dot * (1 - c) - 4,
      z: p.z * c + (ax * p.y - ay * p.x) * s + az * dot * (1 - c) + 30.25,
    };
  });
}

function normalise(v) {
  const length = Math.hypot(...v);
  return v.map((value) => value / length);
}

const CLOUD = [
  { x: 1, y: 0, z: 0 },
  { x: 0, y: 2, z: 1 },
  { x: -3, y: 1, z: 4 },
  { x: 2, y: -5, z: 2 },
  { x: 6, y: 1, z: -2 },
  { x: -1, y: -1, z: -3 },
];

test("should undo a rotation and a translation exactly", () => {
  // given -- the same points, rotated and shifted somewhere else
  const target = moved(CLOUD);

  // when
  const fit = superpose(CLOUD, target);

  // then
  assert.ok(fit.rmsd < 1e-9, `rmsd ${fit.rmsd}`);
  assert.equal(fit.count, 6);
  for (let index = 0; index < CLOUD.length; index += 1) {
    assert.ok(distance(apply(CLOUD[index], fit), target[index]) < 1e-9);
  }
});

test("should leave two identical sets where they are", () => {
  // given
  const fit = superpose(CLOUD, CLOUD);

  // then -- the fit is the identity, so every point lands on itself
  assert.ok(fit.rmsd < 1e-12);
  for (const point of CLOUD) {
    assert.ok(distance(apply(point, fit), point) < 1e-12);
  }
});

test("should report the residual when the two sets are not the same shape", () => {
  // given -- one point pulled 3 A out of place before the whole set is moved
  const bent = CLOUD.map((p, index) =>
    index === 2 ? { ...p, x: p.x + 3 } : p,
  );
  const target = moved(bent);

  // when
  const fit = superpose(CLOUD, target);

  // then -- the fit absorbs what it can and the rest shows up as rmsd, and the
  // bent residue is the one that ends up far from its pair
  assert.ok(fit.rmsd > 0.5 && fit.rmsd < 3, `rmsd ${fit.rmsd}`);
  const gaps = CLOUD.map((point, index) =>
    distance(apply(point, fit), target[index]),
  );
  assert.equal(gaps.indexOf(Math.max(...gaps)), 2);
});

test("should refuse a fit with fewer than three pairs, and mismatched lists", () => {
  assert.equal(superpose(CLOUD.slice(0, 2), CLOUD.slice(0, 2)), null);
  assert.equal(superpose(CLOUD, CLOUD.slice(0, 4)), null);
});
