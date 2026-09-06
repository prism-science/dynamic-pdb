import Link from "next/link";
import type { ReactNode } from "react";

import type { Entry } from "@/lib/api/entries";
import { formatEntryLabel } from "@/lib/entry-label";

import EntriesInfiniteScroll from "./EntriesInfiniteScroll";
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

export default function EntriesBrowser({
  entries,
  canCreate = false,
  query = "",
  infiniteScroll = true,
  tabs = null,
  emptyLabel,
}: {
  entries: Entry[];
  canCreate?: boolean;
  query?: string;
  infiniteScroll?: boolean;
  tabs?: ReactNode;
  emptyLabel?: string;
}) {
  return (
    <div className={styles.wrap}>
      {/* No page title here: the list is the whole page, so a "Proteins"
          heading only repeated what the surrounding chrome already says. */}
      {canCreate ? (
        <header className={styles.head}>
          <Link className={styles.addButton} href="/entries/new">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            New entry
          </Link>
        </header>
      ) : null}

      {tabs}

      {/* Search moved to the app header; query is still used for the empty
          state so a fruitless search says so. */}
      {entries.length > 0 ? (
        <ul className={styles.grid}>
          {entries.map((entry) => {
            const updated = formatDate(entry.updated_at);
            return (
              <li key={entry.id} className={styles.cardItem}>
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
                    <span className={styles.cardName}>
                      {formatEntryLabel(entry)}
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
          {query
            ? `No entries match "${query}".`
            : (emptyLabel ?? "No entries found.")}
        </p>
      )}

      {infiniteScroll ? (
        <EntriesInfiniteScroll query={query} initialOffset={entries.length} />
      ) : null}
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
