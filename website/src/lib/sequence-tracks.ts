import {
  chainAgreement,
  chainCounterpart,
  type ChainAgreement,
  type OtherChain,
} from "@/lib/model-agreement";
import type { PolymerEntityView } from "@/lib/polymer-entities";
import type { ResidueData } from "@/lib/api/entries";
import {
  chainsOfEntity,
  sequenceShift,
  shiftResidues,
  type Span,
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
  /** The whole of the row's name on one line: "Secondary structure". Counts
   *  and ranges are not part of it -- they are in the features' own
   *  tooltips. */
  label: string;
  kind: "span" | "point" | "level";
  /**
   * What a full-height feature stands for, on a row that draws a quantity.
   *
   * Present only on the rows that need an axis, and the row is drawn tall when
   * it is there: a profile in angstroms is unreadable without the number its
   * height is a share of, and there is no room for that number in a 12px lane.
   */
  scale?: { max: number; unit: string };
  /**
   * How the row was arrived at, for the tooltip on its name.
   *
   * On the tooltip rather than on the page: a row drawn from a superposition
   * of several files needs that said somewhere, and a line of prose over the
   * board is a line every reader pays for so that the few who ask the question
   * can have an answer.
   */
  hint?: string;
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
 * The first comes from the entry and never changes: the depositor's
 * substitutions. The rest are read out of the selected model's coordinates and
 * therefore *do* change with the rail -- which is the point of the whole page.
 * Two models of one crystal share a sequence and differ in exactly these
 * rows.
 *
 * Rows are only drawn when the entry actually carries their values. RCSB
 * validation metrics stored on the polymer chain therefore sit on the same
 * ruler as the rows read from the coordinate file.
 */
export function sequenceTracks(
  entity: PolymerEntityView,
  structure?: StructureResidues | null,
  agreement?: ChainAgreement | null,
): SequenceTrack[] {
  const length = sequenceLength(entity);
  if (length === 0) {
    return [];
  }

  // No chain row: the chain is chosen above the viewer and its own row is the
  // sequence, which the panel draws from the residue letters.
  const tracks: SequenceTrack[] = [];

  // No UniProt row: a reference records an accession and where it came from,
  // and nothing about extent -- no unp_begin/unp_end, no seq_begin/seq_end, on
  // either side of the API. The row used to draw a coverage bar by pulling the
  // first "24-333" out of the depositor's free-text construct line, which
  // borrowed a length from UniProt numbering and anchored it at residue 1. The
  // accession is on the entity's line in Macromolecules, where it needs no
  // position; here it would only be a bar across the whole sequence, saying
  // nothing. It comes back when the record carries the spans.

  const mutations = mutationPositions(entity.mutations, length);
  if (mutations.length > 0) {
    tracks.push({
      key: "mutations",
      label: "Mutations",
      kind: "point",
      features: mutations,
    });
  }

  const residueTracks = residueMetricTracks(entity.residueData, length);
  return structure
    ? [
        ...tracks,
        ...coordinateTracks(
          structure,
          length,
          agreement ?? null,
          residueTracks,
        ),
      ]
    : [...tracks, ...residueTracks];
}

/** The rows read out of this model's coordinate file, and the other models'. */
function coordinateTracks(
  structure: StructureResidues,
  length: number,
  agreement: ChainAgreement | null,
  residueTracks: SequenceTrack[],
): SequenceTrack[] {
  const tracks: SequenceTrack[] = [];


  if (structure.helices.length > 0 || structure.strands.length > 0) {
    tracks.push({
      key: "secondary",
      label: "Secondary structure",
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
      kind: "span",
      features: missing.map((span, index) => ({
        key: `u${index}`,
        start: span.start,
        end: span.end,
        title: `No atoms modelled for residues ${span.start}-${span.end}`,
      })),
    });
  }

  // Coverage, this model against the others: which residues exist in one file
  // and not the other. A difference here is not a small one -- it is one model
  // claiming to know where a stretch of chain goes and another declining to
  // say -- and until now the page could only show the gaps in the file it
  // happened to be on.
  if (agreement !== null) {
    for (const [key, label, positions, note] of [
      [
        "onlyOthers",
        "Only in others",
        agreement.onlyOthers,
        "modelled by another model of this entry and not by this one",
      ],
      [
        "onlyMine",
        "Only in this model",
        agreement.onlyMine,
        "modelled here and by no other model of this entry",
      ],
    ] as [string, string, Set<number>, string][]) {
      const spans = spansOf(positions, length);
      if (spans.length === 0) {
        continue;
      }
      tracks.push({
        key,
        label,
        kind: "span",
        features: spans.map((span, index) => ({
          key: `${key}${index}`,
          start: span.start,
          end: span.end,
          title: `Residues ${span.start}-${span.end}: ${note}`,
        })),
      });
    }
  }

  if (structure.alternates.size > 0) {
    tracks.push({
      key: "alternates",
      label: agreement === null ? "Alt conformers" : "Alt · this model",
      kind: "point",
      features: conformerRuns(structure.alternates, length, "a"),
    });
  }

  // A row per model rather than one row for all of them: the whole question
  // about an alternate conformation is WHO put it there. One model splitting a
  // residue its neighbours leave alone is that model's own claim about the
  // density, and it is invisible in a merged row. Models with no alternates
  // get no row, so this stays one line on most entries and only grows on the
  // ones where it is the story.
  for (const other of agreement?.alternatesByModel ?? []) {
    tracks.push({
      key: `alt:${other.modelId}`,
      label: `Alt · ${other.title}`,
      kind: "point",
      features: conformerRuns(
        new Set(other.positions),
        length,
        "a",
        other.title,
      ),
    });
  }

  const storedBFactor = residueTracks.find((track) => track.key === "bfactor");
  if (storedBFactor) {
    tracks.push(storedBFactor);
  } else if (structure.bFactor.size > 0) {
    tracks.push({
      key: "bfactor",
      label: "B-factor",
      kind: "level",
      features: bLevels(structure.bFactor),
    });
  }

  tracks.push(...residueTracks.filter((track) => track.key !== "bfactor"));

  // Last, and the tall one. It is the only row here that is about more than
  // one model, and it is read against the rows above it -- a peak over a run
  // the chain marks unobserved, or over a residue one model split in two, is
  // a different finding each time -- so it belongs on the same ruler as them
  // rather than in a chart of its own above the board.
  if (agreement !== null) {
    const row = disagreementTrack(agreement, length);
    if (row !== null) {
      tracks.push(row);
    }
  }

  return tracks;
}

function disagreementTrack(
  agreement: ChainAgreement,
  length: number,
): SequenceTrack | null {
  if (agreement.worst === null || agreement.disagreement.size === 0) {
    return null;
  }

  const flagged = new Set<number>();
  for (const region of agreement.regions) {
    for (let seq = region.start; seq <= region.end; seq += 1) {
      flagged.add(seq);
    }
  }

  const max = niceCeiling(agreement.worst.value);
  const features: SequenceFeature[] = [];
  for (const [seq, value] of [...agreement.disagreement.entries()].sort(
    (a, b) => a[0] - b[0],
  )) {
    if (seq < 1 || seq > length) {
      continue;
    }
    features.push({
      key: `g${seq}`,
      start: seq,
      end: seq,
      level: value / max,
      variant: flagged.has(seq) ? "flagged" : undefined,
      title:
        `Residue ${seq}: the models place it ${value.toFixed(2)} Å apart` +
        (agreement.alternates.has(seq)
          ? ", and at least one of them modelled it in more than one conformation"
          : ""),
    });
  }

  return {
    key: "disagreement",
    // A noun, like every other row's name, and one that says what is measured:
    // backbone, not side chains. The bracket says between what, which is the
    // one thing a reader cannot get from the row itself -- it does not fit on
    // one line of the label column, and this is the row tall enough to take
    // two. The space inside the bracket is non-breaking so the wrap lands
    // between the name and the bracket rather than inside it.
    label: "Backbone spread (between models)",
    kind: "level",
    scale: { max, unit: "Å" },
    features,
  };
}

// A round number for the axis, so the height of a bar can be read off it
// rather than guessed from the tallest one.
function niceCeiling(value: number): number {
  const steps = [0.5, 1, 2, 3, 5, 10, 20, 50];
  return steps.find((step) => value <= step) ?? Math.ceil(value);
}

/**
 * Split residues as the runs they form, not as loose residues.
 *
 * A multiconformer model splits three quarters of a chain, and fifteen
 * neighbouring single-residue marks are drawn as one unbroken bar. Clicking
 * that bar then held one residue of it -- a mark a fraction of the width of
 * the thing that was clicked, which reads as the feature being broken rather
 * than as the row being a row of residues. What looks like one block is one
 * block.
 */
function conformerRuns(
  positions: Set<number>,
  length: number,
  prefix: string,
  model?: string,
): SequenceFeature[] {
  const who = model === undefined ? "" : `${model}: `;
  return spansOf(positions, length).map((span, index) => ({
    key: `${prefix}${index}`,
    start: span.start,
    end: span.end,
    title:
      span.start === span.end
        ? `${who}residue ${span.start} modelled in more than one conformation`
        : `${who}residues ${span.start}-${span.end} modelled in more than one conformation`,
  }));
}

/** Scattered residue numbers as the runs they form. */
function spansOf(positions: Set<number>, length: number): Span[] {
  const sorted = [...positions]
    .filter((seq) => seq >= 1 && seq <= length)
    .sort((a, b) => a - b);
  const spans: Span[] = [];
  for (const seq of sorted) {
    const last = spans[spans.length - 1];
    if (last && seq === last.end + 1) {
      last.end = seq;
      continue;
    }
    spans.push({ start: seq, end: seq });
  }
  return spans;
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

type ResidueMetric = {
  key: "bfactor" | "occupancy" | "rscc" | "conformerCount" | "rmsf";
  label: string;
  value: (residue: ResidueData) => number | undefined;
  format: (value: number) => string;
  level: (value: number, values: number[]) => number;
};

const RESIDUE_METRICS: ResidueMetric[] = [
  {
    key: "bfactor",
    label: "B-factor",
    value: (residue) => residue.b_iso,
    format: (value) => `${value.toFixed(2)} Å²`,
    level: rangedLevel,
  },
  {
    key: "occupancy",
    label: "Average occupancy",
    value: (residue) => residue.occupancy,
    format: (value) => value.toFixed(3),
    level: boundedLevel,
  },
  {
    key: "rscc",
    label: "RSCC",
    value: (residue) => residue.rscc,
    format: (value) => value.toFixed(3),
    level: boundedLevel,
  },
  {
    key: "conformerCount",
    label: "Conformer count",
    value: (residue) => residue.conformer_count,
    format: (value) => String(value),
    level: zeroBasedLevel,
  },
  {
    key: "rmsf",
    label: "RMSF",
    value: (residue) => residue.rmsf,
    format: (value) => `${value.toFixed(3)} Å`,
    level: zeroBasedLevel,
  },
];

function residueMetricTracks(
  residues: ResidueData[],
  length: number,
): SequenceTrack[] {
  return RESIDUE_METRICS.flatMap((metric) => {
    const measured = residues
      .map((residue) => ({ residue, value: metric.value(residue) }))
      .filter(
        (item): item is { residue: ResidueData; value: number } =>
          item.value !== undefined &&
          Number.isFinite(item.value) &&
          item.residue.label_seq_id >= 1 &&
          item.residue.label_seq_id <= length,
      );
    if (measured.length === 0) {
      return [];
    }

    const values = measured.map((item) => item.value);
    return [
      {
        key: metric.key,
        label: metric.label,
        kind: "level" as const,
        features: measured.map(({ residue, value }) => ({
          key: `${metric.key}${residue.label_seq_id}`,
          start: residue.label_seq_id,
          end: residue.label_seq_id,
          level: metric.level(value, values),
          title: residueMetricTitle(residue, metric.label, metric.format(value)),
        })),
      },
    ];
  });
}

function residueMetricTitle(
  residue: ResidueData,
  label: string,
  value: string,
): string {
  const component = residue.label_comp_id
    ? `${residue.label_comp_id} `
    : "Residue ";
  const authorPosition = residue.auth_seq_id
    ? ` [auth ${residue.auth_seq_id}${residue.pdbx_pdb_ins_code ?? ""}]`
    : "";
  const chain = residue.auth_asym_id ?? residue.label_asym_id;
  return `${label}: ${value} | ${component}${residue.label_seq_id}${authorPosition} | Chain ${chain}`;
}

function boundedLevel(value: number): number {
  return Math.max(0, Math.min(1, value));
}

function zeroBasedLevel(value: number, values: number[]): number {
  const high = Math.max(...values);
  return high <= 0 ? 0 : Math.max(0, value) / high;
}

function rangedLevel(value: number, values: number[]): number {
  const low = Math.min(...values);
  const high = Math.max(...values);
  return (value - low) / (high - low || 1);
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
  /** What the comparison with the entry's other models came to, for the line
   *  that says how the disagreement rows were measured. Null when there was
   *  nothing to compare with. */
  agreement: ChainAgreement | null;
  tracks: SequenceTrack[];
};

/** One other model of the entry, already read. */
export type OtherModelCoordinates = {
  modelId: string;
  title: string;
  /** Every chain of that model, as its own file gives them. */
  chains: StructureResidues[];
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
  others: OtherModelCoordinates[] = [],
): SequenceChain[] {
  const chains: SequenceChain[] = [];

  for (const entity of entities) {
    const length = sequenceLength(entity);
    if (length === 0) {
      continue;
    }

    const modelled = chainsOfEntity(
      coordinates,
      entity.entityId,
      entity.chains,
    );
    if (modelled.length > 0) {
      for (const residues of modelled) {
        chains.push(
          chainView(entity, residues.chainId || null, residues, length, others),
        );
      }
      continue;
    }

    if (entity.chains.length > 0) {
      for (const chainId of entity.chains) {
        chains.push(chainView(entity, chainId, null, length, others));
      }
      continue;
    }

    chains.push(chainView(entity, null, null, length, others));
  }

  return chains;
}

function chainView(
  entity: PolymerEntityView,
  chainId: string | null,
  residues: StructureResidues | null,
  length: number,
  others: OtherModelCoordinates[],
): SequenceChain {
  // Every file is moved onto the entry's sequence before anything is drawn or
  // compared. Each of them is numbered however its own program saw fit, and
  // this is the one coordinate they all have in common -- the same one the
  // ruler above the rows is drawn in.
  const sequence = residueLetters(entity);
  const aligned = residues === null ? null : onSequence(residues, sequence);
  // Only a chain we actually read coordinates for can be compared: the rows
  // are differences between two sets of atoms, and a chain we know only from
  // the entry's sequence has none.
  const agreement =
    aligned === null
      ? null
      : chainAgreement(aligned, counterparts(aligned, others, sequence));
  return {
    key: `${entity.key}:${chainId ?? ""}`,
    chainId,
    entityId: entity.entityId,
    moleculeName: entity.name,
    organism: entity.organisms[0]?.scientific_name ?? null,
    length,
    sequence,
    modelled: residues !== null,
    agreement,
    tracks: sequenceTracks(entity, aligned, agreement),
  };
}

function onSequence(
  residues: StructureResidues,
  sequence: string | null,
): StructureResidues {
  return shiftResidues(residues, sequenceShift(residues, sequence));
}

// A model that does not contain this chain at all is left out rather than
// counted as disagreeing with it.
function counterparts(
  residues: StructureResidues,
  others: OtherModelCoordinates[],
  sequence: string | null,
): OtherChain[] {
  return others.flatMap((other) => {
    const match = chainCounterpart(
      other.chains,
      residues.entityId,
      residues.chainId,
    );
    return match
      ? [
          {
            modelId: other.modelId,
            title: other.title,
            residues: onSequence(match, sequence),
          },
        ]
      : [];
  });
}
