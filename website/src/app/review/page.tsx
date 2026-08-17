import Link from "next/link";

import {
  ApiRequestError,
  getEntryReview,
  listReviews,
  type EntryReview,
  type ReviewQueueItem,
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

  const queue = await loadQueue(session.token);
  const params = (await searchParams) ?? {};
  const requested = firstValue(params.sel);

  const selected =
    queue.find((item) => item.key === requested) ?? queue[0] ?? null;
  const review = selected
    ? await loadReview(session.token, selected.target)
    : null;

  return (
    <main className={styles.page} aria-label="Review inbox">
      <div className={styles.shell}>
        <h1 className={styles.title}>Review</h1>
        <div className={styles.inbox}>
          <div className={`${styles.col} ${styles.list}`}>
            <div className={styles.listHead}>
              <h2 className={styles.listTitle}>To review</h2>
              <div className={styles.sub}>
                {queue.length} item{queue.length === 1 ? "" : "s"} waiting
              </div>
            </div>
            {queue.length === 0 ? (
              <p className={styles.empty}>Nothing is waiting for your review.</p>
            ) : (
              queue.map((item) => (
                <Link
                  key={item.key}
                  href={`/review?sel=${encodeURIComponent(item.key)}`}
                  className={`${styles.row} ${
                    item.key === selected?.key ? styles.rowSel : ""
                  }`}
                >
                  <div className={styles.rowName}>{item.name}</div>
                  <div className={styles.rowMeta}>
                    submitted {formatDate(item.submitted_at)}
                  </div>
                </Link>
              ))
            )}
          </div>

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
      <div className={styles.pvSub}>
        submitted {formatDate(item.submitted_at)}
      </div>

      <div className={styles.toolbar}>
        <ReviewDecision target={review.target} />
      </div>

      <Changes review={review} />
    </div>
  );
}

/** What is being decided: only what was actually submitted. An entry nobody
 *  touched is not part of the submission and is not drawn. */
function Changes({ review }: { review: EntryReview }) {
  const proposedEntry = review.entry.proposed;

  return (
    <>
      {proposedEntry ? (
        <DiffBlock
          title="Entry"
          badge={review.entry.active ? "revision" : "new"}
          sections={diffEntry(review.entry.active, proposedEntry)}
        />
      ) : null}

      {review.models.length > 0 ? (
        <div className={styles.sect}>
          <span className={styles.sectH}>
            {proposedEntry ? "Models in review" : "Models"}
          </span>
          <span className={styles.sectLine} />
        </div>
      ) : null}

      {review.models.map((model) => (
        <DiffBlock
          key={model.model_id}
          title={model.proposed.name}
          badge={model.active ? "revision" : "new"}
          sections={diffModel(model.active, model.proposed)}
        />
      ))}
    </>
  );
}

function DiffBlock({
  title,
  badge,
  sections,
}: {
  title: string;
  badge: string;
  sections: DiffSection[];
}) {
  const visible = sections
    .map((section) => ({
      title: section.title,
      rows: section.rows.filter((row) => row.state !== "same"),
    }))
    .filter((section) => section.rows.length > 0);

  return (
    <Block title={title} badge={badge}>
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
  children,
}: {
  title: string;
  badge: string;
  children: React.ReactNode;
}) {
  return (
    <div className={styles.block}>
      <div className={styles.blockHead}>
        <span className={styles.blockTitle}>{title}</span>
        <span className={styles.typ}>{badge}</span>
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

async function loadQueue(token: string): Promise<ReviewQueueItem[]> {
  try {
    return await listReviews(token);
  } catch (error) {
    if (error instanceof ApiRequestError) return [];
    throw error;
  }
}

async function loadReview(
  token: string,
  target: RevisionTarget,
): Promise<EntryReview | null> {
  try {
    return await getEntryReview(token, target);
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
