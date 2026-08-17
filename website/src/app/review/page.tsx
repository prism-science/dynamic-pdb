import {
  ApiRequestError,
  findReviewItem,
  getEntryReview,
  listReviews,
  type EntryReview,
  type ReviewEntry,
  type ReviewModel,
  type ReviewQueueItem,
  type ReviewQueuePage,
  type RevisionTarget,
} from "@/lib/api/entries";
import {
  diffEntry,
  diffModel,
  inlineDiff,
  type DiffRow,
  type DiffSection,
} from "@/lib/reviewDiff";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import ReviewDecision from "@/app/reviews/ReviewDecision";
import { isConfiguredAdmin } from "@/app/reviews/admin";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";
import ReviewQueue from "./ReviewQueue";

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
      <div className={styles.shell}>
        <div className={styles.inbox}>
          <ReviewQueue
            initialItems={queue.items}
            initialHasMore={queue.hasMore}
            selectedEntryId={selected?.entry_id ?? null}
          />

          <div className={`${styles.col} ${styles.preview}`}>
            {selected && review ? (
              <Card item={selected} review={review} />
            ) : (
              <div className={styles.emptyPane}>Select an entry to review.</div>
            )}
          </div>
        </div>
      </div>
    </main>
  );
}

function Card({
  item,
  review,
}: {
  item: ReviewQueueItem;
  review: EntryReview;
}) {
  return (
    <div className={styles.pvScroll}>
      <div className={styles.pvTitle}>{item.name}</div>

      <Changes review={review} />
    </div>
  );
}

/** What is being decided: only what was actually submitted. Every block carries
 *  its own decision — the entry and each model are approved or rejected
 *  separately. */
function Changes({ review }: { review: EntryReview }) {
  // No published revision behind the entry means nothing for a model to go live
  // under, and the backend refuses it. Same condition the "new" badge reads, so
  // what the block says and what its buttons do cannot drift apart.
  const entryUnpublished = review.entry !== null && review.entry.active === null;
  const modelsBlockedReason = entryUnpublished
    ? "Approve the entry first — it has no published revision yet."
    : null;

  return (
    <>
      {review.entry ? (
        review.entry.proposed.entry_state === "deleted" ? (
          <DeletionBlock
            title="Entry"
            what="entry"
            target={review.entry.target}
            removes={entryRemovals(review.entry.active)}
            publishedHref={`/entries/${encodeURIComponent(review.entry_id)}`}
          />
        ) : (
          <DiffBlock
            title="Entry"
            badge={review.entry.active ? "revision" : "new"}
            sections={diffEntry(review.entry.active, review.entry.proposed)}
            target={review.entry.target}
            previewHref={`/review/preview/${encodeURIComponent(
              review.entry_id,
            )}/${encodeURIComponent(review.entry.target.revision_id)}`}
          />
        )
      ) : null}

      {review.models.length > 0 ? (
        <div className={styles.sect}>
          <span className={styles.sectH}>
            {review.entry ? "Models in review" : "Models"}
          </span>
          <span className={styles.sectLine} />
        </div>
      ) : null}

      {review.models.map((model) =>
        model.proposed.model_state === "deleted" ? (
          <DeletionBlock
            key={model.model_id}
            title={model.proposed.name}
            what="model"
            target={model.target}
            removes={modelRemovals(model.active)}
            publishedHref={`/entries/${encodeURIComponent(
              review.entry_id,
            )}/models/${encodeURIComponent(model.model_id)}`}
            blockedReason={modelsBlockedReason}
          />
        ) : (
          <DiffBlock
            key={model.model_id}
            title={model.proposed.name}
            badge={model.active ? "revision" : "new"}
            sections={diffModel(model.active, model.proposed)}
            target={model.target}
            blockedReason={modelsBlockedReason}
            previewHref={`/review/preview/${encodeURIComponent(
              review.entry_id,
            )}/models/${encodeURIComponent(
              model.model_id,
            )}/${encodeURIComponent(model.target.revision_id)}`}
          />
        ),
      )}
    </>
  );
}

/** A deletion has nothing to diff: every field of the published revision is on
 *  its way out. What a reviewer needs is what disappears and why. */
function DeletionBlock({
  title,
  what,
  target,
  removes,
  publishedHref,
  blockedReason,
}: {
  title: string;
  what: "entry" | "model";
  target: RevisionTarget;
  removes: string[];
  publishedHref: string;
  blockedReason?: string | null;
}) {
  return (
    <Block
      title={title}
      badge="deletion"
      badgeTone="danger"
      previewHref={publishedHref}
      previewLabel="Open published"
      target={target}
      blockedReason={blockedReason}
    >
      {blockedReason ? (
        <div className={styles.blocked}>{blockedReason}</div>
      ) : null}
      <div className={styles.deleting}>
        {what === "entry"
          ? "Approving this takes the entry out of the catalog."
          : "Approving this takes the model off its entry."}
      </div>
      {removes.length > 0 ? (
        <div className={styles.dgroup}>
          <div className={styles.dgroupH}>Goes away</div>
          {removes.map((item) => (
            <div key={item} className={`${styles.drow} ${styles.removed}`}>
              <div className={styles.dk}>
                <span className={styles.sign}>−</span>
                {item}
              </div>
              <div className={styles.dv} />
            </div>
          ))}
        </div>
      ) : null}
    </Block>
  );
}

