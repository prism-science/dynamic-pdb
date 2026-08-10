"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import DeleteButton from "./DeleteButton";
import styles from "./EntriesBrowser.module.css";

const pageSize = 50;

type Entry = {
  id: string;
  created_by: string;
  name: string;
  description: string | null;
  thumbnail_image_url: string | null;
  created_at: string;
  updated_at: string;
};

type EntriesPage = {
  items: Entry[];
};

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

export default function EntriesInfiniteScroll({
  query,
  initialOffset,
  currentUserId,
}: {
  query: string;
  initialOffset: number;
  currentUserId: string | null;
}) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [offset, setOffset] = useState(initialOffset);
  const [hasMore, setHasMore] = useState(initialOffset === pageSize);
  const [loading, setLoading] = useState(false);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    setEntries([]);
    setOffset(initialOffset);
    setHasMore(initialOffset === pageSize);
    setLoading(false);
  }, [initialOffset, query]);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) {
      return;
    }
    setLoading(true);
    try {
      const params = new URLSearchParams({
        limit: String(pageSize),
        offset: String(offset),
      });
      if (query.trim()) {
        params.set("query", query.trim());
      }
      const response = await fetch(`/entries/feed?${params.toString()}`, {
        cache: "no-store",
      });
      if (!response.ok) {
        setHasMore(false);
        return;
      }
      const page = (await response.json()) as EntriesPage;
      setEntries((current) => [...current, ...page.items]);
      setOffset((current) => current + page.items.length);
      setHasMore(page.items.length === pageSize);
    } finally {
      setLoading(false);
    }
  }, [hasMore, loading, offset, query]);

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
      { rootMargin: "600px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);

  return (
    <>
      {entries.length > 0 ? (
        <ul className={styles.grid}>
          {entries.map((entry) => (
            <EntryCard
              key={entry.id}
              entry={entry}
              canDelete={currentUserId != null && entry.created_by === currentUserId}
            />
          ))}
        </ul>
      ) : null}
      {hasMore ? (
        <div
          ref={sentinelRef}
          className={styles.loadSentinel}
          aria-hidden="true"
        />
      ) : null}
    </>
  );
}

function EntryCard({
  entry,
  canDelete,
}: {
  entry: Entry;
  canDelete: boolean;
}) {
  const updated = formatDate(entry.updated_at);

  return (
    <li
      className={styles.cardItem}
      data-has-delete={canDelete ? "true" : undefined}
    >
      <Link className={styles.card} href={`/entries/${entry.id}`}>
        <span
          className={styles.thumb}
          data-empty={entry.thumbnail_image_url ? undefined : "true"}
        >
          {entry.thumbnail_image_url ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={entry.thumbnail_image_url} alt="" loading="lazy" />
          ) : (
            <MoleculeIcon />
          )}
        </span>
        <span className={styles.cardBody}>
          <span className={styles.cardName}>{entry.name}</span>
          <span className={styles.cardDesc}>
            {entry.description?.trim() ? entry.description : "No description yet."}
          </span>
          {updated ? (
            <span className={styles.cardMeta}>
              <span className={styles.cardDate}>Updated {updated}</span>
            </span>
          ) : null}
        </span>
      </Link>
      {canDelete ? (
        <div className={styles.cardDelete}>
          <DeleteButton
            action={() => deleteEntry(entry.id)}
            itemName={entry.name}
            itemKind="entry"
          />
        </div>
      ) : null}
    </li>
  );
}

async function deleteEntry(entryId: string): Promise<{ error: string } | void> {
  const response = await fetch(`/entries/${encodeURIComponent(entryId)}/delete`, {
    method: "DELETE",
  });
  if (!response.ok) {
    return { error: "Failed to delete the entry." };
  }
}

function formatDate(value: string): string | null {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : dateFormatter.format(parsed);
}

function MoleculeIcon() {
  return (
    <svg width="38" height="38" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
