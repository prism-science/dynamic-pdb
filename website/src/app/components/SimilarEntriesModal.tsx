"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import Link from "next/link";

import type { ProteinSequence, SimilarEntry } from "@/lib/api/entries";
import { formatEntryLabel } from "@/lib/entry-label";
import {
  bestMatchStats,
  formatPercent,
  SIMILAR_ENTRIES_FETCH_LIMIT,
} from "@/lib/similarity";
import SimilarMatchAlignment from "./SimilarMatchAlignment";

import styles from "./SimilarEntriesModal.module.css";

/* Page size for follow-up loads; the first page arrived with the page. */
const pageSize = 50;

type SimilarPage = {
  items: SimilarEntry[];
};

/**
 * The full similar-proteins list as a dialog: one row per entry with its best
 * numbers, expanding into the per-chain alignment view. Same dialog pattern
 * as FilePreviewModal (backdrop + card, Escape and backdrop-click close).
 * Scrolling near the bottom loads further pages from the similar feed, the
 * same way the entries list keeps loading on the home page.
 */
export default function SimilarEntriesModal({
  entryId,
  entryName,
  sequences,
  items: initialItems,
  onClose,
}: {
  entryId: string;
  entryName: string;
  sequences: ProteinSequence[];
  items: SimilarEntry[];
  onClose: () => void;
}) {
  // Portals need the document; skip the first server-matching render.
  const [mounted, setMounted] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [items, setItems] = useState(initialItems);
  const [hasMore, setHasMore] = useState(
    initialItems.length >= SIMILAR_ENTRIES_FETCH_LIMIT,
  );
  const [loading, setLoading] = useState(false);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
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

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) {
      return;
    }
    setLoading(true);
    try {
      const params = new URLSearchParams({
        limit: String(pageSize),
        offset: String(items.length),
      });
      const response = await fetch(
        `/entries/${encodeURIComponent(entryId)}/similar?${params.toString()}`,
        { cache: "no-store" },
      );
      if (!response.ok) {
        setHasMore(false);
        return;
      }
      const page = (await response.json()) as SimilarPage;
      setItems((current) => {
        const seen = new Set(current.map((item) => item.entry.id));
        return [
          ...current,
          ...page.items.filter((item) => !seen.has(item.entry.id)),
        ];
      });
      setHasMore(page.items.length === pageSize);
    } finally {
      setLoading(false);
    }
  }, [entryId, hasMore, items.length, loading]);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!mounted || sentinel == null || !hasMore) {
      return;
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          void loadMore();
        }
      },
      // The dialog body is the scroll container, not the window.
      { root: bodyRef.current, rootMargin: "400px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [mounted, hasMore, loadMore]);

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

        <div className={styles.body} ref={bodyRef}>
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
          {hasMore ? (
            <div
              ref={sentinelRef}
              className={styles.loadSentinel}
              aria-hidden="true"
            />
          ) : null}
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
                {formatEntryLabel(item.entry)}
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