function entryRemovals(active: ReviewEntry | null): string[] {
  if (!active) return [];
  const items = active.artifacts.map((artifact) => artifact.name);
  const sequences = active.protein_sequences.length;
  if (sequences > 0) {
    items.push(`${sequences} protein sequence${sequences === 1 ? "" : "s"}`);
  }
  return items;
}

function modelRemovals(active: ReviewModel | null): string[] {
  if (!active) return [];
  const items = active.artifacts.map((artifact) => artifact.name);
  if (active.metrics.length > 0) {
    items.push(
      `${active.metrics.length} metric${active.metrics.length === 1 ? "" : "s"}`,
    );
  }
  return items;
}

function DiffBlock({
  title,
  badge,
  sections,
  target,
  previewHref,
  blockedReason,
}: {
  title: string;
  badge: string;
  sections: DiffSection[];
  target: RevisionTarget;
  previewHref: string;
  blockedReason?: string | null;
}) {
  const visible = sections
    .map((section) => ({
      title: section.title,
      rows: section.rows.filter((row) => row.state !== "same"),
    }))
    .filter((section) => section.rows.length > 0);

  return (
    <Block
      title={title}
      badge={badge}
      previewHref={previewHref}
      target={target}
      blockedReason={blockedReason}
    >
      {blockedReason ? (
        <div className={styles.blocked}>{blockedReason}</div>
      ) : null}
      {visible.length === 0 ? (
        <div className={styles.note}>Nothing changed here.</div>
      ) : (
        visible.map((section) => (
          <div key={section.title} className={styles.dgroup}>
            <div className={styles.dgroupH}>{section.title}</div>
            {section.rows.map((row) => (
              <Row key={`${section.title}:${row.label}`} row={row} />
            ))}
          </div>
        ))
      )}
    </Block>
  );
}

function Block({
  title,
  badge,
  badgeTone,
  previewHref,
  previewLabel = "Preview",
  target,
  blockedReason,
  children,
}: {
  title: string;
  badge: string;
  badgeTone?: "danger";
  previewHref: string;
  previewLabel?: string;
  target: RevisionTarget;
  blockedReason?: string | null;
  children: React.ReactNode;
}) {
  return (
    <div className={styles.block}>
      <div className={styles.blockHead}>
        <span className={styles.blockTitle}>{title}</span>
        <span
          className={`${styles.typ} ${
            badgeTone === "danger" ? styles.typDanger : ""
          }`}
        >
          {badge}
        </span>
        {/* This revision rendered as the page it will become. New tab, so the
            reviewer keeps the queue where it was. */}
        <a
          className={styles.previewLink}
          href={previewHref}
          target="_blank"
          rel="noreferrer"
        >
          {previewLabel}
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
              d="M6.5 3h6.5v6.5M13 3 7 9M11 10.5V13H3V5h2.5"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </a>
        <ReviewDecision target={target} blockedReason={blockedReason} />
      </div>
      <div className={styles.blockBody}>{children}</div>
    </div>
  );
}

const SIGN: Record<DiffRow["state"], string> = {
  added: "+",
  removed: "−",
  changed: "~",
  same: "=",
};

function Row({ row }: { row: DiffRow }) {
  return (
    <div className={`${styles.drow} ${styles[row.state]}`}>
      <div className={styles.dk}>
        <span className={styles.sign}>{SIGN[row.state]}</span>
        {row.label}
      </div>
      <div className={styles.dv}>
        <Value row={row} />
      </div>
    </div>
  );
}

function Value({ row }: { row: DiffRow }) {
  if (row.state === "added") {
    return <span className={styles.new}>{row.after ?? "—"}</span>;
  }
  if (row.state === "removed") {
    return <span className={styles.old}>{row.before ?? "—"}</span>;
  }
  if (row.state === "same") {
    return <>{row.after ?? row.before ?? "—"}</>;
  }

  if (row.prose && row.before && row.after) {
    const parts = inlineDiff(row.before, row.after);
    return (
      <>
        {parts.head}
        {parts.removed ? (
          <span className={styles.old}>{parts.removed}</span>
        ) : null}
        {parts.added ? <span className={styles.new}>{parts.added}</span> : null}
        {parts.tail}
      </>
    );
  }

  return (
    <>
      <span className={styles.old}>{row.before ?? "—"}</span>
      <span className={styles.arrow}>→</span>
      <span className={styles.new}>{row.after ?? "—"}</span>
      {row.delta !== undefined ? (
        <span
          className={`${styles.trend} ${row.delta > 0 ? styles.up : styles.down}`}
        >
          {row.delta > 0 ? "+" : ""}
          {round(row.delta)}
        </span>
      ) : null}
    </>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <p className={styles.empty}>{children}</p>
      </div>
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



function round(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toISOString().slice(0, 16).replace("T", " ") + " UTC";
}

function firstValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
