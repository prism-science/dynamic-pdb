import type { StructureResidues } from "@/lib/structure-tracks";
import {
  apply,
  centroid,
  distance,
  superposeCore,
  type Point,
} from "@/lib/superpose";

/**
 * Where the models of one entry disagree, residue by residue.
 *
 * The per-residue level of the comparison the rest of the page makes in
 * numbers, and the thing the feedback actually asked for: a metric says one
 * model is better, this says *where* they differ, which is the part you can go
 * and look at in the density.
 *
 * It is a statement about the entry, not about the model on screen. The
 * question is "where do these models disagree", so every model that can be
 * placed in one frame counts, including this one, and the figure at each
 * residue is the widest gap between any two of them.
 *
 * All of it is computed in the browser out of the coordinate files. Nothing
 * per-residue is stored on our side, so every model here costs a download and
 * a parse, and the caller decides how many of those it will pay for.
 */

/**
 * The gap, in angstroms, at which two models are placing a residue somewhere
 * different rather than agreeing to within the noise.
 *
 * Backbone coordinates from a normal refinement are good to a couple of tenths
 * of an angstrom, and two re-refinements of one crystal differ by about that
 * much everywhere without disagreeing about anything. Half an angstrom is
 * clear of that and short of anything anyone would call a conformational
 * change, so it is where a run of residues starts being worth naming.
 *
 * It only decides which runs get called out. The chart draws whatever is
 * there, against a labelled axis, so a chain that agrees everywhere still
 * shows how much it agrees by.
 */
export const DISAGREEMENT_FLOOR = 0.5;

/** Shorter than this is a residue or two, not a region. */
const MIN_REGION = 3;

/** One other model's take on the same chain. */
export type OtherChain = {
  modelId: string;
  title: string;
  residues: StructureResidues;
};

export type FittedChain = {
  modelId: string;
  title: string;
  /** How well the whole chain lines up once superposed, in angstroms. */
  rmsd: number;
  /** Alpha carbons the fit rests on. */
  matched: number;
  /** Alpha carbons left out of it as outliers -- which is to say, the ones
   *  the two models place differently. */
  excluded: number;
};

export type Peak = { seq: number; value: number };

/** One model, and how far out of line it sits over some stretch of chain. */
export type ModelStandout = {
  modelId: string;
  title: string;
  /** Mean distance from where the models average out, over the region. */
  value: number;
};

/**
 * A run of residues the models place differently.
 *
 * Named and described because a chart of 165 columns does not tell anyone what
 * to do next. A region with a range, a number and a sentence does: it says go
 * and look at residues 60 to 72, these two models are the ones that disagree,
 * and this one split them into two conformations.
 */
export type DisagreementRegion = {
  start: number;
  end: number;
  /** The widest gap anywhere in the region, and where it falls. */
  peak: Peak;
  /** Models ordered by how far out they sit here, furthest first. */
  standouts: ModelStandout[];
  /** Models that modelled more than one conformation inside the region. */
  split: { modelId: string; title: string }[];
  /** No helix or strand of the model on screen covers this run, so it is
   *  loop -- which is where this kind of difference usually lives, and worth
   *  saying because it changes how surprising the difference is. */
  loop: boolean;
};

export type ChainAgreement = {
  /** The other models this chain could be lined up with. */
  fitted: FittedChain[];
  /** Other models whose chain could not be lined up at all. */
  unfitted: number;
  /** How many models are in the comparison, this one included. */
  models: number;
  /** The widest gap between any two models at each residue, in angstroms. */
  disagreement: Map<number, number>;
  /** How many models were placed at each residue. */
  placed: Map<number, number>;
  /** The worst residue on the chain, for the chart's axis and its caption. */
  worst: Peak | null;
  /** The runs worth naming, longest-standing first. */
  regions: DisagreementRegion[];
  /** Residues any model of the entry modelled in more than one conformation. */
  alternates: Set<number>;
  /** The same, per model, for the rows that say who. */
  alternatesByModel: { modelId: string; title: string; positions: number[] }[];
};

