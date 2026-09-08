"use client";

import { useState, type CSSProperties } from "react";

import type { Entity, EntityLevel } from "@/lib/api/entries";
import {
  dataTableEntities,
  type FileItem,
  fileItemFromEntity,
  structureMaps,
} from "@/lib/entities";
import FileDetailsModal from "./FileDetailsModal";
import FilePreviewModal from "./FilePreviewModal";

import styles from "./DataTable.module.css";

const LEVELS: EntityLevel[] = ["L0", "L1", "L2", "L3"];
const LEVEL_TITLE: Record<EntityLevel, string> = {
  L0: "Raw",
  L1: "Processed",
  L2: "Models",
  L3: "Evaluations",
};

export default function DataTable({ entities }: { entities: Entity[] }) {
  const [details, setDetails] = useState<FileItem | null>(null);
  const [preview, setPreview] = useState<Entity | null>(null);

  const graphEntities = dataTableEntities(entities);

  // Empty levels are dropped rather than shown as a dash, so a model with only
  // L2 output does not render three placeholder columns.
  const columns = LEVELS.map((level) => ({
    level,
    items: graphEntities.filter((entity) => entity.level === level),
  })).filter((column) => column.items.length > 0);

  if (columns.length === 0) {
    return null;
  }

  // A file opens its details — the same window the entry's file list opens — so
  // format, size and checksum are one click away from either page. Metrics are
  // not a file and have nothing to state beyond their numbers, so they still go
  // straight to the view that prints them.
  function open(entity: Entity) {
    if (entity.type === "metrics") {
      setPreview(entity);
      return;
    }
    setDetails(fileItemFromEntity(entity));
  }

  return (
    <div
      className={styles.grid}
      // The surviving columns split the full width evenly; data-cols lets the
      // stylesheet special-case a lone column. See DataTable.module.css.
      data-cols={columns.length}
      style={{ "--cols": columns.length } as CSSProperties}
    >
      {columns.map(({ level, items }) => (
        <div className={styles.column} key={level}>
          <div className={styles.columnHead}>
            {level} {LEVEL_TITLE[level]}
          </div>

          <div className={styles.cells}>
            {items.map((entity) => (
              <button
                type="button"
                key={entity.id}
                className={styles.cell}
                onClick={() => open(entity)}
                title="Open preview"
              >
                <span className={styles.cellName}>{entity.name}</span>
              </button>
            ))}
          </div>
        </div>
      ))}

      <FileDetailsModal
        item={preview ? null : details}
        onClose={() => setDetails(null)}
        onPreview={setPreview}
      />

      <FilePreviewModal
        entity={preview}
        maps={structureMaps(entities)}
        onClose={() => setPreview(null)}
      />
    </div>
  );
}
