import type { ModelSearchPage } from "@/lib/api/entries";
import { modelTotalLine } from "@/lib/model-search";

import SelectAllResults from "@/app/components/download-selection/SelectAllResults";
import SelectModelCheckbox from "@/app/components/download-selection/SelectModelCheckbox";
import { selectedModelFromResult } from "@/lib/download-selection";

import CopyLinkButton from "./CopyLinkButton";
import ModelResultRow from "./ModelResultRow";
import ModelResultsScroll from "./ModelResultsScroll";
import SearchHelp from "./SearchHelp";
import styles from "./model-search.module.css";

/**
 * The first page of a model search, its total and the same search as an API
 * URL, then the scroll that loads the rest. Where the server offers bulk
 * download (selectable), each row has a checkbox and the total offers "Select
 * all results", the whole search rather than the page.
 */
export default function ModelResults({
  page,
  searchQuery,
  apiUrl,
  selectable = false,
}: {
  page: ModelSearchPage;
  /** The search's query string, shared by this page's URL and the API's. */
  searchQuery: string;
  apiUrl: string;
  selectable?: boolean;
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
        {selectable ? (
          <SelectAllResults
            searchQuery={searchQuery}
            count={page.total?.count ?? null}
            countExact={page.total?.exact ?? false}
          />
        ) : null}
      </div>

      {page.items.length > 0 ? (
        <ol className={styles.rows}>
          {page.items.map((result) => (
            <ModelResultRow
              key={result.id}
              result={result}
              leading={
                selectable ? (
                  <SelectModelCheckbox model={selectedModelFromResult(result)} searchQuery={searchQuery} />
                ) : undefined
              }
            />
          ))}
        </ol>
      ) : (
        // An empty result is where a missing value surprises most, so the
        // help opens here.
        <SearchHelp open />
      )}

      <ModelResultsScroll searchQuery={searchQuery} initialCursor={page.nextCursor} selectable={selectable} />
    </section>
  );
}
