import type { DataPayload, Entity, ModelPayload } from "@/lib/api/entries";
import type { StructureMap } from "@/lib/structureKind";

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
