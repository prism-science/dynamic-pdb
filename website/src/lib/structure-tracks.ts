import { cifNumber, cifValue, columnIndex, parseCifLoops } from "@/lib/mmcif";
import { oneLetter } from "@/lib/residue-codes";
import type { Point } from "@/lib/superpose";

/**
 * What the coordinates say about each residue of one entity.
 *
 * Everything here is read out of the model's own mmCIF, so it changes when the
 * rail changes -- which is the point: two models of the same crystal have the
 * same sequence and different coordinates, and this is where that difference
 * becomes visible.
 *
 * Positions are the file's own author residue numbers -- `auth_seq_id`, the
 * numbering a paper cites and a depositor assigns. NOT `label_seq_id`, which
 * is an index into each file's own `_entity_poly_seq` and therefore means
 * something different in every file: qFit's output for 4GS3 numbers the
 * deposit's residue 10 as residue 1, and its own numbering skips nothing where
 * the deposit skips eight residues, so no single offset relates the two. The
 * author numbering is the one thing two independently written files of the
 * same protein agree on.
 *
 * Which means these positions are not yet the positions the rows are drawn in.
 * `sequenceShift` and `shiftResidues` move a chain onto the entry's sequence,
 * and the caller does that before drawing or comparing anything.
 */
export type StructureResidues = {
  /**
   * Entity id as the coordinates label it, e.g. "1" -- empty when the file
   * does not say.
   *
   * Plenty of them do not. qFit writes `label_entity_id` as unknown, and a
   * file is not useless for that: it still names its chains and still numbers
   * its residues in the entity's own sequence, which is everything these rows
   * are drawn from. So the id is recorded when it is there and the chain is
   * matched on its author id when it is not.
   */
  entityId: string;
  /** Author chain id, the one the entity's `chains` list uses. */
  chainId: string;
  /** Residues that have at least one atom. */
  observed: Set<number>;
  /** What each residue is called, for lining this chain up against the
   *  sequence the entry records. */
  names: Map<number, string>;
  /** Mean B-factor per residue, over the atoms that carry one. */
  bFactor: Map<number, number>;
  /** Residues modelled in more than one conformation. */
  alternates: Set<number>;
  /**
   * Where this model puts each residue's alpha carbon.
   *
   * One position per residue, and the first one the file gives: mmCIF writes
   * conformer A before B and ensemble member 1 before 2, so first-wins picks
   * a single consistent copy of the chain out of a multiconformer or ensemble
   * file. Averaging them would invent a position no conformer holds, and
   * keeping them all would mean this map could not answer "where is residue
   * 66" at all.
   *
   * Backbone only, and one atom of it, because this exists to be compared
   * between models: the alpha carbon is the atom every amino acid has exactly
   * one of, so it is the one thing two models can always be lined up on.
   */
  alpha: Map<number, Point>;
  /** Helix spans, inclusive, in label_seq_id. */
  helices: Span[];
  /** Strand spans, inclusive. */
  strands: Span[];
};

export type Span = { start: number; end: number };

/**
 * Read one model's coordinates into per-chain residue facts.
 *
 * Only `_atom_site`, `_struct_conf` and `_struct_sheet_range` are touched.
 * A file missing any of them yields correspondingly fewer rows rather than an
 * error: plenty of coordinate files carry no secondary-structure records, and
 * that is not a reason to show nothing.
 */
