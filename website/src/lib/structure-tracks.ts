import { cifNumber, cifValue, columnIndex, parseCifLoops } from "@/lib/mmcif";

/**
 * What the coordinates say about each residue of one entity.
 *
 * Everything here is read out of the model's own mmCIF, so it changes when the
 * rail changes -- which is the point: two models of the same crystal have the
 * same sequence and different coordinates, and this is where that difference
 * becomes visible.
 *
 * Positions are `label_seq_id`, the residue's index in the entity's sequence.
 * That is the same coordinate the sequence tracks use, so the rows line up.
 */
export type StructureResidues = {
  /** Entity id as the coordinates label it, e.g. "1". */
  entityId: string;
  /** Author chain id, the one the entity's `chains` list uses. */
  chainId: string;
  /** Residues that have at least one atom. */
  observed: Set<number>;
  /** Mean B-factor per residue, over the atoms that carry one. */
  bFactor: Map<number, number>;
  /** Residues modelled in more than one conformation. */
  alternates: Set<number>;
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
  const seqAt = columnIndex(atoms, "label_seq_id");
  const authAt = columnIndex(atoms, "auth_asym_id");
  const labelAsymAt = columnIndex(atoms, "label_asym_id");
  const altAt = columnIndex(atoms, "label_alt_id");
  const bAt = columnIndex(atoms, "B_iso_or_equiv");
  const groupAt = columnIndex(atoms, "group_PDB");
  if (entityAt === -1 || seqAt === -1) {
    return [];
  }

  const sums = new Map<string, Map<number, { total: number; count: number }>>();

  for (const row of atoms.rows) {
    // HETATM rows are ligands, ions and waters; they carry no label_seq_id in
    // the polymer's numbering, so they cannot land on a track drawn against
    // the sequence.
    if (groupAt !== -1 && cifValue(row[groupAt]) !== "ATOM") {
      continue;
    }
    const entityId = cifValue(row[entityAt]);
    const seq = cifNumber(row[seqAt]);
    if (entityId === null || seq === null) {
      continue;
    }
    const chainId =
      (authAt === -1 ? null : cifValue(row[authAt])) ??
      (labelAsymAt === -1 ? null : cifValue(row[labelAsymAt])) ??
      "";
    const key = `${entityId}:${chainId}`;
    const entity = ensure(byEntity, key, entityId, chainId);
    entity.observed.add(seq);

    if (altAt !== -1 && cifValue(row[altAt]) !== null) {
      entity.alternates.add(seq);
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
  addSpans(byEntity, loops.get("_struct_conf"), keyOfLabelChain, "helices", true);
  addSpans(
    byEntity,
    loops.get("_struct_sheet_range"),
    keyOfLabelChain,
    "strands",
    false,
  );

  return [...byEntity.values()].sort(byEntityThenChain);
}

/** The chains of one entity, in the order a picker should list them. */
export function chainsOfEntity(
  chains: StructureResidues[],
  entityId: string | null,
): StructureResidues[] {
  if (entityId === null) {
    return [];
  }
  return chains.filter((chain) => chain.entityId === entityId);
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
    bFactor: new Map(),
    alternates: new Set(),
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
    const entityId = cifValue(row[entityAt]);
    if (label === null || entityId === null || map.has(label)) {
      continue;
    }
    const auth = (authAt === -1 ? null : cifValue(row[authAt])) ?? label;
    map.set(label, `${entityId}:${auth}`);
  }
  return map;
}

function addSpans(
  byEntity: Map<string, StructureResidues>,
  loop: { columns: string[]; rows: string[][] } | undefined,
  keyOfLabelChain: Map<string, string>,
  field: "helices" | "strands",
  helicesOnly: boolean,
): void {
  if (!loop) {
    return;
  }
  const begAt = loop.columns.indexOf("beg_label_seq_id");
  const endAt = loop.columns.indexOf("end_label_seq_id");
  const chainAt = loop.columns.indexOf("beg_label_asym_id");
  const typeAt = loop.columns.indexOf("conf_type_id");
  if (begAt === -1 || endAt === -1) {
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
    const start = cifNumber(row[begAt]);
    const end = cifNumber(row[endAt]);
    if (start === null || end === null || end < start) {
      continue;
    }
    const label = chainAt === -1 ? null : cifValue(row[chainAt]);
    const key = label === null ? null : (keyOfLabelChain.get(label) ?? null);
    // With no chain to attribute it to, the span goes to the only row there is;
    // with several, it is dropped rather than drawn on the wrong one.
    const target = key ?? (byEntity.size === 1 ? [...byEntity.keys()][0] : null);
    const row_ = target === null ? null : byEntity.get(target);
    if (!row_) {
      continue;
    }
    row_[field].push({ start, end });
  }
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
