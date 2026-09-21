import type {
  PolymerEntity,
  PolymerEntityOrganism,
  PolymerEntityUniProtMapping,
  PolymerEntityUniProtSource,
  ProteinSequence,
  ResidueData,
} from "@/lib/api/entries";

/** A polymer entity joined with the stored sequence it describes. */
export type PolymerEntityView = {
  key: string;
  proteinSequenceId: string | null;
  entityId: string | null;
  name: string;
  chains: string[];
  residues: number | null;
  sequence: string | null;
  /** The artifact the matched FASTA record came from: the file that holds this
   *  entity's chains, and the only thing that can be linked to from a chain. */
  sequenceArtifactId: string | null;
  organisms: PolymerEntityOrganism[];
  construct: string | null;
  /** Single substitutions, when the field holds a plain list of them. Null for
   *  anything else -- the field is free text from the depositor -- and then
   *  mutationsText is shown as written. */
  mutations: string[] | null;
  mutationsText: string | null;
  uniprotMappings: PolymerEntityUniProtMapping[];
  residueData: ResidueData[];
};

export function polymerEntityViews(
  entities: PolymerEntity[],
  sequences: ProteinSequence[],
): PolymerEntityView[] {
  return entities
    .map((entity, index) => {
      const entityId = trimmed(entity.label_entity_id);
      const proteinSequenceId = trimmed(entity.protein_sequence_id);
      const sequence = sequenceForEntity(proteinSequenceId, entityId, sequences);
      const mutationsText = trimmed(entity.mutations);
      const residues = sequence ? residueCount(sequence.sequence) : null;
      const chain = chainForEntity(entity);

      return {
        key: entity.id || entityId || String(index),
        proteinSequenceId,
        entityId,
        name:
          trimmed(entity.description) ??
          (entityId ? `Entity ${entityId}` : "Polymer entity"),
        chains:
          chain === null
            ? sequence
              ? chainsFromHeader(sequence.header)
              : []
            : [chain],
        residues,
        sequence: sequence?.sequence ?? null,
        sequenceArtifactId: sequence?.source_artifact_id ?? null,
        organisms: entity.source_organisms ?? [],
        construct: trimmed(entity.construct),
        mutations: mutationsText ? mutationTokens(mutationsText) : null,
        mutationsText,
        uniprotMappings: entity.uniprot_mappings ?? [],
        residueData: entity.residue_data ?? [],
      };
    })
    .sort(byEntityId);
}

/** Chains that share one stored sequence are one group. Legacy records without
 *  that foreign key fall back to UniProt accession and then molecule name. */
export type EntityGroup = {
  key: string;
  name: string;
  chains: string[];
  residues: number | null;
  organisms: PolymerEntityOrganism[];
  entityCount: number;
};

export function groupedPolymerEntities(
  views: PolymerEntityView[],
): EntityGroup[] {
  const groups = new Map<string, EntityGroup>();

  for (const view of views) {
    const key =
      view.proteinSequenceId ??
      view.uniprotMappings[0]?.accession.toLowerCase() ??
      view.name.toLowerCase();
    const group = groups.get(key);

    if (!group) {
      groups.set(key, {
        key,
        name: view.name,
        chains: [...view.chains],
        residues: view.residues,
        organisms: [...view.organisms],
        entityCount: 1,
      });
      continue;
    }

    group.chains.push(...view.chains);
    group.entityCount += 1;
    if (view.residues !== null) {
      group.residues = (group.residues ?? 0) + view.residues;
    }
    for (const organism of view.organisms) {
      const known = group.organisms.some(
        (existing) => existing.scientific_name === organism.scientific_name,
      );
      if (!known) {
        group.organisms.push(organism);
      }
    }
  }

  return [...groups.values()];
}

export function chainsLabel(chains: string[]): string | null {
  if (chains.length === 0) {
    return null;
  }
  return `${chains.length === 1 ? "Chain" : "Chains"} ${chains.join(", ")}`;
}

export function organismLabel(organisms: PolymerEntityOrganism[]): string | null {
  const names = organisms
    .map((organism) => organism.scientific_name.trim())
    .filter(Boolean);
  return names.length > 0 ? names.join(", ") : null;
}

export function uniProtSourceLabel(source: PolymerEntityUniProtSource): string {
  return source === "sifts" ? "SIFTS" : "Depositor";
}

export function uniProtURL(accession: string): string {
  return `https://www.uniprot.org/uniprotkb/${encodeURIComponent(accession.trim())}`;
}

export function taxonomyURL(taxonomyId: number): string {
  return `https://www.ncbi.nlm.nih.gov/Taxonomy/Browser/wwwtax.cgi?id=${taxonomyId}`;
}

/** Whether opening the row shows anything the collapsed line does not. */
export function hasEntityDetails(view: PolymerEntityView): boolean {
  return (
    view.construct !== null ||
    view.mutationsText !== null ||
    view.sequence !== null ||
    view.uniprotMappings.length > 0 ||
    view.organisms.some((organism) => organism.ncbi_taxonomy_id != null)
  );
}

// A depositor writes this field by hand. "K986P, V987P" splits into chips;
// "C to S", "YES" and prose do not, and are left as written.
const SUBSTITUTION = /^[A-Za-z]{1,3}\d+[A-Za-z]{1,3}$/;

export function mutationTokens(text: string): string[] | null {
  const tokens = text
    .split(",")
    .map((token) => token.trim())
    .filter(Boolean);
  if (tokens.length === 0 || !tokens.every((token) => SUBSTITUTION.test(token))) {
    return null;
  }
  return tokens;
}

function sequenceForEntity(
  proteinSequenceId: string | null,
  entityId: string | null,
  sequences: ProteinSequence[],
): ProteinSequence | null {
  if (proteinSequenceId) {
    const sequence = sequences.find((candidate) => candidate.id === proteinSequenceId);
    if (sequence) {
      return sequence;
    }
  }
  if (!entityId) {
    return null;
  }
  const suffix = `_${entityId.toLowerCase()}`;
  return (
    sequences.find((sequence) =>
      recordID(sequence.header).toLowerCase().endsWith(suffix),
    ) ?? null
  );
}

function chainForEntity(entity: PolymerEntity): string | null {
  const label = trimmed(entity.label_asym_id);
  const author = trimmed(entity.auth_asym_id);
  if (label && author && label !== author) {
    return `${label}[auth ${author}]`;
  }
  return author ?? label;
}

function recordID(header: string): string {
  return header.replace(/^>/, "").split("|")[0].trim();
}

// The chain list is the second defline field: "Chain A", "Chains A, C", or
// "Chain C[auth D]" when the deposited label differs from the author's.
function chainsFromHeader(header: string): string[] {
  const field = header.split("|")[1]?.trim() ?? "";
  const match = /^chains?\b(.*)$/i.exec(field);
  if (!match) {
    return [];
  }
  return match[1]
    .split(",")
    .map((chain) => chain.trim())
    .filter(Boolean);
}

// Entity ids are numbers kept as text, so the backend's lexicographic order
// puts entity 10 in front of entity 2.
function byEntityId(first: PolymerEntityView, second: PolymerEntityView): number {
  const firstId = first.entityId ?? "";
  const secondId = second.entityId ?? "";
  if (/^\d+$/.test(firstId) && /^\d+$/.test(secondId)) {
    return Number(firstId) - Number(secondId);
  }
  return firstId.localeCompare(secondId);
}

function residueCount(sequence: string): number {
  return sequence.replace(/\s+/g, "").length;
}

function trimmed(value: string | undefined): string | null {
  const text = value?.trim();
  return text ? text : null;
}
