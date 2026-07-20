"use client";

import Link from "next/link";
import { useMemo, useState } from "react";

import type { Entry } from "@/lib/api/entries";

import styles from "./EntriesBrowser.module.css";

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

function formatDate(value: string): string | null {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : dateFormatter.format(parsed);
}

export default function EntriesBrowser({ entries }: { entries: Entry[] }) {
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) {
      return entries;
    }
    return entries.filter((entry) => {
      const haystack = `${entry.name} ${entry.description ?? ""} ${entry.id}`;
      return haystack.toLowerCase().includes(q);
    });
  }, [entries, query]);

  return (
    <div className={styles.wrap}>
      <header className={styles.head}>
        <h1 className={styles.title}>Proteins</h1>
      </header>

      <div className={styles.searchRow}>
        <svg
          className={styles.searchIcon}
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="none"
          aria-hidden="true"
        >
          <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
          <path
            d="m11 11 3 3"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </svg>
        <input
          className={styles.search}
          type="search"
          placeholder="Search by name, description, etc."
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label="Search"
        />
      </div>

      {filtered.length > 0 ? (
        <ul className={styles.grid}>
          {filtered.map((entry) => {
            const updated = formatDate(entry.updated_at);
            return (
              <li key={entry.id}>
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
                      {entry.description?.trim()
                        ? entry.description
                        : "No description yet."}
                    </span>
                    {updated ? (
                      <span className={styles.cardMeta}>
                        <span className={styles.cardDate}>Updated {updated}</span>
                      </span>
                    ) : null}
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className={styles.empty}>
          {entries.length === 0
            ? "No entries found."
            : `No entries match "${query}".`}
        </p>
      )}
    </div>
  );
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
