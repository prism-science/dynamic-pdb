import type { PolymerEntityView } from "@/lib/polymer-entities";
import {
  chainsOfEntity,
  type StructureResidues,
  unobservedSpans,
} from "@/lib/structure-tracks";

/**
 * One row of the feature viewer: a name on the left and coloured spans along
 * the sequence on the right.
 *
 * Positions are 1-based residue numbers in the entity's own sequence, which is
 * the coordinate every row is drawn in -- the same convention RCSB's viewer
 * uses, so a range read here means the same thing as a range read there.
 */
export type SequenceTrack = {
  key: string;
  label: string;
  /** Printed under the label, e.g. the UniProt accession the row is about. */
  caption: string | null;
  kind: "span" | "point" | "level";
  features: SequenceFeature[];
};

export type SequenceFeature = {
  key: string;
  start: number;
  end: number;
  title: string;
  /** 0..1, for rows that draw a value rather than a presence. */
  level?: number;
  /** Tells apart features that share a row, e.g. a helix from a strand. */
  variant?: string;
};

/**
 * The rows we can fill, in the order they are read.
 *
 * The first two come from the entry and never change: the UniProt mapping and
 * the depositor's substitutions. The rest are read out of the selected model's
 * coordinates and therefore *do* change with the rail -- which is the point of
 * the whole page. Two models of one crystal share a sequence and differ in
 * exactly these rows.
 *
 * Of RCSB's fifteen we cannot draw the ones that come from the wwPDB
 * validation report -- RSRZ, RSR, RSCC per residue, clashes, plane outliers --
 * because we hold that report as a PDF, not as data. Hydropathy and disorder
 * are predictions from third-party services we do not call.
 */
export function sequenceTracks(
  entity: PolymerEntityView,
  structure?: StructureResidues | null,
): SequenceTrack[] {
  const length = sequenceLength(entity);
  if (length === 0) {
    return [];
  }

  // No chain row: the chain is chosen above the viewer and its own row is the
  // sequence, which the panel draws from the residue letters.
  const tracks: SequenceTrack[] = [];

  for (const mapping of entity.uniprotMappings) {
    // The construct line is free text from the depositor; when it names a
    // range, that range is in UniProt numbering, not ours. Only its length is
    // comparable, so the bar spans what it covers of this sequence and the
    // caption keeps the numbers as the depositor wrote them.
    const range = constructRange(entity.construct);
    const covered = range ? Math.min(range.end - range.start + 1, length) : length;
    tracks.push({
      key: `uniprot-${mapping.accession}`,
      label: "UniProt",
      caption: mapping.accession,
      kind: "span",
      features: [
        {
          key: mapping.accession,
          start: 1,
          end: covered,
          title: range
            ? `${mapping.accession} · UNP residues ${range.start}-${range.end}`
            : mapping.accession,
        },
      ],
    });
  }

  const mutations = mutationPositions(entity.mutations, length);
  if (mutations.length > 0) {
    tracks.push({
      key: "mutations",
      label: "Mutations",
      caption: `${mutations.length}`,
      kind: "point",
      features: mutations,
    });
  }

  return structure ? [...tracks, ...coordinateTracks(structure, length)] : tracks;
}

/** The rows read out of this model's coordinate file. */
function coordinateTracks(
  structure: StructureResidues,
  length: number,
): SequenceTrack[] {
  const tracks: SequenceTrack[] = [];

  if (structure.helices.length > 0 || structure.strands.length > 0) {
    tracks.push({
      key: "secondary",
      label: "Secondary structure",
      caption: `${structure.helices.length}H / ${structure.strands.length}E`,
      kind: "span",
      features: [
        ...structure.helices.map((span, index) => ({
          key: `h${index}`,
          start: span.start,
          end: span.end,
          title: `Helix ${span.start}-${span.end}`,
          variant: "helix",
        })),
        ...structure.strands.map((span, index) => ({
          key: `e${index}`,
          start: span.start,
          end: span.end,
          title: `Strand ${span.start}-${span.end}`,
          variant: "strand",
        })),
      ],
    });
  }

  // Only worth a row when something is actually missing: a full-length "all
  // observed" bar says nothing that the chain row did not already say.
  const missing = unobservedSpans(structure.observed, length);
  if (missing.length > 0) {
    tracks.push({
      key: "unobserved",
      label: "Unobserved",
      caption: `${missing.reduce((n, s) => n + (s.end - s.start + 1), 0)}`,
      kind: "span",
      features: missing.map((span, index) => ({
        key: `u${index}`,
        start: span.start,
        end: span.end,
        title: `No atoms modelled for residues ${span.start}-${span.end}`,
      })),
    });
  }

  if (structure.alternates.size > 0) {
    tracks.push({
      key: "alternates",
      label: "Alt conformers",
      caption: `${structure.alternates.size}`,
      kind: "point",
      features: [...structure.alternates].sort((a, b) => a - b).map((seq) => ({
        key: `a${seq}`,
        start: seq,
        end: seq,
        title: `Residue ${seq} modelled in more than one conformation`,
      })),
    });
  }

  if (structure.bFactor.size > 0) {
    tracks.push({
      key: "bfactor",
      label: "B-factor",
      caption: bRange(structure.bFactor),
      kind: "level",
      features: bLevels(structure.bFactor),
    });
  }

  return tracks;
}

