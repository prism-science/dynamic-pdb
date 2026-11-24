"use client";

import {
  addWholeSearch,
  hasWholeSearch,
  removeWholeSearch,
  wholeSearchFromQuery,
} from "@/lib/download-selection";

import { useDownloadSelection } from "./DownloadSelectionProvider";
import styles from "./download-selection.module.css";

/**
 * "Select all results": the whole search, every page, not the rows on screen.
 * The selection keeps the search itself; saving it holds every model the
 * search matches at that moment.
 */
export default function SelectAllResults({
  searchQuery,
  count,
  countExact,
}: {
  searchQuery: string;
  /** The search's total; null when the API did not count it. */
  count: number | null;
  countExact: boolean;
}) {
  const context = useDownloadSelection();
  if (context === null || count === 0) {
    return null;
  }
  const selected = hasWholeSearch(context.selection, searchQuery);
  const single = count === 1 && countExact;
  const countText =
    count === null ? "" : ` ${count.toLocaleString("en-US")}${countExact ? "" : "+"}`;
  if (selected) {
    return (
      <p className={styles.selectAll} role="status">
        <span className={styles.selectAllDone}>
          {single ? "The 1 result is selected." : `All${countText} results are selected.`}
        </span>{" "}
        <button
          type="button"
          className={styles.linkButton}
          disabled={!context.ready}
          onClick={() => context.update((selection) => removeWholeSearch(selection, searchQuery))}
        >
          Unselect all
        </button>
      </p>
    );
  }
  return (
    <p className={styles.selectAll}>
      <button
        type="button"
        className={styles.secondaryButton}
        disabled={!context.ready}
        onClick={() =>
          context.apply((selection) =>
            addWholeSearch(selection, wholeSearchFromQuery(searchQuery, count ?? 0, count !== null && countExact)),
          )
        }
      >
        {single ? "Select the 1 result" : `Select all${countText} results`}
      </button>
    </p>
  );
}