export function readStructure(cif: string): StructureResidues[] {
  const loops = parseCifLoops(cif);
  // One row per chain, not per entity: two chains of one entity share a
  // sequence and differ in exactly the things these rows show, so merging them
  // would hide the difference the reader came for. The key is internal to this
  // function -- callers get a list and match on the fields.
  const byEntity = new Map<string, StructureResidues>();

  const atoms = loops.get("_atom_site");
  if (!atoms) {
    return [];
  }

  const entityAt = columnIndex(atoms, "label_entity_id");
  const seqAt = columnIndex(atoms, "auth_seq_id");
  const labelSeqAt = columnIndex(atoms, "label_seq_id");
  const authAt = columnIndex(atoms, "auth_asym_id");
  const labelAsymAt = columnIndex(atoms, "label_asym_id");
  const altAt = columnIndex(atoms, "label_alt_id");
  const bAt = columnIndex(atoms, "B_iso_or_equiv");
  const atomAt = columnIndex(atoms, "label_atom_id");
  const compAt = columnIndex(atoms, "label_comp_id");
  const xAt = columnIndex(atoms, "Cartn_x");
  const yAt = columnIndex(atoms, "Cartn_y");
  const zAt = columnIndex(atoms, "Cartn_z");
  const groupAt = columnIndex(atoms, "group_PDB");
  // A residue number of some kind is the one thing there is no working around.
  // The entity id is a nicety.
  if (seqAt === -1 && labelSeqAt === -1) {
    return [];
  }

  const sums = new Map<string, Map<number, { total: number; count: number }>>();
  // Secondary structure is recorded in label numbering even where the atoms
  // are read in author numbering, so the two have to be joined -- through the
  // atoms, which carry both.
  const authOfLabel = new Map<string, Map<number, number>>();

  for (const row of atoms.rows) {
    // HETATM rows are ligands, ions and waters; they carry no label_seq_id in
    // the polymer's numbering, so they cannot land on a track drawn against
    // the sequence.
    if (groupAt !== -1 && cifValue(row[groupAt]) !== "ATOM") {
      continue;
    }
    const entityId = (entityAt === -1 ? null : cifValue(row[entityAt])) ?? "";
    const labelSeq = labelSeqAt === -1 ? null : cifNumber(row[labelSeqAt]);
    const seq = (seqAt === -1 ? null : cifNumber(row[seqAt])) ?? labelSeq;
    if (seq === null) {
      continue;
    }
    const chainId =
      (authAt === -1 ? null : cifValue(row[authAt])) ??
      (labelAsymAt === -1 ? null : cifValue(row[labelAsymAt])) ??
      "";
    const key = `${entityId}:${chainId}`;
    const entity = ensure(byEntity, key, entityId, chainId);
    entity.observed.add(seq);

    if (labelSeq !== null) {
      const labels = authOfLabel.get(key) ?? new Map<number, number>();
      if (!labels.has(labelSeq)) {
        labels.set(labelSeq, seq);
      }
      authOfLabel.set(key, labels);
    }

    if (compAt !== -1 && !entity.names.has(seq)) {
      const name = cifValue(row[compAt]);
      if (name !== null) {
        entity.names.set(seq, name);
      }
    }

    if (altAt !== -1 && cifValue(row[altAt]) !== null) {
      entity.alternates.add(seq);
    }

    if (
      atomAt !== -1 &&
      xAt !== -1 &&
      !entity.alpha.has(seq) &&
      cifValue(row[atomAt]) === "CA"
    ) {
      const x = cifNumber(row[xAt]);
      const y = yAt === -1 ? null : cifNumber(row[yAt]);
      const z = zAt === -1 ? null : cifNumber(row[zAt]);
      if (x !== null && y !== null && z !== null) {
        entity.alpha.set(seq, { x, y, z });
      }
    }

    if (bAt !== -1) {
      const b = cifNumber(row[bAt]);
      if (b !== null) {
        const perEntity = sums.get(key) ?? new Map();
        const cell = perEntity.get(seq) ?? { total: 0, count: 0 };
        cell.total += b;
        cell.count += 1;
        perEntity.set(seq, cell);
        sums.set(key, perEntity);
      }
    }
  }

  for (const [key, perResidue] of sums) {
    const entity = byEntity.get(key);
    if (!entity) {
      continue;
    }
    for (const [seq, cell] of perResidue) {
      entity.bFactor.set(seq, cell.total / cell.count);
    }
  }

  // Secondary structure is recorded per chain, not per entity, so each span is
  // attributed to the entity that chain belongs to.
  const keyOfLabelChain = labelChainToKey(atoms, entityAt);
  addSpans(
    byEntity,
    loops.get("_struct_conf"),
    keyOfLabelChain,
    authOfLabel,
    "helices",
    true,
  );
  addSpans(
    byEntity,
    loops.get("_struct_sheet_range"),
    keyOfLabelChain,
    authOfLabel,
    "strands",
    false,
  );

  return [...byEntity.values()].sort(byEntityThenChain);
}

/**
 * The chains of one entity, in the order a picker should list them.
 *
 * Matched on the entity id the file gives, and failing that on the author
 * chain ids the entry lists for this entity -- which is the only handle left
 * on a file that writes its entity ids as unknown. Without the fallback such a
 * model shows no residue rows at all, which is what it used to do.
 */
