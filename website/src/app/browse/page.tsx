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
import { bulkDownloadAvailable } from "@/lib/api/downloads";
import { getAuthSession } from "@/lib/auth/session";
import { canDeleteEntries } from "@/lib/auth/permissions";
import {
  browseSearchHref,
  modelSearchApiUrl,
  modelSearchQuery,
  readModelSearch,
  type ModelSearchCriteria,
  type SearchFailure,
} from "@/lib/model-search";
import EntriesBrowser from "@/app/components/EntriesBrowser";
import DownloadSelectionProvider from "@/app/components/download-selection/DownloadSelectionProvider";
import SelectionBar from "@/app/components/download-selection/SelectionBar";
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
 * restores the search. Anyone may filter by review flags; searches run as the
 * signed-in reader, so reviewers and admins also see which issues a flagged
 * model has.
 *
 * Where the server offers bulk download, model rows have checkboxes, a search
 * can be selected whole, and the selection bar sits at the bottom.
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
  const bulkDownload = await bulkDownloadAvailable();

  const page = (
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
            <ModelSearch
              token={session?.token}
              criteria={criteria}
              textQuery={query}
              selectable={bulkDownload}
            />
          ) : (
            <Entries token={session?.token} query={query} />
          )}
        </section>
      </div>
      {bulkDownload ? <SelectionBar /> : null}
    </main>
  );
  return bulkDownload ? <DownloadSelectionProvider>{page}</DownloadSelectionProvider> : page;
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
  token,
  criteria,
  textQuery,
  selectable,
}: {
  token?: string;
  criteria: ModelSearchCriteria;
  textQuery: string;
  selectable: boolean;
}) {
  const searchQuery = modelSearchQuery(criteria);
  const outcome = await runModelSearch(criteria, token);

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
          <FailureActions failure={outcome.failure} criteria={criteria} />
        </SearchErrorNotice>
      ) : (
        <>
          <ModelResults
            page={outcome.page}
            searchQuery={searchQuery}
            apiUrl={modelSearchApiUrl(getApiBaseUrl(), criteria)}
            selectable={selectable}
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

/**
 * What a reader can do about a failed search. A 400 or 403 fails the same way
 * again, so only the others offer Try again.
 */
function FailureActions({
  failure,
  criteria,
}: {
  failure: SearchFailure;
  criteria: ModelSearchCriteria;
}) {
  const retryHelps = failure.status !== 400 && failure.status !== 403;
  return (
    <>
      {retryHelps ? (
        // A full reload: the search runs again on the server.
        <a href={browseSearchHref(criteria)}>Try again</a>
      ) : null}
      <Link href="/browse">Clear filters</Link>
    </>
  );
}

async function runModelSearch(
  criteria: ModelSearchCriteria,
  token?: string,
): Promise<{ page: ModelSearchPage } | { failure: SearchFailure }> {
  try {
    return { page: await searchModels(criteria, { token }) };
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
