import { ApiRequestError, listEntries } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import EntriesBrowser from "./components/EntriesBrowser";

import styles from "./page.module.css";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;

type HomeProps = {
  searchParams?: Promise<Record<string, SearchParamValue>>;
};

export default async function Home({ searchParams }: HomeProps) {
  const session = await getAuthSession();
  const resolvedSearchParams = (await searchParams) ?? {};
  const query = firstQueryValue(resolvedSearchParams.query)?.trim() ?? "";
  const entries = await loadEntries(session?.token, query);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser
          entries={entries}
          canCreate={session != null}
          query={query}
        />
      </section>
    </main>
  );
}

// The entries list is public. A missing/invalid session just yields whatever
// the backend returns for an anonymous request (empty on 401), never a gate.
async function loadEntries(token?: string, query?: string) {
  try {
    return await listEntries(token, { query });
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