export function chainsOfEntity(
  chains: StructureResidues[],
  entityId: string | null,
  authorChains: readonly string[] = [],
): StructureResidues[] {
  const named =
    entityId === null
      ? []
      : chains.filter((chain) => chain.entityId === entityId);
  if (named.length > 0) {
    return named;
  }
  const wanted = new Set(authorChains);
  return chains.filter(
    (chain) => chain.entityId === "" && wanted.has(chain.chainId),
  );
}

// Entity ids are numbers kept as text, so a plain string sort puts entity 10
// in front of entity 2.
function byEntityThenChain(
  first: StructureResidues,
  second: StructureResidues,
): number {
  if (/^\d+$/.test(first.entityId) && /^\d+$/.test(second.entityId)) {
    const order = Number(first.entityId) - Number(second.entityId);
    if (order !== 0) {
      return order;
    }
  } else if (first.entityId !== second.entityId) {
    return first.entityId.localeCompare(second.entityId);
  }
  return first.chainId.localeCompare(second.chainId);
}

function ensure(
  byEntity: Map<string, StructureResidues>,
  key: string,
  entityId: string,
  chainId: string,
): StructureResidues {
  const existing = byEntity.get(key);
  if (existing) {
    return existing;
  }
  const created: StructureResidues = {
    entityId,
    chainId,
    observed: new Set(),
    names: new Map(),
    bFactor: new Map(),
    alternates: new Set(),
    alpha: new Map(),
    helices: [],
    strands: [],
  };
  byEntity.set(key, created);
  return created;
}

// Secondary structure is recorded against label_asym_id, while the rows are
// keyed by author chain, so the two have to be joined through the atoms.
function labelChainToKey(
  atoms: { columns: string[]; rows: string[][] },
  entityAt: number,
): Map<string, string> {
  const labelAt = atoms.columns.indexOf("label_asym_id");
  const authAt = atoms.columns.indexOf("auth_asym_id");
  const map = new Map<string, string>();
  if (labelAt === -1) {
    return map;
  }
  for (const row of atoms.rows) {
    const label = cifValue(row[labelAt]);
    if (label === null || map.has(label)) {
      continue;
    }
    const entityId = (entityAt === -1 ? null : cifValue(row[entityAt])) ?? "";
    const auth = (authAt === -1 ? null : cifValue(row[authAt])) ?? label;
    map.set(label, `${entityId}:${auth}`);
  }
  return map;
}

function addSpans(
  byEntity: Map<string, StructureResidues>,
  loop: { columns: string[]; rows: string[][] } | undefined,
  keyOfLabelChain: Map<string, string>,
  authOfLabel: Map<string, Map<number, number>>,
  field: "helices" | "strands",
  helicesOnly: boolean,
): void {
  if (!loop) {
    return;
  }
  const begAt = loop.columns.indexOf("beg_label_seq_id");
  const endAt = loop.columns.indexOf("end_label_seq_id");
  const begAuthAt = loop.columns.indexOf("beg_auth_seq_id");
  const endAuthAt = loop.columns.indexOf("end_auth_seq_id");
  const chainAt = loop.columns.indexOf("beg_label_asym_id");
  const typeAt = loop.columns.indexOf("conf_type_id");
  if ((begAt === -1 || endAt === -1) && (begAuthAt === -1 || endAuthAt === -1)) {
    return;
  }

  for (const row of loop.rows) {
    // `_struct_conf` also carries turns and bends; only the helices are worth a
    // row of their own, and the rest would draw as noise along the whole chain.
    if (helicesOnly && typeAt !== -1) {
      const type = cifValue(row[typeAt]) ?? "";
      if (!type.toUpperCase().startsWith("HELX")) {
        continue;
      }
    }
    const label = chainAt === -1 ? null : cifValue(row[chainAt]);
    const key = label === null ? null : (keyOfLabelChain.get(label) ?? null);
    // With no chain to attribute it to, the span goes to the only row there is;
    // with several, it is dropped rather than drawn on the wrong one.
    const target = key ?? (byEntity.size === 1 ? [...byEntity.keys()][0] : null);
    const row_ = target === null ? null : byEntity.get(target);
    if (!row_ || target === null) {
      continue;
    }

    // Author numbering if the record carries it, and otherwise the label
    // numbering put through the chain's own label-to-author table. A span
    // neither route can place is dropped: drawn in the wrong numbering it
    // would sit on residues it says nothing about.
    const labels = authOfLabel.get(target);
    const authOf = (at: number, labelAt: number): number | null => {
      const direct = at === -1 ? null : cifNumber(row[at]);
      if (direct !== null) {
        return direct;
      }
      const own = labelAt === -1 ? null : cifNumber(row[labelAt]);
      return own === null ? null : (labels?.get(own) ?? null);
    };
    const start = authOf(begAuthAt, begAt);
    const end = authOf(endAuthAt, endAt);
    if (start === null || end === null || end < start) {
      continue;
    }
    row_[field].push({ start, end });
  }
}

