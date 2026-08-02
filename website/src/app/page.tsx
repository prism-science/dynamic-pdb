import { ApiRequestError, listStructures } from "@/lib/api/structures";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import StructuresBrowser from "./components/StructuresBrowser";

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
  const [structures, currentUserId] = await Promise.all([
    loadStructures(session?.token, query),
    getCurrentUserId(),
  ]);

  return (
    <main className={styles.page} aria-label="dynamic-pdb structures">
      <section className={styles.structuresShell}>
        <StructuresBrowser
          structures={structures}
          canCreate={session != null}
          query={query}
          currentUserId={currentUserId}
        />
      </section>
    </main>
  );
}

// The structures list is public. A missing/invalid session just yields whatever
// the backend returns for an anonymous request (empty on 401), never a gate.
async function loadStructures(token?: string, query?: string) {
  try {
    return await listStructures(token, { query });
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
