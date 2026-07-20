import { ApiRequestError, listEntries } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import EntriesBrowser from "./components/EntriesBrowser";

import styles from "./page.module.css";

export const dynamic = "force-dynamic";

export default async function Home() {
  const session = await getAuthSession();
  const entries = await loadEntries(session?.token);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser entries={entries} canCreate={session != null} />
      </section>
    </main>
  );
}

// The entries list is public. A missing/invalid session just yields whatever
// the backend returns for an anonymous request (empty on 401), never a gate.
async function loadEntries(token?: string) {
  try {
    return await listEntries(token);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}
