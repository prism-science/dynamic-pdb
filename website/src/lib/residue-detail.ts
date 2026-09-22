import type { ResidueData } from "@/lib/api/entries";
import type { ChainAgreement } from "@/lib/model-agreement";
import type { Span, StructureResidues } from "@/lib/structure-tracks";

/**
 * Everything one residue of one chain is known to be, gathered for the panel
 * that opens when a residue is held.
 *
 * The rows of the board answer "where along the chain"; none of them answers
 * "what is this residue". How many conformations this model gave it and at
 * what occupancy, what the refinement measured on it, how far the other models
 * put it from here -- all of that is in the record and in the coordinate file
 * already, and until now it was either on the tooltip of a metric row (so it
 * vanished on a residue that had no metrics) or nowhere.
 *
 * A field with nothing in it is left out, and a group whose fields are all
 * empty is left out with it. The panel was built the other way round at first
 * -- every field always drawn, an em dash and a sentence saying why it was
 * empty -- on the theory that a stable shape is easier to read. It is not: a
 * column of dashes and apologies is a column a reader has to work through to
 * find the two numbers that are there.
 */
export type ResidueFact = {
  key: string;
  /** What the field is, in the words a reader of proteins has. */
  label: string;
  /**
   * The name the schema gives it, for the label's tooltip.
   *
   * A depositor querying the API needs that exact string; a crystallographer
   * looking at a residue should not have to read `pdbx_pdb_ins_code` to find
   * out the residue has an insertion code. Both, in that order.
   */
  field?: string;
  value: string;
  /** What qualifies the value, on the row's tooltip. */
  note?: string;
};

export type ResidueGroup = {
  key: string;
  title: string;
  facts: ResidueFact[];
};

export type ResidueDetail = {
  /** Position in the entry's sequence, which is what the board is drawn in. */
  position: number;
  /** "ILE 52", or the one-letter code when the file names no residue. */
  title: string;
  groups: ResidueGroup[];
};

/**
 * One chain, seen the way the inspector needs it.
 *
 * Passed in rather than reached for, so the assembly can be tested without a
 * coordinate file and a browser.
 */
export type ResidueContext = {
  /** One-letter codes, in the entry's own numbering. */
  sequence: string | null;
  /** This model's coordinates for the chain, already moved onto the entry's
   *  sequence -- which is also the numbering `position` is in. */
  structure: StructureResidues | null;
  /** The entry's stored per-residue records for this chain. */
  residueData: ResidueData[];
  agreement: ChainAgreement | null;
};

/** A fact before the empty ones are dropped. */
type Draft = Omit<ResidueFact, "value"> & { value: string | null };

export function residueDetail(
  context: ResidueContext,
  position: number,
): ResidueDetail | null {
  if (!Number.isInteger(position) || position < 1) {
    return null;
  }
  const { structure, agreement } = context;
  const stored = context.residueData.find(
    (residue) => residue.label_seq_id === position,
  );
  const letter = context.sequence?.[position - 1] ?? null;
  const name = structure?.names.get(position) ?? stored?.label_comp_id ?? null;

  // A residue this model left out has nothing under the heading, and the
  // heading is the answer: the panel opens on its name alone.
  return {
    position,
    title: name ? `${name} ${position}` : `${letter ?? "Residue"} ${position}`,
    groups: [
      thisModel(structure, position, stored),
      validation(stored),
      betweenModels(agreement, position),
    ]
      .map((group) => ({ ...group, facts: filled(group.facts) }))
      .filter((group) => group.facts.length > 0),
  };
}

function filled(drafts: Draft[]): ResidueFact[] {
  return drafts.filter((draft): draft is ResidueFact => draft.value !== null);
}

function thisModel(
  structure: StructureResidues | null,
  position: number,
  stored: ResidueData | undefined,
): { key: string; title: string; facts: Draft[] } {
  const facts: Draft[] = [
    {
      key: "secondary",
      label: "Secondary structure",
      field: "_struct_conf / _struct_sheet_range",
      value: secondaryStructure(structure, position),
    },
    {
      key: "conformer_count",
      label: "Conformations",
      field: "conformer_count",
      value: conformerCount(structure, position, stored),
    },
  ];

  for (const conformer of structure?.conformers.get(position) ?? []) {
    facts.push({
      key: `alt-${conformer.id}`,
      label: `Conformer ${conformer.id}`,
      field: "label_alt_id · occupancy",
      value:
        conformer.occupancy === null
          ? "occupancy not stated"
          : `occupancy ${conformer.occupancy.toFixed(2)}`,
    });
  }

  const bFactor = structure?.bFactor.get(position) ?? stored?.b_iso ?? null;
  facts.push({
    key: "b_iso",
    label: "B-factor",
    field: "b_iso",
    value: bFactor === null ? null : `${bFactor.toFixed(1)} Å²`,
  });

  facts.push({
    key: "occupancy",
    label: "Occupancy",
    field: "occupancy",
    value:
      stored?.occupancy === undefined ? null : stored.occupancy.toFixed(2),
    note: "as the entry records it, over the whole residue",
  });

  const rmsf = structure?.rmsf.get(position) ?? stored?.rmsf ?? null;
  facts.push({
    key: "rmsf",
    label: "RMSF",
    field: "rmsf",
    value: rmsf === null ? null : `${rmsf.toFixed(2)} Å`,
    note: "across the members of this ensemble",
  });

  return {
    key: "model",
    title: "Model",
    facts,
  };
}

function conformerCount(
  structure: StructureResidues | null,
  position: number,
  stored: ResidueData | undefined,
): string | null {
  const count =
    structure?.conformerCount.get(position) ?? stored?.conformer_count ?? null;
  return count === null ? null : String(count);
}

function secondaryStructure(
  structure: StructureResidues | null,
  position: number,
): string | null {
  if (structure === null) {
    return null;
  }
  const kinds: [string, Span[]][] = [
    ["Helix", structure.helices],
    ["Strand", structure.strands],
    ["Turn", structure.turns],
    ["Bend", structure.bends],
  ];
  for (const [name, spans] of kinds) {
    const span = spans.find(
      (item) => position >= item.start && position <= item.end,
    );
    if (span) {
      return `${name} ${span.start}–${span.end}`;
    }
  }
  return null;
}

function validation(stored: ResidueData | undefined): {
  key: string;
  title: string;
  facts: Draft[];
} {
  return {
    key: "validation",
    title: "Validation",
    facts: [
      {
        key: "rscc",
        label: "RSCC",
        field: "rscc",
        value: stored?.rscc === undefined ? null : stored.rscc.toFixed(3),
      },
    ],
  };
}

function betweenModels(
  agreement: ChainAgreement | null,
  position: number,
): { key: string; title: string; facts: Draft[] } {
  const spread = agreement?.disagreement.get(position) ?? null;
  return {
    key: "between",
    title: "Between models",
    facts: [
      {
        key: "spread",
        label: "Backbone spread",
        value: spread === null ? null : `${spread.toFixed(2)} Å`,
        note: "widest gap between any two of them, C-alpha",
      },
    ],
  };
}
