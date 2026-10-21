import {
  ApiRequestError,
  type Entry,
  listEntriesPage,
  type ListPage,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import EntriesBrowser from "@/app/components/EntriesBrowser";
import SearchTracker from "@/app/components/SearchTracker";

import styles from "./browse.module.css";

export const dynamic = "force-dynamic";

const entriesPageSize = 50;

type SearchParamValue = string | string[] | undefined;

type BrowseProps = {
  searchParams?: Promise<Record<string, SearchParamValue>>;
};

/**
 * Every protein in the registry.
 *
 * This was the site root until the landing page took that slot; the list itself
 * is unchanged, and the header search still lands here with its query.
 */
export default async function Browse({ searchParams }: BrowseProps) {
  const session = await getAuthSession();
  const resolvedSearchParams = (await searchParams) ?? {};
  const query = firstQueryValue(resolvedSearchParams.query)?.trim() ?? "";
  const firstPage = await loadEntries(session?.token, query);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser
          entries={firstPage.items}
          nextCursor={firstPage.nextCursor}
          canCreate={false}
          query={query}
        />
        <SearchTracker query={query} results={firstPage.items.length} />
      </section>
    </main>
  );
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
