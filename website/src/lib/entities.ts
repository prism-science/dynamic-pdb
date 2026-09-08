import type {
  DataPayload,
  Entity,
  EntityLevel,
  ModelPayload,
} from "@/lib/api/entries";
import { detectStructureKind, type StructureMap } from "@/lib/structureKind";

// One file as the pages list it. Every column of the file table is a field
// here, so a page that can build these can render the table without knowing
// whether the file came from an entry, a model or a pending revision.
export type FileItem = {
  id: string;
  name: string;
  /** Artifact format: "mmcif", "mtz", "fasta", ... */
  type?: string;
  size?: number;
  level?: EntityLevel | null;
  sha256?: string | null;
  url: string | null;
  /** Present when the file is a live entity, which is what a preview needs. */
  entity?: Entity;
};

// Entities the L0-L3 data table can actually place in a column: everything
// that carries a level and is not a program record. Lives here rather than in
// DataTable so server components can call it too — every export of a
// "use client" module becomes a client reference and is not callable from the
// server.
export function dataTableEntities(entities: Entity[]): Entity[] {
  return entities.filter(
    (entity) => entity.level !== null && entity.type !== "program",
  );
}

/**
 * The entry's artifacts as one model sees them: the model's own, plus the
 * entry-owned ones that belong to every model.
 *
 * An artifact with no model_id is the entry's -- the deposited reflections, the
 * sequence -- and every model of the entry is entitled to it. Everything else
 * was made for one model and is nobody else's business, which is what makes
 * the page follow the rail instead of listing the whole entry every time.
 */
export function modelScopedEntities(
  entities: Entity[],
  modelId: string | null,
): Entity[] {
  return entities.filter(
    (entity) => entity.model_id === null || entity.model_id === modelId,
  );
}

export function getFilePayload(
  entity: Entity,
): DataPayload | ModelPayload | null {
  if (entity.type !== "data" && entity.type !== "model") {
    return null;
  }
  if (typeof entity.payload !== "object" || entity.payload === null) {
    return null;
  }
  return entity.payload as DataPayload | ModelPayload;
}

export function getEntityFileURL(entity: Entity): string | null {
  const payload = getFilePayload(entity);
  const fileURL = payload?.file_url;
  return typeof fileURL === "string" && fileURL.length > 0 ? fileURL : null;
}

export function fileItemFromEntity(entity: Entity): FileItem {
  const payload = getFilePayload(entity);
  return {
    id: entity.id,
    name: entity.name,
    type: payload?.type,
    size: payload?.size,
    level: entity.level,
    sha256: payload?.sha256 ?? null,
    url: getEntityFileURL(entity),
    entity,
  };
}

// Whether opening the viewer would show anything. The file dialog offers its
// "Open preview" button only when this is true, so a file the viewer has
// nothing to draw does not get a button that opens an empty window.
//
// Kept in step with what FilePreviewModal actually renders: metrics, a parsed
// FASTA, an image, or a structure the viewer recognises.
export function isPreviewable(entity: Entity | undefined | null): boolean {
  if (!entity) {
    return false;
  }
  if (entity.type === "metrics") {
    return true;
  }
  const payload = getFilePayload(entity);
  if (!payload) {
    return false;
  }
  if (payload.type === "fasta" && payload.metadata) {
    return true;
  }
  const url = getEntityFileURL(entity);
  if (url === null) {
    return false;
  }
  if (payload.type === "image" || /\.(png|jpe?g|gif|webp|svg|bmp)/i.test(url)) {
    return true;
  }
  return detectStructureKind(payload.type ?? url) !== null;
}

// Formats are stored lowercase. Upper-casing reads fine for the acronyms the
// vocabulary is made of — MTZ, PDB, FASTA, CCP4 — but not for mmCIF, which is a
// proper noun with a small prefix, so the ones with a house spelling get one.
const FORMAT_LABELS: Record<string, string> = {
  mmcif: "mmCIF",
  pdbx: "PDBx",
};

export function formatLabel(format: string | null | undefined): string | null {
  const value = (format ?? "").trim();
  if (value === "") {
    return null;
  }
  return FORMAT_LABELS[value.toLowerCase()] ?? value.toUpperCase();
}

export function formatFileSize(size: number | null | undefined): string | null {
  if (typeof size !== "number" || size <= 0) {
    return null;
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? value : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}

// Density-map layers a structure viewer can overlay. As on the model page, every
// nearby MTZ counts, regardless of how it is related to the structure. Shared so
// that a structure opened from a card and the same structure opened from a file
// list are offered the same maps.
export function structureMaps(entities: Entity[]): StructureMap[] {
  return entities
    .filter(
      (entity) =>
        entity.type === "data" &&
        (entity.payload as DataPayload)?.type === "mtz",
    )
    .map((entity) => ({ url: getEntityFileURL(entity), name: entity.name }))
    .filter((map): map is StructureMap => typeof map.url === "string");
}
