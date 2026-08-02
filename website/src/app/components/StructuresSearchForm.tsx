"use client";

import type { ChangeEvent } from "react";
import { useRouter } from "next/navigation";

import styles from "./StructuresBrowser.module.css";

export default function StructuresSearchForm({ query = "" }: { query?: string }) {
  const router = useRouter();
  const hasActiveQuery = query.trim().length > 0;

  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (hasActiveQuery && event.currentTarget.value === "") {
      router.replace("/");
    }
  };

  return (
    <form className={styles.searchRow} action="/" method="get">
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
        name="query"
        placeholder="Search — e.g. 2OU8, lysozyme…"
        defaultValue={query}
        aria-label="Search"
        onChange={handleChange}
      />
      <button className={styles.searchButton} type="submit">
        Search
      </button>
    </form>
  );
}
