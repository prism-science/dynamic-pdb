"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import type { ReviewQueueItem, ReviewQueuePage } from "@/lib/api/entries";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";

import styles from "./review-inbox.module.css";

/** The queue is unbounded, so the column loads a page at a time as it scrolls.
 *  The sentinel is watched against the column itself, not the viewport: the
 *  column is the thing that scrolls. */
export default function ReviewQueue({
  initialItems,
  initialHasMore,
  selectedEntryId,
  heading,
  headingSlot,
  feedPath,
  hrefPath,
  emptyLabel = "Nothing is waiting for your review.",
}: {
  initialItems: ReviewQueueItem[];
  initialHasMore: boolean;
  selectedEntryId: string | null;
  /** Column heading. Omitted when headingSlot carries the tabs instead. */
  heading?: string;
  headingSlot?: React.ReactNode;
  /** Where further pages come from, query string included. */
  feedPath: string;
  /** Where a row links to; `sel` is appended. A path, not a callback: a server
   *  component cannot hand a function to a client one. */
  hrefPath: string;
  emptyLabel?: string;
}) {
  const [items, setItems] = useState(initialItems);
  const [hasMore, setHasMore] = useState(initialHasMore);
  const [loading, setLoading] = useState(false);
  const columnRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  // Clicking a row re-renders the server component, handing back a fresh (but
  // identical) first page. Resetting on the array identity would throw away
  // everything scrolled so far on every click, so the reset is keyed on the
  // content: it fires when the queue actually changed, e.g. after a decision.
  const signature = initialItems.map((item) => item.entry_id).join(",");
  const appliedSignature = useRef(signature);
  useEffect(() => {
    if (appliedSignature.current === signature) {
      return;
    }
    appliedSignature.current = signature;
    setItems(initialItems);
    setHasMore(initialHasMore);
  }, [signature, initialItems, initialHasMore]);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) {
      return;
    }
    setLoading(true);
    try {
      const separator = feedPath.includes("?") ? "&" : "?";
      const params = new URLSearchParams({
        limit: String(REVIEW_PAGE_SIZE),
        offset: String(items.length),
      });
      const response = await fetch(`${feedPath}${separator}${params.toString()}`, {
        cache: "no-store",
      });
      if (!response.ok) {
        setHasMore(false);
        return;
      }
      const page = (await response.json()) as ReviewQueuePage;
      // A row can arrive twice if the queue shifted between pages; keying on the
      // entry id keeps the column from duplicating it.
      setItems((current) => {
        const seen = new Set(current.map((item) => item.entry_id));
        return [
          ...current,
          ...page.items.filter((item) => !seen.has(item.entry_id)),
        ];
      });
      setHasMore(page.hasMore);
    } finally {
      setLoading(false);
    }
  }, [feedPath, hasMore, items.length, loading]);

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
      { root: columnRef.current, rootMargin: "300px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);

  return (
    <div className={`${styles.col} ${styles.list}`} ref={columnRef}>
      <div className={styles.listHead}>
        {headingSlot ?? <h2 className={styles.listTitle}>{heading}</h2>}
      </div>

      {items.length === 0 ? (
        <p className={styles.empty}>{emptyLabel}</p>
      ) : (
        items.map((item) => (
          <Link
            key={item.entry_id}
            href={rowHref(hrefPath, item.entry_id)}
            className={`${styles.row} ${
              item.entry_id === selectedEntryId ? styles.rowSel : ""
            }`}
          >
            <div className={styles.rowName}>{item.name}</div>
            <div className={styles.rowMeta}>
              submitted {formatDate(item.submitted_at)}
            </div>
          </Link>
        ))
      )}

      {hasMore ? (
        <div ref={sentinelRef} className={styles.sentinel} aria-hidden="true">
          {loading ? "Loading…" : null}
        </div>
      ) : null}
    </div>
  );
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toISOString().slice(0, 16).replace("T", " ") + " UTC";
}

function rowHref(hrefPath: string, entryId: string): string {
  const separator = hrefPath.includes("?") ? "&" : "?";
  return `${hrefPath}${separator}sel=${encodeURIComponent(entryId)}`;
}
