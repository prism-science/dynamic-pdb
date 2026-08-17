import { ApiRequestError, listEntries } from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import EntriesBrowser from "./components/EntriesBrowser";

import styles from "./page.module.css";

export const dynamic = "force-dynamic";

const entriesPageSize = 50;

type SearchParamValue = string | string[] | undefined;

type HomeProps = {
  searchParams?: Promise<Record<string, SearchParamValue>>;
};

export default async function Home({ searchParams }: HomeProps) {
  const session = await getAuthSession();
  const resolvedSearchParams = (await searchParams) ?? {};
  const query = firstQueryValue(resolvedSearchParams.query)?.trim() ?? "";
  const [entries, currentUserId] = await Promise.all([
    loadEntries(session?.token, query),
    getCurrentUserId(),
  ]);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser
          entries={entries}
          canCreate={false}
          query={query}
          currentUserId={currentUserId}
        />
      </section>
    </main>
  );
}

// The entries list is public. A missing/invalid session just yields whatever
// the backend returns for an anonymous request (empty on 401), never a gate.
async function loadEntries(token?: string, query?: string) {
  try {
    return await listEntries(token, {
      query,
      limit: entriesPageSize,
      offset: 0,
    });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}

function firstQueryValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
