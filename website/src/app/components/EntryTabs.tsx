import Link from "next/link";

import styles from "./EntryTabs.module.css";

export const OVERVIEW_TAB = "overview";

export type EntryTabDescriptor = {
  id: string;
  label: string;
};

/**
 * The tab strip, for whatever the page is currently about.
 *
 * It takes the path it hangs off rather than an entry id, because the entry and
 * one of its models are the same kind of page with the same tabs -- only the
 * base of the URL and the list of tabs differ.
 */
export default function EntryTabs({
  base,
  tabs,
  active,
}: {
  /** The scope's own path, e.g. `/entries/5GY3` or `/entries/5GY3/models/x`. */
  base: string;
  tabs: EntryTabDescriptor[];
  active: string;
}) {
  return (
    <nav className={styles.tabs} aria-label="Sections">
      {tabs.map((tab) => {
        const current = tab.id === active;
        return (
          <Link
            key={tab.id}
            className={styles.tab}
            href={tab.id === OVERVIEW_TAB ? base : `${base}?tab=${tab.id}`}
            prefetch={false}
            data-current={current ? "true" : undefined}
            aria-current={current ? "page" : undefined}
            scroll={false}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}

/**
 * Which tab to render: the requested one when this scope has it, its overview
 * otherwise.
 *
 * A link to a tab the scope does not have -- an old bookmark, a deposit that
 * lost its crystallography, a tab carried over from the other scope -- lands on
 * the overview rather than on nothing.
 */
export function resolveTab(
  tabs: EntryTabDescriptor[],
  requested: string | undefined,
): string {
  return tabs.some((tab) => tab.id === requested)
    ? (requested as string)
    : OVERVIEW_TAB;
}
