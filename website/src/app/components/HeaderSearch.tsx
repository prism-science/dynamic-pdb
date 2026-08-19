"use client";

import { useRouter, useSearchParams } from "next/navigation";

import styles from "./AppHeader.module.css";

// Search sits with the account controls as a narrow field that widens on focus.
// The widening is pure CSS (:focus-within), so there is no open/close state to
// keep in sync. It is still a plain GET form, now aimed at /browse —
// submitting from anywhere means "search the registry", and lands on the
// list with results.
export default function HeaderSearch() {
  const router = useRouter();
  const query = useSearchParams().get("query")?.trim() ?? "";

  return (
    <form
      className={styles.search}
      // An active query holds the field open after focus leaves: collapsing it
      // would hide the term the results on screen are answering.
      data-active={query ? "true" : undefined}
      action="/browse"
      method="get"
      role="search"
    >
      <input
        // Remount on query change: the input is uncontrolled, so without this
        // the value would keep whatever was typed after a back/forward.
        key={query}
        className={styles.searchInput}
        type="search"
        name="query"
        defaultValue={query}
        placeholder="Search"
        aria-label="Search entries"
      />

      {query ? (
        <button
          type="button"
          className={styles.searchClear}
          onClick={() => router.push("/browse")}
          aria-label="Clear search"
          title="Clear search"
        >
          ×
        </button>
      ) : null}

      <button type="submit" className={styles.searchSubmit} aria-label="Search">
        <SearchIcon />
      </button>
    </form>
  );
}

function SearchIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="m11 11 3 3"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}
