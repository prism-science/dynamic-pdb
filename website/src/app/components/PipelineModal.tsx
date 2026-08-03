"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import dynamic from "next/dynamic";

import type { Entity } from "@/lib/api/entries";
import { structureMaps } from "@/lib/entities";
import type { Lineage, LineageNode } from "@/lib/lineage";
import FilePreviewModal from "./FilePreviewModal";

import styles from "./PipelineModal.module.css";

// Loaded on first open, never on page load: React Flow is the heaviest thing
// on this route and most visits never ask for the graph.
const LineageFlow = dynamic(() => import("./LineageFlow"), {
  ssr: false,
  loading: () => <div className={styles.loading}>Loading graph…</div>,
});

export default function PipelineModal({
  lineage,
  title,
}: {
  lineage: Lineage;
  title: string;
}) {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const [preview, setPreview] = useState<Entity | null>(null);

  // Same preview a DataTable cell opens, and the same density maps offered
  // alongside it — here the candidates are whatever the chain itself contains.
  const maps = useMemo(
    () => structureMaps(lineage.nodes.map((node) => node.entity)),
    [lineage],
  );

  // Stable identity: LineageFlow memoises its layout on this callback.
  const openPreview = useCallback(
    (node: LineageNode) => setPreview(node.entity),
    [],
  );

  // A one-run chain is three rows tall; giving it the same box as a four-run
  // chain leaves half the overlay empty. Row height mirrors LineageFlow's
  // node height plus row gap.
  const height = useMemo(() => {
    const rows =
      lineage.nodes.reduce((deepest, node) => Math.max(deepest, node.generation), 0) + 1;
    return Math.max(320, rows * 120 + 120);
  }, [lineage]);

  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      // The preview sits on top and closes itself on Escape; without this the
      // same keypress would dismiss the graph underneath it too.
      if (event.key === "Escape" && !preview) {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, preview]);

  if (lineage.runCount === 0) {
    return null;
  }

  return (
    <>
      <button
        type="button"
        className={styles.trigger}
        onClick={() => setOpen(true)}
        aria-label="Open the pipeline that produced this model"
      >
        <PipelineIcon />
        Pipeline
      </button>

      {mounted && open
        ? createPortal(
            <div className={styles.backdrop} onClick={() => setOpen(false)}>
              <div
                className={styles.card}
                style={{ "--pipeline-height": `${height}px` } as CSSProperties}
                role="dialog"
                aria-label={`Pipeline for ${title}`}
                onClick={(event) => event.stopPropagation()}
              >
                <div className={styles.head}>
                  <span className={styles.title}>{title}</span>
                  <button
                    type="button"
                    className={styles.close}
                    onClick={() => setOpen(false)}
                    aria-label="Close"
                  >
                    ×
                  </button>
                </div>
                <div className={styles.body}>
                  <LineageFlow lineage={lineage} onSelect={openPreview} />
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}

      {/* Deliberately a sibling of the portal, not a child of the backdrop:
          React events bubble through the component tree, not the DOM, so a
          click inside the preview would otherwise reach the backdrop's
          onClick and close the graph behind it. */}
      <FilePreviewModal
        entity={preview}
        maps={maps}
        onClose={() => setPreview(null)}
      />
    </>
  );
}

function PipelineIcon() {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="6" cy="5" r="2.4" />
      <circle cx="6" cy="19" r="2.4" />
      <circle cx="18" cy="12" r="2.4" />
      <path d="M8.2 6.3 15.6 10.8M8.2 17.7 15.6 13.2" />
    </svg>
  );
}
