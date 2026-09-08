import type { Entity } from "@/lib/api/entries";
import {
  formatLabel,
  getEntityFileURL,
  getFilePayload,
  modelScopedEntities,
} from "@/lib/entities";

/** One line of the download menu. */
export type DownloadFile = {
  id: string;
  label: string;
  href: string;
};

/**
 * The files this record offers by name, in groups.
 *
 * Not the whole file list -- that is the Files tab. These are the few a reader
 * comes for: the sequence, the coordinates, the measured data. Each group is
 * drawn with a rule between it and the next, the way RCSB separates sequence
 * from coordinates from structure factors.
 */
export type DownloadGroup = {
  key: string;
  files: DownloadFile[];
};

const STRUCTURE_FACTOR_NAME = /structure[-_. ]?factor|(^|[-_.])sf([-_.]|$)/i;

/**
 * Pick the named files out of the entry's artifacts, scoped to one model.
 *
 * Entry-owned artifacts are every model's, so the sequence is always here;
 * coordinates and measured data come from the model the page is currently
 * about, which is what makes the button follow the rail.
 */
export function downloadGroups(
  entities: Entity[],
  modelId: string | null,
): DownloadGroup[] {
  const inScope = modelScopedEntities(entities, modelId);

  const sequences = inScope
    .filter((entity) => getFilePayload(entity)?.type === "fasta")
    .map((entity) => file(entity, "FASTA Sequence"));

  const coordinates = inScope
    .filter((entity) => entity.type === "model")
    .map((entity) =>
      file(entity, `Coordinates (${formatOf(entity) ?? "file"})`),
    );

  const factors = inScope
    .filter((entity) => isStructureFactorCif(getFilePayload(entity)?.type))
    .map((entity) => file(entity, "Structure Factors (CIF)"));

  return [
    { key: "sequence", files: present(sequences) },
    { key: "coordinates", files: present(coordinates) },
    { key: "factors", files: present(factors) },
  ].filter((group) => group.files.length > 0);
}

// A file with no address cannot be offered: a menu item that downloads nothing
// is worse than one that is not there.
function present(files: (DownloadFile | null)[]): DownloadFile[] {
  return files.filter((entry): entry is DownloadFile => entry !== null);
}

function file(entity: Entity, label: string): DownloadFile | null {
  const href = getEntityFileURL(entity);
  return href ? { id: entity.id, label, href } : null;
}

function formatOf(entity: Entity): string | null {
  return formatLabel(getFilePayload(entity)?.type);
}

// CIF and nothing else. We hold reflections as MTZ too, but that is the file
// the viewer overlays maps from, not the structure factors a reader means when
// they ask for them -- offering it under this name would hand them the wrong
// file, so a format has to say both "structure factors" and "cif" to qualify.
function isStructureFactorCif(format: string | undefined): boolean {
  return (
    format !== undefined &&
    STRUCTURE_FACTOR_NAME.test(format) &&
    /cif/i.test(format)
  );
}
