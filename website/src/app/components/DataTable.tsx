"use client";

import { useState, type CSSProperties } from "react";

import type { Entity, EntityLevel } from "@/lib/api/entries";
import { dataTableEntities } from "@/lib/entities";
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
  const [selected, setSelected] = useState<Entity | null>(null);

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
          <div className={styles.columnHead} data-level={level}>
            <span className={styles.levelBadge}>{level}</span>
            <span className={styles.levelTitle}>{LEVEL_TITLE[level]}</span>
          </div>

          <div className={styles.cells}>
            {items.map((entity) => (
              <button
                type="button"
                key={entity.id}
                className={styles.cell}
                data-level={level}
                onClick={() => setSelected(entity)}
                title="Open preview"
              >
                <span className={styles.cellType} data-type={entity.type}>
                  {entity.type}
                </span>
                <span className={styles.cellName}>{entity.name}</span>
              </button>
            ))}
          </div>
        </div>
      ))}

      <FilePreviewModal entity={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
