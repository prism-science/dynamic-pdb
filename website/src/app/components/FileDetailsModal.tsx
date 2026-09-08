"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

import type { Entity } from "@/lib/api/entries";
import {
  type FileItem,
  formatFileSize,
  formatLabel,
  isPreviewable,
} from "@/lib/entities";
import ResolvedFileLink from "./ResolvedFileLink";

import styles from "./FileDetailsModal.module.css";

/**
 * What the file table opens on a row: everything recorded about one file, in a
 * window with room for it. The 64-character digest in particular has nowhere to
 * live in a table cell, so the cell shows its first bytes and this dialog
 * carries the whole thing.
 *
 * The viewer is one step further in rather than what a click on the row does:
 * most visits to a file are about what it is, not about looking inside it, and
 * the structure viewer is heavy enough that opening it by accident is a cost.
 *
 * View and Download sit on the header strip, where the preview dialog keeps
 * its own actions: the body of either window is about the file, and the strip
 * above it is what you can do with it.
 */
export default function FileDetailsModal({
  item,
  onClose,
  onPreview,
}: {
  item: FileItem | null;
  onClose: () => void;
  onPreview: (entity: Entity) => void;
}) {
  const [mounted, setMounted] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => setMounted(true), []);

  // A fresh file gets a fresh button: leaving "Copied" up while another row's
  // digest is on screen would claim something untrue.
  useEffect(() => setCopied(false), [item?.id]);

  useEffect(() => {
    if (!item) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [item, onClose]);

  if (!mounted || !item) {
    return null;
  }

  const sha256 =
    typeof item.sha256 === "string" && item.sha256.trim() !== ""
      ? item.sha256.trim().toLowerCase()
      : null;
  const format = formatLabel(item.type);
  const size = formatFileSize(item.size);
  const entity = item.entity;
  const canPreview = isPreviewable(entity);

  async function copyChecksum() {
    if (!sha256) {
      return;
    }
    try {
      await navigator.clipboard.writeText(sha256);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return createPortal(
    <div className={styles.backdrop} onClick={onClose}>
      <div
        className={styles.card}
        role="dialog"
        aria-label={`Details of ${item.name}`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className={styles.head}>
          <span className={styles.title}>{item.name}</span>
          {/* On the strip beside the name, the way the preview dialog carries
              its own actions: what you can do with the file is the same
              question in both windows, so it is answered in the same place. */}
          <div className={styles.actions}>
            {canPreview && entity ? (
              <button
                type="button"
                className={styles.action}
                onClick={() => onPreview(entity)}
              >
                <MoleculeIcon />
                View
              </button>
            ) : null}
            {item.url ? (
              <ResolvedFileLink
                className={styles.action}
                href={item.url}
                download
                rel="noreferrer"
                target="_blank"
              >
                <DownloadIcon />
                Download
              </ResolvedFileLink>
            ) : null}
            <button
              type="button"
              className={styles.close}
              onClick={onClose}
              aria-label="Close"
            >
              ×
            </button>
          </div>
        </div>

        <div className={styles.body}>
          <dl className={styles.facts}>
            <div className={styles.factRow}>
              <dt>Level</dt>
              <dd>{item.level ?? <span className={styles.absent}>—</span>}</dd>
            </div>
            <div className={styles.factRow}>
              <dt>Format</dt>
              <dd>{format ?? <span className={styles.absent}>—</span>}</dd>
            </div>
            <div className={styles.factRow}>
              <dt>Size</dt>
              <dd>{size ?? <span className={styles.absent}>—</span>}</dd>
            </div>
          </dl>

          {/* Out of the label/value list and into a block of its own: it is the
              one value here that is copied rather than read, and at 64
              characters it needs the full width to wrap into. A file with no
              digest recorded drops the block rather than announcing the gap —
              there is nothing here for anyone to act on. */}
          {sha256 ? (
            <div className={styles.checksum}>
              <div className={styles.checksumHead}>
                <span className={styles.checksumKey}>SHA-256</span>
                <button
                  type="button"
                  className={styles.copy}
                  onClick={copyChecksum}
                >
                  {copied ? "Copied" : "Copy"}
                </button>
              </div>
              <code className={styles.checksumValue}>{sha256}</code>
            </div>
          ) : null}
        </div>
      </div>
    </div>,
    document.body,
  );
}

function MoleculeIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="5" r="2.2" />
      <circle cx="5" cy="17" r="2.2" />
      <circle cx="19" cy="17" r="2.2" />
      <path d="M12 7.2 6.6 15M12 7.2 17.4 15M7 17h10" />
    </svg>
  );
}

function DownloadIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
    </svg>
  );
}
