"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";

import type {
  ProteinSequence,
  SimilarEntry,
  SimilarEntryMatch,
} from "@/lib/api/entries";
import { formatEntryLabel } from "@/lib/entry-label";
import {
  bestMatch,
  deflineTail,
  formatEvalue,
  formatPercent,
  identityGrade,
  matchedSpan,
  SIMILAR_ENTRIES_FETCH_LIMIT,
  sortedByScore,
  spanLabel,
} from "@/lib/similarity";
import SimilarMatchAlignment from "./SimilarMatchAlignment";

import styles from "./SimilarProteins.module.css";

/* Page size for follow-up loads; the first page arrived with the page. */
const pageSize = 50;

type SimilarPage = {
  items: SimilarEntry[];
};

/**
 * "Similar proteins" for the Sequence tab: every hit in one scrolling list
 * under the feature viewer, each row opening into its per-chain alignments.
 *
 * The four numbers are the ones a reader compares hits by -- identity,
 * coverage, e-value, and where on the chain the match falls -- all read off
 * one alignment, the best-scoring match of the hit. Scrolling near the bottom
 * loads further pages from the similar feed, the same way the entries list
 * keeps loading on the home page.
 */
export default function SimilarProteins({
  entryId,
  sequences,
  items: initialItems,
}: {
  entryId: string;
  /** The entry's own sequences: matches reference them by id, and both the
   *  matched-region bar and the alignment view need their lengths. */
  sequences: ProteinSequence[];
  items: SimilarEntry[];
}) {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [items, setItems] = useState(initialItems);
  const [hasMore, setHasMore] = useState(
    initialItems.length >= SIMILAR_ENTRIES_FETCH_LIMIT,
  );
  const [loading, setLoading] = useState(false);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

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
    if (sentinel == null || !hasMore) {
      return;
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          void loadMore();
        }
      },
      // The list's own box is the scroll container, not the window.
      { root: bodyRef.current, rootMargin: "400px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);

  const sorted = sortedByScore(items);

  // Nothing to point at — no block at all. New sequences are matched in the
  // background, so the block simply appears once the first hits land.
  if (sorted.length === 0) {
    return null;
  }

  return (
    <section className={styles.block} aria-label="Similar proteins">
      <header className={styles.head}>
        <h3 className={styles.heading}>Similar proteins</h3>
      </header>

      <div className={styles.body} ref={bodyRef}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Protein</th>
              <th className={styles.num}>Identity</th>
              <th className={styles.num}>Coverage</th>
              <th className={styles.num}>E-value</th>
              <th>Matched region</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((item) => (
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
    </section>
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
  const best = bestMatch(item.matches);
  const stats = best?.stats ?? null;
  const chains = item.matches.length;
  const organism = organismOf(best?.match);
  const queryLength = best
    ? (sourceSequence(sequences, best.match)?.sequence.length ?? null)
    : null;
  const span = stats
    ? matchedSpan(stats.qstart, stats.qend, stats.qcov, queryLength)
    : null;

  return (
    <>
      <tr
        className={styles.row}
        data-open={expanded ? "true" : undefined}
        onClick={onToggle}
      >
        <td>
          <div className={styles.protein}>
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
            <span className={styles.names}>
              <Link
                className={styles.name}
                href={`/entries/${encodeURIComponent(item.entry.id)}`}
                onClick={(event) => event.stopPropagation()}
              >
                {formatEntryLabel(item.entry)}
              </Link>
              <span className={styles.sub}>
                {organism ? (
                  <span className={styles.organism}>{organism}</span>
                ) : null}
                {organism ? " · " : null}
                {`${chains} ${chains === 1 ? "chain" : "chains"} matched`}
              </span>
            </span>
          </div>
        </td>
        <td
          className={styles.num}
          data-grade={identityGrade(stats?.fident ?? null)}
        >
          {formatPercent(stats?.fident ?? null)}
        </td>
        <td className={styles.num}>{formatPercent(stats?.qcov ?? null)}</td>
        <td className={`${styles.num} ${styles.dim}`}>
          {formatEvalue(stats?.evalue ?? null)}
        </td>
        <td>
          {span ? (
            <span className={styles.spanBar}>
              {/* Drawn hollow when the run recorded no positions: the width
                  comes from coverage and the placement is a guess, so it must
                  not look measured. */}
              <i
                data-approx={span.exact ? undefined : "true"}
                style={{ left: `${span.left}%`, width: `${span.width}%` }}
              />
            </span>
          ) : null}
          {/* Positions only, no chain length: every row is a match against
              this entry's own chains, and repeating their length down the
              column says nothing the viewer above has not already said. */}
          <span className={styles.spanText}>
            {spanLabel(stats?.qstart ?? null, stats?.qend ?? null, null) || "—"}
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

function sourceSequence(
  sequences: ProteinSequence[],
  match: SimilarEntryMatch,
): ProteinSequence | null {
  return (
    sequences.find((sequence) => sequence.id === match.source_sequence_id) ??
    null
  );
}

// The matched chain's own defline names its organism in the last field, the
// way the sync's FASTA records are written. Nothing is shown when the header
// carries only a name.
function organismOf(match: SimilarEntryMatch | undefined): string | null {
  if (!match) {
    return null;
  }
  const fields = deflineTail(match.similar_sequence.header);
  if (fields.length < 2) {
    return null;
  }
  return fields[fields.length - 1].replace(/\s*\(\d+\)\s*$/, "").trim() || null;
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
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

function MoleculeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
