import Link from "next/link";

import styles from "@/app/landing.module.css";

/**
 * The landing page's search field and its example terms.
 *
 * The examples live inside the form and are laid out in the input's grid
 * column, so they line up under the field and stop where it stops rather than
 * running on under the button.
 *
 * Each one is a link to the same list the field submits to, so clicking a term
 * runs that search rather than typing it — the design asks for the page you
 * would have got had you typed it. Links rather than buttons because that is
 * what they behave like: they can be middle-clicked, opened in a new tab, and
 * their destination shows in the status bar before the click. The field on the
 * far side is not left empty either — the header's search reads the term back
 * out of the URL — so the search that ran is still on screen and editable.
 *
 * Nothing here needs the client any more: the form is a plain GET, and a link
 * is a link, so the whole component renders on the server.
 */
export default function LandingSearch({ samples }: { samples: string[] }) {
  return (
    <form className={styles.search} action="/browse" method="get" role="search">
      <input
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
          <Link
            key={term}
            className={styles.sample}
            href={`/browse?query=${encodeURIComponent(term)}`}
          >
            {term}
          </Link>
        ))}
      </p>
    </form>
  );
}
