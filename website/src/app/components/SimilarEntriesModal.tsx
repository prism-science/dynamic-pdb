"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import Link from "next/link";

import type { ProteinSequence, SimilarEntry } from "@/lib/api/entries";
import { bestMatchStats, formatPercent } from "@/lib/similarity";
import SimilarMatchAlignment from "./SimilarMatchAlignment";

import styles from "./SimilarEntriesModal.module.css";

/**
 * The full similar-proteins list as a dialog: one row per entry with its best
 * numbers, expanding into the per-chain alignment view. Same dialog pattern
 * as FilePreviewModal (backdrop + card, Escape and backdrop-click close).
 */
export default function SimilarEntriesModal({
  entryName,
  sequences,
  items,
  onClose,
}: {
  entryName: string;
  sequences: ProteinSequence[];
  items: SimilarEntry[];
  onClose: () => void;
}) {
  // Portals need the document; skip the first server-matching render.
  const [mounted, setMounted] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  useEffect(() => setMounted(true), []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  if (!mounted) {
    return null;
  }

  return createPortal(
    <div className={styles.backdrop} onClick={onClose}>
      <div
        className={styles.card}
        role="dialog"
        aria-label={`Proteins similar to ${entryName}`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className={styles.head}>
          <span className={styles.title}>Similar proteins</span>
          <button
            type="button"
            className={styles.close}
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>

        <div className={styles.body}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Protein</th>
                <th>Identity</th>
                <th>Coverage</th>
                <th>Score</th>
                {/* The score bar lives in its own headerless column, so the
                    "Score" title sits over the digits, not over the bar. */}
                <th aria-hidden="true" />
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <Row
                  key={item.entry.id}
                  item={item}
                  sequences={sequences}
                  expanded={expandedId === item.entry.id}
                  onToggle={() =>
                    setExpandedId((current) =>
                      current === item.entry.id ? null : item.entry.id,
                    )
                  }
                />
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function Row({
  item,
  sequences,
  expanded,
  onToggle,
}: {
  item: SimilarEntry;
  sequences: ProteinSequence[];
  expanded: boolean;
  onToggle: () => void;
}) {
  const stats = bestMatchStats(item.matches);
  const chains = item.matches.length;
  const score = Math.min(1, Math.max(0, item.score));

  return (
    <>
      <tr
        className={styles.row}
        data-open={expanded ? "true" : undefined}
        onClick={onToggle}
      >
        <td>
          <div className={styles.cellMain}>
            <button
              type="button"
              className={styles.expander}
              aria-expanded={expanded}
              aria-label={expanded ? "Hide alignment" : "Show alignment"}
              onClick={(event) => {
                // The row toggles too; without this the two handlers cancel
                // each other out and the chevron appears dead.
                event.stopPropagation();
                onToggle();
              }}
            >
              <ChevronIcon />
            </button>
            <span
              className={styles.thumb}
              data-empty={item.entry.thumbnail_image_url ? undefined : "true"}
            >
              {item.entry.thumbnail_image_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={item.entry.thumbnail_image_url}
                  alt=""
                  loading="lazy"
                />
              ) : (
                <MoleculeIcon />
              )}
            </span>
            <span className={styles.nameBlock}>
              <Link
                className={styles.name}
                href={`/entries/${encodeURIComponent(item.entry.id)}`}
                onClick={(event) => event.stopPropagation()}
              >
                {item.entry.name}
              </Link>
              <span className={styles.rowSub}>
                {chains} {chains === 1 ? "chain" : "chains"} matched
              </span>
            </span>
          </div>
        </td>
        <td className={styles.num} data-grade={identityGrade(stats.fident)}>
          {formatPercent(stats.fident)}
        </td>
        <td className={styles.num}>{formatPercent(stats.qcov)}</td>
        <td className={styles.num}>{score.toFixed(2)}</td>
        <td className={styles.barCell}>
          <span className={styles.scoreBar}>
            <i style={{ width: `${Math.round(score * 100)}%` }} />
          </span>
        </td>
      </tr>
      {expanded ? (
        <tr className={styles.detailRow}>
          <td colSpan={5}>
            <SimilarMatchAlignment
              matches={item.matches}
              sequences={sequences}
            />
          </td>
        </tr>
      ) : null}
    </>
  );
}

function identityGrade(fident: number | null): string | undefined {
  if (fident == null) {
    return undefined;
  }
  return fident >= 0.6 ? "good" : fident >= 0.25 ? "warn" : "dim";
}

function ChevronIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m9 6 6 6-6 6" />
    </svg>
  );
}

function MoleculeIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
