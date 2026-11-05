import type { CreateEntityInput, CreateModelInput } from "@/lib/api/entries";

/** What the PDB archive and the deposited file say about a model to add from a PDB ID. */
export type ArchiveModelLookup = {
  pdbId: string;
  title: string;
  coordinateFileURL: string;
  structureFactorsURL: string | null;
  metadata: {
    authors: string[];
    details?: string;
    affiliation?: string;
    atom_count?: number;
    modeled_residues?: number;
    unique_protein_chains?: number;
    unmodeled_fraction?: number;
    altloc_fraction?: number;
    ligands: string[];
  };
  /** Whole-model states the file holds; null when it holds one, or was too long to read to the end. */
  memberCount: number | null;
  metrics: {
    key: string;
    value: number;
    software_name?: string;
    software_version?: string;
  }[];
  relatedEntries: {
    db_name: string;
    db_id: string;
    content_type: string;
    details?: string;
  }[];
};

export const RE_REFINEMENT = "re-refinement";

/** The PDB IDs the archive says this deposition re-refines. */
export function reRefinementTargets(relatedEntries: ArchiveModelLookup["relatedEntries"]): string[] {
  return relatedEntries
    .filter((entry) => entry.content_type === RE_REFINEMENT && entry.db_name === "PDB")
    .map((entry) => entry.db_id.toUpperCase());
}

/** The file's own evidence of a type, as the default the depositor may change. */
export function suggestedModelType(lookup: ArchiveModelLookup): string {
  if (lookup.memberCount !== null) {
    return "Ensemble";
  }
  return (lookup.metadata.altloc_fraction ?? 0) > 0 ? "Multiconformer" : "Single Conformer";
}

/** The request for a model that is an unchanged deposition of this PDB ID. */
export function buildArchiveModelInput(
  lookup: ArchiveModelLookup,
  choices: { modelType: string; purpose: string },
): CreateModelInput {
  const name = lookup.pdbId.toLowerCase();
  const entities: CreateEntityInput[] = [
    {
      id: crypto.randomUUID(),
      type: "model",
      artifact_type: "model",
      level: "L2",
      name: `${name}.cif`,
      payload: { file_url: lookup.coordinateFileURL, type: "cif" },
    },
  ];
  if (lookup.structureFactorsURL) {
    entities.push({
      id: crypto.randomUUID(),
      type: "data",
      artifact_type: "structure_factors",
      level: "L1",
      name: `${name}-sf.cif`,
      payload: { file_url: lookup.structureFactorsURL, type: "structure_factors_cif" },
    });
  }
  for (const metric of lookup.metrics) {
    entities.push({
      id: crypto.randomUUID(),
      type: "metrics",
      level: "L3",
      name: "Metrics",
      payload: {
        [metric.key]: metric.value,
        ...(metric.software_name ? { software_name: metric.software_name } : {}),
        ...(metric.software_version ? { software_version: metric.software_version } : {}),
      },
    });
  }
  return {
    title: lookup.title,
    entities,
    metadata: {
      ...lookup.metadata,
      model_type: choices.modelType,
      purpose: choices.purpose,
      origin: "archive",
      external_refs: { pdb: lookup.pdbId },
      ...(lookup.relatedEntries.length > 0 ? { related_entries: lookup.relatedEntries } : {}),
      ...(choices.modelType === "Ensemble" && lookup.memberCount !== null
        ? { member_count: lookup.memberCount }
        : {}),
    },
  };
}
