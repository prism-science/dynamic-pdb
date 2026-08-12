"use client";

import { useState } from "react";
import Link from "next/link";

import type { ProteinSequence, SimilarEntry } from "@/lib/api/entries";
import { bestMatchStats, formatPercent, sortedByScore } from "@/lib/similarity";
import SimilarEntriesModal from "./SimilarEntriesModal";

import styles from "./SimilarProteins.module.css";

/* The rail shows this many hits; the rest live behind "All similar". Five
   keeps the sidebar shorter than the content column it annotates. */
const RAIL_LIMIT = 5;

/**
 * "Similar proteins" block for the entry sidebar: the top hits as compact
 * links, and a modal with the full list and per-chain alignments.
 */
export default function SimilarProteins({
  entryName,
  sequences,
  items,
}: {
  entryName: string;
  /** The entry's own sequences: matches reference them by id, and the
   *  alignment view needs their headers and lengths for the query side. */
  sequences: ProteinSequence[];
  items: SimilarEntry[];
}) {
  const [open, setOpen] = useState(false);
  const sorted = sortedByScore(items);
  const top = sorted.slice(0, RAIL_LIMIT);

  return (
    <div className={styles.block}>
      <h3 className={styles.heading}>Similar proteins</h3>

      {top.length === 0 ? (
        // New sequences are matched in the background, so an empty list most
        // often means "not yet" rather than "nothing alike exists".
        <p className={styles.empty}>No similar proteins found yet.</p>
      ) : (
        <>
          <div className={styles.list}>
            {top.map((item) => (
              <RailItem key={item.entry.id} item={item} />
            ))}
          </div>
          <button
            type="button"
            className={styles.more}
            onClick={() => setOpen(true)}
          >
            All similar ({sorted.length}) →
          </button>
        </>
      )}

      {open ? (
        <SimilarEntriesModal
          entryName={entryName}
          sequences={sequences}
          items={sorted}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

function RailItem({ item }: { item: SimilarEntry }) {
  const stats = bestMatchStats(item.matches);
  const chains = item.matches.length;

  return (
    <Link
      className={styles.item}
      href={`/entries/${encodeURIComponent(item.entry.id)}`}
    >
      <span
        className={styles.thumb}
        data-empty={item.entry.thumbnail_image_url ? undefined : "true"}
      >
        {item.entry.thumbnail_image_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={item.entry.thumbnail_image_url} alt="" loading="lazy" />
        ) : (
          <MoleculeIcon />
        )}
      </span>
      <span className={styles.body}>
        <span className={styles.name}>{item.entry.name}</span>
        <span className={styles.sub}>
          {stats.fident != null ? (
            <>
              <span
                className={styles.id}
                data-strong={stats.fident >= 0.6 ? "true" : undefined}
              >
                {formatPercent(stats.fident)} id
              </span>
              {" · "}
            </>
          ) : null}
          {chains} {chains === 1 ? "chain" : "chains"}
        </span>
      </span>
    </Link>
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
