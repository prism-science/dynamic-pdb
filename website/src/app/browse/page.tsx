import Link from "next/link";

import {
  ApiRequestError,
  type Entry,
  listEntriesPage,
  type ListPage,
  type ModelSearchPage,
  searchFailureFromError,
  searchModels,
} from "@/lib/api/entries";
import { getApiBaseUrl } from "@/lib/api/baseUrl";
import { getAuthSession } from "@/lib/auth/session";
import { canDeleteEntries } from "@/lib/auth/permissions";
import {
  modelSearchApiUrl,
  modelSearchQuery,
  readModelSearch,
  type ModelSearchCriteria,
  type SearchFailure,
} from "@/lib/model-search";
import EntriesBrowser from "@/app/components/EntriesBrowser";
import SearchTracker, { ModelSearchTracker } from "@/app/components/SearchTracker";
import FilterPanel from "@/app/components/model-search/FilterPanel";
import ModelResults from "@/app/components/model-search/ModelResults";
import SearchErrorNotice from "@/app/components/model-search/SearchErrorNotice";

import styles from "./browse.module.css";
import { ENTRY_DELETED_PARAM, entryDeletedNotice } from "./deletedNotice";

export const dynamic = "force-dynamic";

const entriesPageSize = 50;

type SearchParamValue = string | string[] | undefined;

type BrowseProps = {
  searchParams?: Promise<Record<string, SearchParamValue>>;
};

/**
 * Every protein in the registry, and the model search.
 *
 * Without a filter in the URL this is the list of entries it has always been,
 * which the header's text search also lands on. With one, it is the model
 * search: one row per model, labelled with its entry, for exactly the filters
 * in the URL (GET /v1/models with the same query string), so a shared URL
 * restores the search.
 */
export default async function Browse({ searchParams }: BrowseProps) {
  const session = await getAuthSession();
  const resolvedSearchParams = (await searchParams) ?? {};
  const query = firstQueryValue(resolvedSearchParams.query)?.trim() ?? "";
  // Only someone who can delete has just deleted; for anyone else the
  // parameter is a hand-made link, and the notice would be a false claim.
  const deletedEntryId =
    session !== null && canDeleteEntries(session.permissions)
      ? firstQueryValue(resolvedSearchParams[ENTRY_DELETED_PARAM])
      : undefined;
  const criteria = readModelSearch(resolvedSearchParams);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      {deletedEntryId ? (
        <p className={styles.deletedNotice} role="status">
          {entryDeletedNotice(deletedEntryId)}
        </p>
      ) : null}
      <div className={styles.layout}>
        <aside className={styles.filters}>
          {/* Remounted per search, so Back and Forward show the URL's filters. */}
          <FilterPanel key={modelSearchQuery(criteria)} criteria={criteria} />
        </aside>
        <section className={styles.entriesShell}>
          {criteria.length > 0 ? (
            <ModelSearch criteria={criteria} textQuery={query} />
          ) : (
            <Entries token={session?.token} query={query} />
          )}
        </section>
      </div>
    </main>
  );
}

async function Entries({ token, query }: { token?: string; query: string }) {
  const firstPage = await loadEntries(token, query);
  return (
    <>
      <EntriesBrowser
        entries={firstPage.items}
        nextCursor={firstPage.nextCursor}
        canCreate={false}
        query={query}
      />
      <SearchTracker query={query} results={firstPage.items.length} />
    </>
  );
}

async function ModelSearch({
  criteria,
  textQuery,
}: {
  criteria: ModelSearchCriteria;
  textQuery: string;
}) {
  const searchQuery = modelSearchQuery(criteria);
  const outcome = await runModelSearch(criteria);

  return (
    <>
      {textQuery ? (
        // Only a hand-made URL holds both: Apply drops the text, the header
        // search drops the filters.
        <p className={styles.textQueryNote} role="note">
          The text search &ldquo;{textQuery}&rdquo; is not applied here: filters search models, the
          text search finds entries.{" "}
          <Link href={`/browse?query=${encodeURIComponent(textQuery)}`}>
            Search &ldquo;{textQuery}&rdquo; alone
          </Link>
        </p>
      ) : null}
      {"failure" in outcome ? (
        <SearchErrorNotice failure={outcome.failure}>
          {outcome.failure.status !== 400 ? (
            // A full reload: the search runs again on the server.
            <a href={`/browse?${searchQuery}`}>Try again</a>
          ) : null}
          <Link href="/browse">Clear filters</Link>
        </SearchErrorNotice>
      ) : (
        <>
          <ModelResults
            page={outcome.page}
            searchQuery={searchQuery}
            apiUrl={modelSearchApiUrl(getApiBaseUrl(), criteria)}
          />
          <ModelSearchTracker
            filters={[...new Set(criteria.map(([parameter]) => parameter))].join(",")}
            results={outcome.page.total?.count ?? outcome.page.items.length}
            capped={outcome.page.total?.exact === false}
          />
        </>
      )}
    </>
  );
}

async function runModelSearch(
  criteria: ModelSearchCriteria,
): Promise<{ page: ModelSearchPage } | { failure: SearchFailure }> {
  try {
    return { page: await searchModels(criteria) };
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return { failure: searchFailureFromError(error) };
    }
    throw error;
  }
}

// The entries list is public. A missing/invalid session just yields whatever
// the backend returns for an anonymous request (empty on 401), never a gate.
async function loadEntries(
  token?: string,
  query?: string,
): Promise<ListPage<Entry>> {
  try {
    return await listEntriesPage(token, { query, limit: entriesPageSize });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return { items: [], nextCursor: null };
    }
    throw error;
  }
}

function firstQueryValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
