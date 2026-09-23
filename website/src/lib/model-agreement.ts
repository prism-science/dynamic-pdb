import type { StructureResidues } from "@/lib/structure-tracks";
import {
  apply,
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

/**
 * A run of residues the models place differently, over the floor and long
 * enough to be a run rather than a rotamer.
 *
 * A range and nothing else. It used to carry a peak, the model that stood
 * furthest out and who had split the residues, all of which went into a card
 * of generated prose under the board; the card said less than the row above it
 * and named runs after the wrong decade ("120s loop" for residues 128-130), so
 * it is gone. What is left is what the row itself uses: which columns to paint
 * as a finding rather than as noise.
 */
export type DisagreementRegion = {
  start: number;
  end: number;
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
    regions: regionsOf(disagreement),
    alternates,
  };
}

/**
 * The runs the disagreement row paints as a finding rather than as noise.
 *
 * A run has to clear the floor and be at least a few residues long: one
 * residue over the line is a rotamer or a rounding, and two residues are not a
 * run of anything.
 */
function regionsOf(disagreement: Map<number, number>): DisagreementRegion[] {
  const runs: DisagreementRegion[] = [];
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
  return runs.filter((run) => run.end - run.start + 1 >= MIN_REGION);
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
