import type {
  ArchivePolymerEntityProposal,
  DepositorPolymerEntity,
} from "@/lib/api/entries";

import type { PolymerEntityDraft } from "./types";

/** The pattern backend/api/openapi.yaml sets on DepositorPolymerEntity.mutations. */
export const MUTATION_PATTERN = /^[A-Z][1-9][0-9]*[A-Z]$/;

export function normalizeMutation(raw: string): string {
  return raw.replace(/\s+/g, "").toUpperCase();
}

export function mutationError(mutation: string): string | null {
  return MUTATION_PATTERN.test(mutation)
    ? null
    : `${mutation} is not one substitution: write the original residue, the UniProt position and the new residue, for example C54T`;
}

/** A PDB ID as the archive lookup accepts it: a digit, then three letters or digits. */
export const PDB_ID_PATTERN = /^[0-9][A-Z0-9]{3}$/;

export function organismLabel(organism: {
  scientific_name: string;
  ncbi_taxonomy_id?: number;
}): string {
  return organism.ncbi_taxonomy_id
    ? `${organism.scientific_name} (taxon ${organism.ncbi_taxonomy_id})`
    : organism.scientific_name;
}

/**
 * The entities of a fresh lookup, with what the depositor already typed for
 * them carried over by label_entity_id. An entity the lookup no longer
 * reports is dropped with its typed values: it belonged to another PDB ID.
 */
export function polymerEntityDraftsFromArchive(
  proposals: ArchivePolymerEntityProposal[],
  current: PolymerEntityDraft[],
): PolymerEntityDraft[] {
  return proposals.flatMap((proposal) => {
    const labelEntityId = proposal.label_entity_id?.trim();
    if (!labelEntityId) {
      return [];
    }
    const typed = current.find((draft) => draft.labelEntityId === labelEntityId);
    return [
      {
        labelEntityId,
        description: proposal.description ?? "",
        uniprotAccessions: proposal.uniprot_mappings.map(
          (mapping) => mapping.accession,
        ),
        archiveOrganisms: proposal.source_organisms.map(organismLabel),
        archiveConstruct: proposal.construct ?? "",
        archiveMutations: proposal.mutations ?? [],
        construct: typed?.construct ?? "",
        mutations: typed?.mutations ?? [],
        organismName: typed?.organismName ?? "",
        organismTaxonomyId: typed?.organismTaxonomyId ?? "",
      },
    ];
  });
}

/** The API uses a depositor's organism only when extraction found none. */
export function organismIsEditable(entity: PolymerEntityDraft): boolean {
  return entity.archiveOrganisms.length === 0;
}

/**
 * Only what the depositor typed is sent. A value copied from the archive
 * would be stored as theirs, and the sync never overwrites a depositor's
 * value, so later archive corrections would stop reaching the entry.
 */
export function polymerEntitiesRequest(
  entities: PolymerEntityDraft[],
): DepositorPolymerEntity[] | undefined {
  const supplied = entities.flatMap((entity): DepositorPolymerEntity[] => {
    const construct = entity.construct.trim();
    const organismName = entity.organismName.trim();
    const taxonomyId = entity.organismTaxonomyId.trim();
    const sourceOrganisms =
      organismIsEditable(entity) && organismName
        ? [
            {
              scientific_name: organismName,
              ...(taxonomyId ? { ncbi_taxonomy_id: Number(taxonomyId) } : {}),
            },
          ]
        : [];
    if (!construct && entity.mutations.length === 0 && sourceOrganisms.length === 0) {
      return [];
    }
    return [
      {
        label_entity_id: entity.labelEntityId,
        ...(construct ? { construct } : {}),
        ...(entity.mutations.length > 0 ? { mutations: entity.mutations } : {}),
        ...(sourceOrganisms.length > 0 ? { source_organisms: sourceOrganisms } : {}),
      },
    ];
  });
  return supplied.length > 0 ? supplied : undefined;
}