/**
 * Where this chain's own residue numbering sits on the entry's sequence.
 *
 * Returns the number to add to a residue number from the file to get a
 * position in that sequence, and 0 when it cannot be worked out.
 *
 * This is not paranoia about a spec. `label_seq_id` is an index into the
 * file's own `_entity_poly_seq`, and a program that writes out only the part
 * of the chain it modelled writes a sequence that starts there -- qFit's
 * output for 4GS3 numbers the deposit's residue 10 as residue 1. Two files of
 * the same protein can therefore disagree about every residue number in them
 * while agreeing about the protein, and a comparison that trusted the numbers
 * would report the whole chain as moved by several angstroms. Lining both up
 * against the entry's sequence by residue name puts them in one coordinate,
 * which is also the coordinate the ruler is drawn in.
 *
 * Candidates come from the first residue's name: the offsets that could put it
 * anywhere in the sequence it actually occurs. Each is then scored over every
 * named residue, and the best wins only if it is convincing -- four fifths of
 * the residues have to agree, and it has to beat the runner-up. A chain that
 * does not align stays on its own numbering rather than being moved somewhere
 * arbitrary.
 */
export function sequenceShift(
  residues: StructureResidues,
  sequence: string | null,
): number {
  if (!sequence || residues.names.size === 0) {
    return 0;
  }

  const named = [...residues.names.entries()]
    .map(([seq, name]) => [seq, oneLetter(name)] as const)
    .filter((pair): pair is readonly [number, string] => pair[1] !== null)
    .sort((a, b) => a[0] - b[0]);
  if (named.length === 0) {
    return 0;
  }

  const [firstSeq, firstLetter] = named[0];
  const candidates = new Set<number>();
  for (let index = 0; index < sequence.length; index += 1) {
    if (sequence[index] === firstLetter) {
      candidates.add(index + 1 - firstSeq);
    }
  }

  let best = 0;
  let bestScore = 0;
  let runnerUp = 0;
  for (const shift of candidates) {
    let score = 0;
    for (const [seq, letter] of named) {
      if (sequence[seq + shift - 1] === letter) {
        score += 1;
      }
    }
    if (score > bestScore) {
      runnerUp = bestScore;
      bestScore = score;
      best = shift;
    } else if (score > runnerUp) {
      runnerUp = score;
    }
  }

  return bestScore >= named.length * 0.8 && bestScore > runnerUp ? best : 0;
}

/** The same chain with every residue number moved by `shift`. */
export function shiftResidues(
  residues: StructureResidues,
  shift: number,
): StructureResidues {
  if (shift === 0) {
    return residues;
  }
  const move = <T>(entries: Iterable<[number, T]>) =>
    new Map([...entries].map(([seq, value]) => [seq + shift, value] as [number, T]));
  const span = (spans: Span[]) =>
    spans.map(({ start, end }) => ({ start: start + shift, end: end + shift }));
  return {
    ...residues,
    observed: new Set([...residues.observed].map((seq) => seq + shift)),
    names: move(residues.names),
    bFactor: move(residues.bFactor),
    alternates: new Set([...residues.alternates].map((seq) => seq + shift)),
    alpha: move(residues.alpha),
    helices: span(residues.helices),
    strands: span(residues.strands),
  };
}

/** Residues of 1..length that the coordinates do not contain. */
export function unobservedSpans(
  observed: Set<number>,
  length: number,
): Span[] {
  const spans: Span[] = [];
  let start: number | null = null;
  for (let position = 1; position <= length; position += 1) {
    if (observed.has(position)) {
      if (start !== null) {
        spans.push({ start, end: position - 1 });
        start = null;
      }
    } else if (start === null) {
      start = position;
    }
  }
  if (start !== null) {
    spans.push({ start, end: length });
  }
  return spans;
}