/**
 * The same chain in another model.
 *
 * Matched on entity and author chain id, which is what re-refinements of one
 * deposit keep. Where one of the two files does not say which entity its
 * chains belong to -- qFit writes the id as unknown -- the author chain id
 * stands on its own, since those are unique within a file. Failing both, the
 * entity's one and only chain.
 *
 * Never one of several candidates: picking by position would quietly compare
 * chain A of this model with chain B of that one and draw the difference
 * between two copies of the same molecule as disagreement.
 */
export function chainCounterpart(
  chains: StructureResidues[],
  entityId: string,
  chainId: string,
): StructureResidues | null {
  const exact = chains.find(
    (chain) => chain.entityId === entityId && chain.chainId === chainId,
  );
  if (exact) {
    return exact;
  }
  const named = chains.filter(
    (chain) =>
      chain.chainId === chainId && (chain.entityId === "" || entityId === ""),
  );
  if (named.length === 1) {
    return named[0];
  }
  const sameEntity = chains.filter((chain) => chain.entityId === entityId);
  return sameEntity.length === 1 ? sameEntity[0] : null;
}

/**
 * Put every model of the entry in one frame and see where they part company.
 *
 * Each other model is superposed onto this one on the alpha carbons the two
 * share, so what comes out is a difference in shape rather than a difference
 * in where the file happens to sit in space.
 *
 * Null when there is nothing to say: no other model, or no backbone in this
 * one to compare against.
 */
export function chainAgreement(
  base: StructureResidues,
  others: OtherChain[],
): ChainAgreement | null {
  if (others.length === 0) {
    return null;
  }

  const fitted: FittedChain[] = [];
  // Every model's alpha carbons in this model's frame, this model first.
  const frames: Map<number, Point>[] = [base.alpha];
  let unfitted = 0;

  for (const other of others) {
    const shared = [...base.alpha.keys()].filter((seq) =>
      other.residues.alpha.has(seq),
    );
    const fit =
      shared.length < 3
        ? null
        : superposeCore(
            shared.map((seq) => other.residues.alpha.get(seq) as Point),
            shared.map((seq) => base.alpha.get(seq) as Point),
          );
    if (fit === null) {
      unfitted += 1;
      continue;
    }
    fitted.push({
      modelId: other.modelId,
      title: other.title,
      rmsd: fit.rmsd,
      matched: fit.count,
      excluded: fit.excluded,
    });
    const here = new Map<number, Point>();
    for (const [seq, point] of other.residues.alpha) {
      here.set(seq, apply(point, fit));
    }
    frames.push(here);
  }

  // Named in the same order as `frames`, so a standout can be traced back to
  // the model it belongs to. The model on screen has no name of its own here:
  // the page is standing on it, and "this model" is what a reader calls it.
  const names = [
    { modelId: "", title: "this model" },
    ...fitted.map((fit) => ({ modelId: fit.modelId, title: fit.title })),
  ];

  const disagreement = new Map<number, number>();
  const placed = new Map<number, number>();
  const seqs = [...new Set(frames.flatMap((frame) => [...frame.keys()]))].sort(
    (a, b) => a - b,
  );

  for (const seq of seqs) {
    const here = frames.map((frame) => frame.get(seq));
    const present = here.filter((point): point is Point => point !== undefined);
    placed.set(seq, present.length);
    if (present.length > 1) {
      disagreement.set(seq, widestGap(present));
    }
  }

  const alternates = new Set<number>(base.alternates);
  for (const other of others) {
    for (const seq of other.residues.alternates) {
      alternates.add(seq);
    }
  }

  return {
    fitted,
    unfitted,
    models: frames.length,
    disagreement,
    placed,
    worst: peak(disagreement),
    regions: regionsOf(disagreement, frames, names, base, others),
    alternates,
    alternatesByModel: others
      .filter((other) => other.residues.alternates.size > 0)
      .map((other) => ({
        modelId: other.modelId,
        title: other.title,
        positions: [...other.residues.alternates].sort((a, b) => a - b),
      })),
  };
}

