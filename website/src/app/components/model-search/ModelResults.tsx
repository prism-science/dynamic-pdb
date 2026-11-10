import type { ModelSearchPage } from "@/lib/api/entries";
import { modelTotalLine } from "@/lib/model-search";

import CopyLinkButton from "./CopyLinkButton";
import ModelResultRow from "./ModelResultRow";
import ModelResultsScroll from "./ModelResultsScroll";
import SearchHelp from "./SearchHelp";
import styles from "./model-search.module.css";

/**
 * The first page of a model search, its total and the same search as an API
 * URL, then the scroll that loads the rest.
 */
export default function ModelResults({
  page,
  searchQuery,
  apiUrl,
}: {
  page: ModelSearchPage;
  /** The search's query string, shared by this page's URL and the API's. */
  searchQuery: string;
  apiUrl: string;
}) {
  return (
    <section className={styles.results} aria-label="Model search results">
      <div className={styles.summary}>
        <p className={styles.count} role="status">
          {page.total === null ? "Models matching these filters" : modelTotalLine(page.total)}
        </p>
        <p className={styles.apiLine}>
          <span className={styles.apiLabel}>Same search in the API:</span>{" "}
          <a className={styles.apiUrl} href={apiUrl} target="_blank" rel="noreferrer">
            GET {apiUrl}
          </a>{" "}
          <CopyLinkButton value={apiUrl} />
        </p>
      </div>

      {page.items.length > 0 ? (
        <ol className={styles.rows}>
          {page.items.map((result) => (
            <ModelResultRow key={result.id} result={result} />
          ))}
        </ol>
      ) : (
        // An empty result is where a missing value surprises most, so the
        // help opens here.
        <SearchHelp open />
      )}

      <ModelResultsScroll searchQuery={searchQuery} initialCursor={page.nextCursor} />
    </section>
  );
}
