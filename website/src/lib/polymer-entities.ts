import type {
  PolymerEntity,
  PolymerEntityOrganism,
  PolymerEntityUniProtMapping,
  PolymerEntityUniProtSource,
  ProteinSequence,
} from "@/lib/api/entries";

/** A polymer entity joined with the stored sequence it describes. Chains and
 *  residue counts are not part of the entity payload; they come from the FASTA
 *  record the sync matched to it. */
export type PolymerEntityView = {
  key: string;
  entityId: string | null;
  name: string;
  chains: string[];
  residues: number | null;
  sequence: string | null;
  organisms: PolymerEntityOrganism[];
  construct: string | null;
  /** Single substitutions, when the field holds a plain list of them. Null for
   *  anything else -- the field is free text from the depositor -- and then
   *  mutationsText is shown as written. */
  mutations: string[] | null;
  mutationsText: string | null;
  uniprotMappings: PolymerEntityUniProtMapping[];
};

export function polymerEntityViews(
  entities: PolymerEntity[],
  sequences: ProteinSequence[],
): PolymerEntityView[] {
  return entities
    .map((entity, index) => {
      const entityId = trimmed(entity.label_entity_id);
      const sequence = sequenceForEntity(entityId, sequences);
      const mutationsText = trimmed(entity.mutations);
      const residues = sequence ? residueCount(sequence.sequence) : null;

      return {
        key: entity.id || entityId || String(index),
        entityId,
        name:
          trimmed(entity.description) ??
          (entityId ? `Entity ${entityId}` : "Polymer entity"),
        chains: sequence ? chainsFromHeader(sequence.header) : [],
        residues,
        sequence: sequence?.sequence ?? null,
        organisms: entity.source_organisms ?? [],
        construct: trimmed(entity.construct),
        mutations: mutationsText ? mutationTokens(mutationsText) : null,
        mutationsText,
        uniprotMappings: entity.uniprot_mappings ?? [],
      };
    })
    .sort(byEntityId);
}

/** Distinct molecules behind the entities: a structure solved with the same
 *  protein in three constructs is one molecule, not three. */
export function moleculeCount(views: PolymerEntityView[]): number {
  const molecules = views.map(
    (view) =>
      view.uniprotMappings[0]?.accession.toLowerCase() ?? view.name.toLowerCase(),
  );
  return new Set(molecules).size;
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

// The only link between a stored sequence and the entity it belongs to. RCSB
// names each FASTA record "<PDB>_<entity>", which is what the sync matched on
// when it created the entity; the API does not expose the foreign key.
function sequenceForEntity(
  entityId: string | null,
  sequences: ProteinSequence[],
): ProteinSequence | null {
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
