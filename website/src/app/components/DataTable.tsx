"use client";

import { useState } from "react";

import type { Entity, EntityLevel } from "@/lib/api/structures";
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

  const graphEntities = entities.filter(
    (entity) => entity.level !== null && entity.type !== "program",
  );

  return (
    <div className={styles.grid}>
      {LEVELS.map((level) => {
        const items = graphEntities.filter((entity) => entity.level === level);
        return (
          <div className={styles.column} key={level}>
            <div className={styles.columnHead} data-level={level}>
              <span className={styles.levelBadge}>{level}</span>
              <span className={styles.levelTitle}>{LEVEL_TITLE[level]}</span>
            </div>

            <div className={styles.cells}>
              {items.length === 0 ? (
                <span className={styles.emptyCell}>—</span>
              ) : (
                items.map((entity) => (
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
                ))
              )}
            </div>
          </div>
        );
      })}

      <FilePreviewModal entity={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
