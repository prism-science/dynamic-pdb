import {
  ApiRequestError,
  findReviewItem,
  getEntryReview,
  listReviews,
  type EntryReview,
  type ReviewQueueItem,
  type ReviewQueuePage,
} from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import { isConfiguredAdmin } from "@/app/reviews/admin";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";
import ReviewQueue from "./ReviewQueue";
import SubmissionCard from "./submission-view";

import styles from "./review-inbox.module.css";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

export default async function ReviewInbox({ searchParams }: Props) {
  const session = await getAuthSession();
  if (!session) {
    return <Shell>Sign in to review.</Shell>;
  }

  const userId = await getCurrentUserId();
  if (!isConfiguredAdmin(userId)) {
    return <Shell>You don&apos;t have review access.</Shell>;
  }

  const params = (await searchParams) ?? {};
  const requested = firstValue(params.sel);

  const [queue, requestedItem] = await Promise.all([
    loadQueue(session.token),
    // A deep link can point at a row far down the queue, past what the column
    // has loaded, so the card is resolved by id rather than from the page.
    requested ? loadItem(session.token, requested) : Promise.resolve(null),
  ]);
  const selected = requestedItem ?? queue.items[0] ?? null;
  const review = selected
    ? await loadReview(session.token, selected)
    : null;

  return (
    <main className={styles.page} aria-label="Review inbox">
      <ReviewQueue
        initialItems={queue.items}
        initialHasMore={queue.hasMore}
        selectedEntryId={selected?.entry_id ?? null}
        heading="To review"
        feedPath="/review/feed"
        hrefPath="/review"
      />

      <div className={`${styles.col} ${styles.preview}`}>
        {selected && review ? (
          <SubmissionCard
            title={selected.name}
            review={review}
            decidable
          />
        ) : (
          <div className={styles.emptyPane}>Select an entry to review.</div>
        )}
      </div>
    </main>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <main className={styles.notice}>
      <p className={styles.empty}>{children}</p>
    </main>
  );
}

async function loadQueue(token: string): Promise<ReviewQueuePage> {
  try {
    return await listReviews(token, { limit: REVIEW_PAGE_SIZE });
  } catch (error) {
    if (error instanceof ApiRequestError) return { items: [], hasMore: false };
    throw error;
  }
}

async function loadItem(
  token: string,
  entryId: string,
): Promise<ReviewQueueItem | null> {
  try {
    return await findReviewItem(token, entryId);
  } catch (error) {
    if (error instanceof ApiRequestError) return null;
    throw error;
  }
}

async function loadReview(
  token: string,
  item: ReviewQueueItem,
): Promise<EntryReview | null> {
  try {
    return await getEntryReview(token, item);
  } catch (error) {
    if (error instanceof ApiRequestError) return null;
    throw error;
  }
}




function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toISOString().slice(0, 16).replace("T", " ") + " UTC";
}

function firstValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
