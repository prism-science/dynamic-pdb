"use client";

import { useMemo } from "react";
import dynamic from "next/dynamic";

import type { LineageNode } from "@/lib/lineage";

import { collapseWideRows } from "@/lib/lineage";

import { buildDraftGraph } from "./draftLineage";
import type { ModelDraft, ParsedFile } from "./types";
import styles from "./form.module.css";

// Same graph component the model page uses, so what the depositor checks here
// is literally what the record will show. Loaded on demand: a form that never
// grows a pipeline never pays for React Flow.
// A deposit with sixty stray files should not print sixty chips.
const TRAY_LIMIT = 8;

const LineageFlow = dynamic(() => import("../LineageFlow"), {
  ssr: false,
  loading: () => <div className={styles.previewLoading}>Loading graph…</div>,
});

export default function PipelinePreview({
  draft,
  baseline = [],
  onSupplyExpected,
}: {
  draft: ModelDraft;
  baseline?: ParsedFile[];
  /** Opens a file picker bound to one missing input. */
  onSupplyExpected?: (node: LineageNode) => void;
}) {
  const graph = useMemo(() => {
    const built = buildDraftGraph(draft, baseline);
    return { ...built, lineage: collapseWideRows(built.lineage) };
  }, [draft, baseline]);

  const rows =
    graph.lineage.nodes.reduce(
      (deepest, node) => Math.max(deepest, node.generation),
      0,
    ) + 1;
  const height = Math.min(560, Math.max(220, rows * 100 + 40));

  if (graph.lineage.nodes.length === 0) {
    return (
      <div className={styles.previewEmpty}>
        The graph appears once a model file is added.
      </div>
    );
  }

  return (
    <div className={styles.preview}>
      <div className={styles.previewCanvas} style={{ height }}>
        <LineageFlow
          lineage={graph.lineage}
          onSupply={onSupplyExpected}
          compact
        />
      </div>

      {/* Docked below the canvas rather than floating inside it: a staging
          area that pans and zooms away with the graph is one the depositor
          loses the moment they move the view. */}
      {graph.unlinkedFiles.length > 0 || graph.unlinkedPrograms.length > 0 ? (
        <div className={styles.tray}>
          <span className={styles.trayLabel}>Unlinked</span>
          <div className={styles.trayChips}>
            {graph.unlinkedFiles.slice(0, TRAY_LIMIT).map((file) => (
              <span key={file.id} className={styles.trayChip}>
                <span className={styles.trayChipMeta}>{file.level}</span>
                {file.name}
              </span>
            ))}
            {graph.unlinkedFiles.length > TRAY_LIMIT ? (
              <span className={styles.trayChip} data-kind="count">
                +{graph.unlinkedFiles.length - TRAY_LIMIT} more
              </span>
            ) : null}
            {graph.unlinkedPrograms.map((program) => (
              <span
                key={program.id}
                className={styles.trayChip}
                data-kind="program"
              >
                {program.name}
                {program.version ? ` ${program.version}` : ""}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
