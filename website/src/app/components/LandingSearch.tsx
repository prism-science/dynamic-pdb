"use client";

import { useRef } from "react";

import styles from "@/app/landing.module.css";

/**
 * The landing page's search field and its example terms.
 *
 * The examples live inside the form and are laid out in the input's grid
 * column, so they line up under the field and stop where it stops rather than
 * running on under the button. Clicking one types it into the field and leaves
 * the cursor there — it does not submit, so a term can be edited before
 * searching.
 */
export default function LandingSearch({ samples }: { samples: string[] }) {
  const inputRef = useRef<HTMLInputElement>(null);

  return (
    <form className={styles.search} action="/browse" method="get" role="search">
      <input
        ref={inputRef}
        className={styles.searchInput}
        type="search"
        name="query"
        placeholder="Entry ID, PDB ID, protein, ligand, software, or modeler…"
        aria-label="Search the registry"
      />
      <button type="submit" className={styles.searchButton}>
        Search
      </button>

      <p className={styles.samples}>
        {samples.map((term) => (
          <button
            key={term}
            type="button"
            className={styles.sample}
            onClick={() => {
              const input = inputRef.current;
              if (!input) {
                return;
              }
              input.value = term;
              input.focus();
            }}
          >
            {term}
          </button>
        ))}
      </p>
    </form>
  );
}
