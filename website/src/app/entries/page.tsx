import Link from "next/link";
import { redirect } from "next/navigation";

import {
  ApiRequestError,
  listEntries,
  type EntryStatus,
} from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";

import EntriesBrowser from "../components/EntriesBrowser";
import browserStyles from "../components/EntriesBrowser.module.css";
import styles from "../page.module.css";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

const TABS: { key: string; label: string; status: EntryStatus }[] = [
  { key: "active", label: "Active", status: "active" },
  { key: "under-review", label: "Under review", status: "under_review" },
  { key: "archived", label: "Archived", status: "archived" },
];

// The Entries area is the signed-in user's own entries, split by status. The
// public catalog stays on the home page and is untouched.
export default async function MyEntriesPage({ searchParams }: Props) {
  const session = await getAuthSession();
  if (!session) {
    redirect("/");
  }

  const params = (await searchParams) ?? {};
  const tabKey = firstValue(params.tab) ?? "active";
  const activeTab = TABS.find((tab) => tab.key === tabKey) ?? TABS[0];

  const [entries, currentUserId] = await Promise.all([
    loadEntries(session.token, activeTab.status),
    getCurrentUserId(),
  ]);

  const tabs = (
    <nav className={browserStyles.tabs} aria-label="Entry status">
      {TABS.map((tab) => (
        <Link
          key={tab.key}
          href={tab.key === "active" ? "/entries" : `/entries?tab=${tab.key}`}
          className={
            tab.key === activeTab.key
              ? `${browserStyles.tab} ${browserStyles.tabActive}`
              : browserStyles.tab
          }
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  );

  return (
    <main className={styles.page} aria-label="My entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser
          entries={entries}
          canCreate
          currentUserId={currentUserId}
          tabs={tabs}
          emptyLabel={emptyLabelFor(activeTab.status)}
          title="My entries"
        />
      </section>
    </main>
  );
}

async function loadEntries(token: string, status: EntryStatus) {
  try {
    return await listEntries(token, { status, mine: true });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}

function emptyLabelFor(status: EntryStatus): string {
  switch (status) {
    case "under_review":
      return "You have no entries in review.";
    case "archived":
      return "Nothing archived.";
    default:
      return "You have not published any entries yet.";
  }
}

function firstValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