/**
 * The runs of residues worth naming, and what to say about each.
 *
 * A run has to clear the floor and be at least a few residues long: one
 * residue over the line is a rotamer or a rounding, and a region that turns
 * out to be two residues wide is not somewhere to send anyone.
 */
function regionsOf(
  disagreement: Map<number, number>,
  frames: Map<number, Point>[],
  names: { modelId: string; title: string }[],
  base: StructureResidues,
  others: OtherChain[],
): DisagreementRegion[] {
  const runs: { start: number; end: number }[] = [];
  for (const seq of [...disagreement.keys()].sort((a, b) => a - b)) {
    if ((disagreement.get(seq) ?? 0) < DISAGREEMENT_FLOOR) {
      continue;
    }
    const last = runs[runs.length - 1];
    if (last && seq === last.end + 1) {
      last.end = seq;
      continue;
    }
    runs.push({ start: seq, end: seq });
  }

  const structured = [...base.helices, ...base.strands];

  return runs
    .filter((run) => run.end - run.start + 1 >= MIN_REGION)
    .map((run) => {
      let worst: Peak = { seq: run.start, value: 0 };
      for (let seq = run.start; seq <= run.end; seq += 1) {
        const value = disagreement.get(seq) ?? 0;
        if (value > worst.value) {
          worst = { seq, value };
        }
      }

      return {
        start: run.start,
        end: run.end,
        peak: worst,
        standouts: standoutsIn(run, frames, names),
        split: [
          ...(overlaps(base.alternates, run)
            ? [{ modelId: "", title: "this model" }]
            : []),
          ...others
            .filter((other) => overlaps(other.residues.alternates, run))
            .map((other) => ({ modelId: other.modelId, title: other.title })),
        ],
        loop: !structured.some(
          (span) => span.start <= run.end && span.end >= run.start,
        ),
      };
    })
    .sort((first, second) => second.peak.value - first.peak.value);
}

// How far each model sits from where the models average out, over the region.
// Against the average of all of them rather than against this model, because
// the region belongs to the entry: the answer to "who is the odd one out" must
// not change when the reader switches models.
function standoutsIn(
  run: { start: number; end: number },
  frames: Map<number, Point>[],
  names: { modelId: string; title: string }[],
): ModelStandout[] {
  const totals = frames.map(() => ({ total: 0, count: 0 }));
  for (let seq = run.start; seq <= run.end; seq += 1) {
    const here = frames.map((frame) => frame.get(seq));
    const present = here.filter((point): point is Point => point !== undefined);
    if (present.length < 2) {
      continue;
    }
    const middle = centroid(present);
    here.forEach((point, index) => {
      if (point === undefined) {
        return;
      }
      totals[index].total += distance(point, middle);
      totals[index].count += 1;
    });
  }
  return totals
    .map((cell, index) => ({
      modelId: names[index]?.modelId ?? "",
      title: names[index]?.title ?? "a model",
      value: cell.count === 0 ? 0 : cell.total / cell.count,
    }))
    .filter((row) => row.value > 0)
    .sort((first, second) => second.value - first.value);
}

function overlaps(positions: Set<number>, run: { start: number; end: number }) {
  for (const seq of positions) {
    if (seq >= run.start && seq <= run.end) {
      return true;
    }
  }
  return false;
}

// The widest gap between any two of them, which is what "how far apart are
// these models here" means: an average over pairs hides one model standing
// somewhere else entirely as soon as three others agree.
function widestGap(points: Point[]): number {
  let widest = 0;
  for (let first = 0; first < points.length - 1; first += 1) {
    for (let second = first + 1; second < points.length; second += 1) {
      const gap = distance(points[first], points[second]);
      if (gap > widest) {
        widest = gap;
      }
    }
  }
  return widest;
}

function peak(values: Map<number, number>): Peak | null {
  let found: Peak | null = null;
  for (const [seq, value] of values) {
    if (found === null || value > found.value) {
      found = { seq, value };
    }
  }
  return found;
}
