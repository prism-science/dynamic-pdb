import Link from "next/link";
import { redirect } from "next/navigation";

import {
  ApiRequestError,
  listUserEntryRevisionGroups,
  type RevisionState,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";

import EntriesBrowser from "../components/EntriesBrowser";
import browserStyles from "../components/EntriesBrowser.module.css";
import styles from "../page.module.css";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

type EntryRevisionListState = Extract<
  RevisionState,
  "active" | "in_review" | "archived"
>;

const TABS: { key: string; label: string; state: EntryRevisionListState }[] = [
  { key: "active", label: "Active", state: "active" },
  { key: "under-review", label: "Under review", state: "in_review" },
  { key: "archived", label: "Archived", state: "archived" },
];

// The Entries area is the signed-in user's own entries, split by status. The
// public catalog stays on the home page and is untouched.
export default async function MyEntriesPage({ searchParams }: Props) {
  const session = await getAuthSession();
  if (!session) {
    redirect("/");
  }
  const userId = userIdFromToken(session.token);
  if (!userId) {
    redirect("/");
  }

  const params = (await searchParams) ?? {};
  const tabKey = firstValue(params.tab) ?? "active";
  const activeTab = TABS.find((tab) => tab.key === tabKey) ?? TABS[0];

  const entries = await loadEntries(session.token, userId, activeTab.state);

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
          infiniteScroll={false}
          tabs={tabs}
          emptyLabel={emptyLabelFor(activeTab.state)}
          title="My entries"
        />
      </section>
    </main>
  );
}

async function loadEntries(
  token: string,
  userId: string,
  state: EntryRevisionListState,
) {
  try {
    const groups = await listUserEntryRevisionGroups(token, userId, state);
    return groups.map((group) => group.entry);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}

function emptyLabelFor(state: EntryRevisionListState): string {
  switch (state) {
    case "in_review":
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
