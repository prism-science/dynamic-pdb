import type { Entity } from "@/lib/api/entries";

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
