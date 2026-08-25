import Link from "next/link";
import { redirect } from "next/navigation";

import {
  ApiRequestError,
  findUserSubmissionItem,
  getUserSubmission,
  listUserSubmissions,
  type EntryReview,
  type ReviewQueueItem,
  type ReviewQueuePage,
  type RevisionState,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";
import ReviewQueue from "@/app/review/ReviewQueue";
import SubmissionCard from "@/app/review/submission-view";

import styles from "@/app/review/review-inbox.module.css";
import { SUBMISSION_TABS, tabFor, type SubmissionTab } from "./submissionTabs";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

/** The author's side of review: the same two-pane reading of a submission the
 *  reviewer gets, minus the decision. Published entries are not here —
 *  those are the public catalog. */
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
  const tab = tabFor(firstValue(params.tab));
  const requested = firstValue(params.sel);

  const [queue, requestedItem] = await Promise.all([
    loadQueue(session.token, userId, tab.state),
    requested
      ? loadItem(session.token, userId, tab.state, requested)
      : Promise.resolve(null),
  ]);
  const selected = requestedItem ?? queue.items[0] ?? null;
  const submission = selected
    ? await loadSubmission(session.token, userId, selected)
    : null;

  return (
    <main className={styles.page} aria-label="My entries">
      <ReviewQueue
        key={tab.key}
        initialItems={queue.items}
        initialHasMore={queue.hasMore}
        selectedEntryId={selected?.entry_id ?? null}
        headingSlot={<Tabs current={tab} />}
        feedPath={`/entries/feed?state=${encodeURIComponent(tab.state)}`}
        hrefPath={
          tab.key === SUBMISSION_TABS[0].key
            ? "/entries"
            : `/entries?tab=${encodeURIComponent(tab.key)}`
        }
        emptyLabel={tab.empty}
      />

      <div className={`${styles.col} ${styles.preview}`}>
        {selected && submission ? (
          <SubmissionCard
            title={selected.name}
            review={submission}
            reviewPermissions={null}
          />
        ) : (
          <div className={styles.emptyPane}>Select a submission.</div>
        )}
      </div>
    </main>
  );
}

function Tabs({ current }: { current: SubmissionTab }) {
  return (
    <div className={styles.tabs} role="tablist">
      {SUBMISSION_TABS.map((tab) => (
        <Link
          key={tab.key}
          href={tab.key === SUBMISSION_TABS[0].key ? "/entries" : `/entries?tab=${tab.key}`}
          role="tab"
          aria-selected={tab.key === current.key}
          className={`${styles.tab} ${
            tab.key === current.key ? styles.tabOn : ""
          }`}
        >
          {tab.label}
        </Link>
      ))}
    </div>
  );
}

async function loadQueue(
  token: string,
  userId: string,
  state: RevisionState,
): Promise<ReviewQueuePage> {
  try {
    return await listUserSubmissions(token, userId, state, {
      limit: REVIEW_PAGE_SIZE,
    });
  } catch (error) {
    if (error instanceof ApiRequestError) return { items: [], hasMore: false };
    throw error;
  }
}

async function loadItem(
  token: string,
  userId: string,
  state: RevisionState,
  entryId: string,
): Promise<ReviewQueueItem | null> {
  try {
    return await findUserSubmissionItem(token, userId, state, entryId);
  } catch (error) {
    if (error instanceof ApiRequestError) return null;
    throw error;
  }
}

async function loadSubmission(
  token: string,
  userId: string,
  item: ReviewQueueItem,
): Promise<EntryReview | null> {
  try {
    return await getUserSubmission(token, userId, item);
  } catch (error) {
    if (error instanceof ApiRequestError) return null;
    throw error;
  }
}


function firstValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
