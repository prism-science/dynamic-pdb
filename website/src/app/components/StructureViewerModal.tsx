"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import dynamic from "next/dynamic";

import type { StructureKind, StructureMap } from "@/lib/structureKind";
import ResolvedFileLink from "./ResolvedFileLink";
import styles from "./StructureViewerModal.module.css";

const StructureViewer = dynamic(() => import("./StructureViewer"), {
  ssr: false,
});

export default function StructureViewerModal({
  url,
  kind,
  name,
  maps,
}: {
  url: string;
  kind: StructureKind;
  name: string;
  maps?: StructureMap[];
}) {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <>
      <button
        type="button"
        className={styles.structureThumb}
        onClick={() => setOpen(true)}
        title="Click to view in 3D"
        aria-label="Open 3D structure viewer"
      >
        <MoleculeIcon />
        <span className={styles.structureThumbLabel}>View</span>
      </button>

      {mounted && open
        ? createPortal(
            <div
              className={`${styles.chartOverlayBackdrop} ${styles.structureOverlayBackdrop}`}
              onClick={() => setOpen(false)}
            >
              <div
                className={`${styles.chartOverlayCard} ${styles.structureOverlayCard}`}
                role="dialog"
                aria-label={`Structure viewer for ${name}`}
                onClick={(event) => event.stopPropagation()}
              >
                <div className={styles.chartOverlayHead}>
                  <span className={styles.chartOverlayTitle}>{name}</span>
                  <div className={styles.headActions}>
                    {/* Resolves the storage URL itself, the same way the file
                        preview does, so both routes offer the same download. */}
                    <ResolvedFileLink
                      className={styles.download}
                      href={url}
                      download
                      target="_blank"
                      rel="noreferrer"
                    >
                      <DownloadIcon />
                      Download
                    </ResolvedFileLink>
                    <button
                      type="button"
                      className={styles.closeButton}
                      onClick={() => setOpen(false)}
                      aria-label="Close viewer"
                    >
                      ×
                    </button>
                  </div>
                </div>
                <div className={styles.structureOverlayBody}>
                  <StructureViewer url={url} kind={kind} maps={maps} fill />
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

function DownloadIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
    </svg>
  );
}

function MoleculeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="5" r="2.2" />
      <circle cx="5" cy="17" r="2.2" />
      <circle cx="19" cy="17" r="2.2" />
      <path d="M12 7.2 6.6 15M12 7.2 17.4 15M7 17h10" />
    </svg>
  );
}
