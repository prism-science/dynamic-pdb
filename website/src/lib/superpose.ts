/**
 * Rigid-body superposition of two sets of paired points.
 *
 * Needed because a per-residue comparison between two models is meaningless
 * until they are in the same frame. Two re-refinements of one crystal usually
 * already are, but "usually" is not something to draw a chart on: a model
 * deposited in a different origin would otherwise read as disagreeing with
 * everything, everywhere, by tens of angstroms.
 *
 * Horn's quaternion method rather than Kabsch's SVD. Both give the same fit;
 * this one needs a single eigen-decomposition of a symmetric 4x4 and cannot
 * return a reflection, so there is no determinant sign to remember to fix.
 */

/** A position in a coordinate file's own frame, in angstroms. */
export type Point = { x: number; y: number; z: number };

export type Superposition = {
  /** Row-major 3x3, applied about `from`. */
  rotation: number[];
  /** Centroid of the mobile set: subtracted before rotating. */
  from: Point;
  /** Centroid of the target set: added after rotating. */
  to: Point;
  /** Root-mean-square deviation over the pairs it was fitted on. */
  rmsd: number;
  /** Pairs the fit rests on. */
  count: number;
  /** Pairs left out of it as outliers. */
  excluded: number;
};

/**
 * The transform that best lays `mobile` over `target`, pair by pair.
 *
 * Null under three pairs: two points fix a line and one fixes a point, and a
 * fit with a free axis of rotation is not a fit. The two lists must be the
 * same length and paired by index -- the caller is the one that knows what
 * makes two points correspond.
 */
export function superpose(
  mobile: Point[],
  target: Point[],
): Superposition | null {
  if (mobile.length !== target.length || mobile.length < 3) {
    return null;
  }

  const from = centroid(mobile);
  const to = centroid(target);

  // S[a][b] = sum over pairs of (mobile_a * target_b), both centred.
  const s = [
    [0, 0, 0],
    [0, 0, 0],
    [0, 0, 0],
  ];
  for (let index = 0; index < mobile.length; index += 1) {
    const p = [
      mobile[index].x - from.x,
      mobile[index].y - from.y,
      mobile[index].z - from.z,
    ];
    const q = [
      target[index].x - to.x,
      target[index].y - to.y,
      target[index].z - to.z,
    ];
    for (let a = 0; a < 3; a += 1) {
      for (let b = 0; b < 3; b += 1) {
        s[a][b] += p[a] * q[b];
      }
    }
  }

  const [[xx, xy, xz], [yx, yy, yz], [zx, zy, zz]] = s;
  const k = [
    [xx + yy + zz, yz - zy, zx - xz, xy - yx],
    [yz - zy, xx - yy - zz, xy + yx, zx + xz],
    [zx - xz, xy + yx, -xx + yy - zz, yz + zy],
    [xy - yx, zx + xz, yz + zy, -xx - yy + zz],
  ];

  const { values, vectors } = eigenSymmetric(k);
  let best = 0;
  for (let index = 1; index < values.length; index += 1) {
    if (values[index] > values[best]) {
      best = index;
    }
  }
  const rotation = rotationOf(vectors.map((row) => row[best]));

  const fit: Superposition = {
    rotation,
    from,
    to,
    rmsd: 0,
    count: mobile.length,
    excluded: 0,
  };
  let total = 0;
  for (let index = 0; index < mobile.length; index += 1) {
    total += squaredDistance(apply(mobile[index], fit), target[index]);
  }
  fit.rmsd = Math.sqrt(total / mobile.length);
  return fit;
}

/**
 * The same fit, made on the part of the structure that actually corresponds.
 *
 * A fit over every pair is dragged by the pairs that disagree. Give it one
 * region built differently in the two models and it tilts the whole molecule
 * to split the difference: the region it should have shouted about comes out
 * halved, and the rest of the chain -- which agreed -- picks up the other
 * half. A per-residue chart drawn on that is wrong in both directions at once,
 * and wrong most where it matters.
 *
 * So the fit is refined the way structure-alignment programs do it: fit, throw
 * out the pairs that sit far outside the rest, fit again.
 *
 * The cutoff comes from the deviations themselves, through the median and the
 * median absolute deviation rather than the mean and the standard deviation.
 * That matters: a region built differently is exactly the thing that inflates
 * a standard deviation, so a mean-based cutoff is widened by the outliers
 * until it admits them -- a loop over a fifth of a chain survives its own
 * rejection test. The median barely moves.
 *
 * It never goes below half an angstrom either. On two models that agree
 * everywhere the spread is a couple of hundredths, and rejecting noise as
 * though it were disagreement would leave the fit resting on a handful of
 * residues that happened to round the same way.
 *
 * At least two fifths of the pairs are kept whatever happens. A fit on the
 * best-agreeing tenth of a chain is a fit on nothing.
 */