// Scaled against this model's own range rather than an absolute one: B-factors
// are only comparable within a structure, and a fixed scale would flatten a
// well-ordered model into a blank row.
function bLevels(bFactor: Map<number, number>): SequenceFeature[] {
  const values = [...bFactor.values()];
  const low = Math.min(...values);
  const high = Math.max(...values);
  const span = high - low || 1;
  return [...bFactor.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([seq, value]) => ({
      key: `b${seq}`,
      start: seq,
      end: seq,
      title: `Residue ${seq}: B ${value.toFixed(1)}`,
      level: (value - low) / span,
    }));
}

function bRange(bFactor: Map<number, number>): string {
  const values = [...bFactor.values()];
  return `${Math.min(...values).toFixed(0)}–${Math.max(...values).toFixed(0)} Å²`;
}

export function sequenceLength(entity: PolymerEntityView): number {
  const residues = residueLetters(entity);
  if (residues !== null) {
    return residues.length;
  }
  return entity.residues ?? 0;
}

/** The stored sequence as one-letter codes with the file's wrapping removed,
 *  which is what the viewer draws a residue at a time. */
export function residueLetters(entity: PolymerEntityView): string | null {
  const letters = entity.sequence?.replace(/\s+/g, "");
  return letters ? letters : null;
}

// "UNP residues 24-333" and the handful of ways depositors write the same
// thing. Anything else is left alone: a wrong range drawn confidently is worse
// than no range.
function constructRange(
  construct: string | null,
): { start: number; end: number } | null {
  if (!construct) {
    return null;
  }
  const match = /(\d+)\s*[-–]\s*(\d+)/.exec(construct);
  if (!match) {
    return null;
  }
  const start = Number(match[1]);
  const end = Number(match[2]);
  return end > start ? { start, end } : null;
}

// "A123G" -- the position is the digits in the middle. A substitution outside
// the sequence is dropped rather than clamped to its edge, where it would
// claim a residue it does not describe.
function mutationPositions(
  mutations: string[] | null,
  length: number,
): SequenceFeature[] {
  if (!mutations) {
    return [];
  }
  return mutations.flatMap((token) => {
    const match = /^([A-Za-z]{1,3})(\d+)([A-Za-z]{1,3})$/.exec(token);
    if (!match) {
      return [];
    }
    const position = Number(match[2]);
    if (position < 1 || position > length) {
      return [];
    }
    return [{ key: token, start: position, end: position, title: token }];
  });
}

/**
 * Ruler ticks: round numbers along the sequence, ending at its length.
 *
 * The step is chosen so the ruler never carries more labels than it has room
 * for -- a ruler crowded with numbers is read as a texture, not a scale. How
 * many that is depends on how wide the viewer is drawn, which is why the
 * caller passes it: fitted to the column it is about a dozen, zoomed in to
 * one letter per residue it is far more.
 */
export function rulerTicks(length: number, maxLabels = 12): number[] {
  if (length <= 0) {
    return [];
  }
  const steps = [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000];
  const step =
    steps.find((value) => length / value <= Math.max(maxLabels, 2)) ?? 2000;
  // The last label is always the length itself, so a tick that would land
  // under it is dropped rather than printed on top of it.
  const ticks: number[] = [];
  for (let position = step; position < length - step / 2; position += step) {
    ticks.push(position);
  }
  return ticks;
}


/**
 * One selectable chain of the viewer: an entity's sequence, seen through one
 * of the chains that carries it.
 *
 * Two chains of one entity hold the same residues and differ in their
 * coordinates, so each gets its own entry with its own rows -- which is what
 * the chain picker moves between.
 */
export type SequenceChain = {
  key: string;
  /** Author chain id, e.g. "A". Null when nothing names a chain. */
  chainId: string | null;
  entityId: string | null;
  moleculeName: string;
  organism: string | null;
  length: number;
  /** One-letter residue codes, when the entry stores the sequence. */
  sequence: string | null;
  /** Whether this chain's rows were read from the model's coordinates, as
   *  opposed to only from what the entry records. */
  modelled: boolean;
  tracks: SequenceTrack[];
};

/**
 * The chains the picker offers, in the order it lists them.
 *
 * The chains of the selected model come first choice: they are the ones this
 * model actually contains, and they are the only ones with coordinate rows to
 * show. An entity the coordinates say nothing about -- an unreadable file, or
 * a chain this model leaves out -- falls back to the chains the entry names,
 * which still carry the sequence and the entry's own rows.
 */
export function sequenceChains(
  entities: PolymerEntityView[],
  coordinates: StructureResidues[],
): SequenceChain[] {
  const chains: SequenceChain[] = [];

  for (const entity of entities) {
    const length = sequenceLength(entity);
    if (length === 0) {
      continue;
    }

    const modelled = chainsOfEntity(coordinates, entity.entityId);
    if (modelled.length > 0) {
      for (const residues of modelled) {
        chains.push(
          chainView(entity, residues.chainId || null, residues, length),
        );
      }
      continue;
    }

    if (entity.chains.length > 0) {
      for (const chainId of entity.chains) {
        chains.push(chainView(entity, chainId, null, length));
      }
      continue;
    }

    chains.push(chainView(entity, null, null, length));
  }

  return chains;
}

function chainView(
  entity: PolymerEntityView,
  chainId: string | null,
  residues: StructureResidues | null,
  length: number,
): SequenceChain {
  return {
    key: `${entity.key}:${chainId ?? ""}`,
    chainId,
    entityId: entity.entityId,
    moleculeName: entity.name,
    organism: entity.organisms[0]?.scientific_name ?? null,
    length,
    sequence: residueLetters(entity),
    modelled: residues !== null,
    tracks: sequenceTracks(entity, residues),
  };
}