export function superposeCore(
  mobile: Point[],
  target: Point[],
  rounds = 5,
): Superposition | null {
  let kept = mobile.map((_, index) => index);
  let fit = superpose(mobile, target);
  if (fit === null) {
    return null;
  }

  const floor = Math.max(3, Math.ceil(mobile.length * 0.4));

  for (let round = 0; round < rounds; round += 1) {
    const gaps = kept.map((index) =>
      distance(apply(mobile[index], fit as Superposition), target[index]),
    );
    const middle = median(gaps);
    // 1.4826 is what turns a median absolute deviation into the standard
    // deviation of a normal distribution, which is what makes "two sigma"
    // mean the same thing here as it does anywhere else.
    const sigma = 1.4826 * median(gaps.map((gap) => Math.abs(gap - middle)));
    const cutoff = Math.max(0.5, middle + 2 * sigma);

    const next = kept.filter((index, at) => gaps[at] <= cutoff);
    if (next.length === kept.length || next.length < floor) {
      break;
    }
    const refit = superpose(
      next.map((index) => mobile[index]),
      next.map((index) => target[index]),
    );
    if (refit === null) {
      break;
    }
    kept = next;
    fit = refit;
  }

  return { ...fit, count: kept.length, excluded: mobile.length - kept.length };
}

/** One point moved into the target's frame. */
export function apply(point: Point, fit: Superposition): Point {
  const x = point.x - fit.from.x;
  const y = point.y - fit.from.y;
  const z = point.z - fit.from.z;
  const r = fit.rotation;
  return {
    x: r[0] * x + r[1] * y + r[2] * z + fit.to.x,
    y: r[3] * x + r[4] * y + r[5] * z + fit.to.y,
    z: r[6] * x + r[7] * y + r[8] * z + fit.to.z,
  };
}

function median(values: number[]): number {
  const sorted = [...values].sort((first, second) => first - second);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? (sorted[middle - 1] + sorted[middle]) / 2
    : sorted[middle];
}

export function distance(a: Point, b: Point): number {
  return Math.sqrt(squaredDistance(a, b));
}

export function centroid(points: Point[]): Point {
  let x = 0;
  let y = 0;
  let z = 0;
  for (const point of points) {
    x += point.x;
    y += point.y;
    z += point.z;
  }
  const count = points.length || 1;
  return { x: x / count, y: y / count, z: z / count };
}

function squaredDistance(a: Point, b: Point): number {
  const dx = a.x - b.x;
  const dy = a.y - b.y;
  const dz = a.z - b.z;
  return dx * dx + dy * dy + dz * dz;
}

// A unit quaternion (w, x, y, z) as a row-major rotation matrix.
function rotationOf(quaternion: number[]): number[] {
  const length =
    Math.hypot(quaternion[0], quaternion[1], quaternion[2], quaternion[3]) || 1;
  const [w, x, y, z] = quaternion.map((value) => value / length);
  return [
    1 - 2 * (y * y + z * z),
    2 * (x * y - w * z),
    2 * (x * z + w * y),
    2 * (x * y + w * z),
    1 - 2 * (x * x + z * z),
    2 * (y * z - w * x),
    2 * (x * z - w * y),
    2 * (y * z + w * x),
    1 - 2 * (x * x + y * y),
  ];
}

/**
 * Eigenvalues and eigenvectors of a small symmetric matrix, by cyclic Jacobi.
 *
 * `vectors[row][column]` is one component of the eigenvector belonging to
 * `values[column]` -- the eigenvectors are the columns, which is the shape the
 * caller wants when it picks the largest eigenvalue's.
 *
 * Jacobi rather than anything cleverer because the matrix here is 4x4 and
 * always symmetric: it converges in a handful of sweeps, needs no library, and
 * cannot return complex values.
 */
function eigenSymmetric(matrix: number[][]): {
  values: number[];
  vectors: number[][];
} {
  const n = matrix.length;
  const a = matrix.map((row) => [...row]);
  const v: number[][] = matrix.map((_, row) =>
    matrix.map((__, column): number => (row === column ? 1 : 0)),
  );

  for (let sweep = 0; sweep < 60; sweep += 1) {
    let off = 0;
    for (let p = 0; p < n - 1; p += 1) {
      for (let q = p + 1; q < n; q += 1) {
        off += a[p][q] * a[p][q];
      }
    }
    if (off < 1e-20) {
      break;
    }

    for (let p = 0; p < n - 1; p += 1) {
      for (let q = p + 1; q < n; q += 1) {
        if (Math.abs(a[p][q]) < 1e-18) {
          continue;
        }
        const theta = (a[q][q] - a[p][p]) / (2 * a[p][q]);
        const sign = theta >= 0 ? 1 : -1;
        const t = sign / (Math.abs(theta) + Math.sqrt(theta * theta + 1));
        const c = 1 / Math.sqrt(t * t + 1);
        const s = t * c;

        for (let i = 0; i < n; i += 1) {
          const aip = a[i][p];
          const aiq = a[i][q];
          a[i][p] = c * aip - s * aiq;
          a[i][q] = s * aip + c * aiq;
        }
        for (let i = 0; i < n; i += 1) {
          const api = a[p][i];
          const aqi = a[q][i];
          a[p][i] = c * api - s * aqi;
          a[q][i] = s * api + c * aqi;
        }
        for (let i = 0; i < n; i += 1) {
          const vip = v[i][p];
          const viq = v[i][q];
          v[i][p] = c * vip - s * viq;
          v[i][q] = s * vip + c * viq;
        }
      }
    }
  }

  return { values: a.map((row, index) => row[index]), vectors: v };
}
