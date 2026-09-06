import {
  diffEntry,
  diffModel,
  inlineDiff,
  type DiffRow,
  type DiffSection,
} from "@/lib/reviewDiff";
import type {
  EntryReview,
  ReviewEntry,
  ReviewModel,
  RevisionTarget,
} from "@/lib/api/entries";
import type { ReviewPermissions } from "@/lib/auth/permissions";
import ReviewDecision from "@/app/reviews/ReviewDecision";

import styles from "./review-inbox.module.css";

/** The submission card, shared by the reviewer queue and the author's own
 *  submissions. The two differ only in whether the blocks can be decided, so
 *  everything else is identical by construction rather than by copy. */
type Props = {
  title: string;
  review: EntryReview;
  reviewPermissions: ReviewPermissions | null;
};

export default function SubmissionCard({
  title,
  review,
  reviewPermissions,
}: Props) {
  return (
    <div className={styles.pvScroll}>
      <div className={styles.pvTitle}>{title}</div>
      <Changes review={review} reviewPermissions={reviewPermissions} />
    </div>
  );
}

/** What is being decided: only what was actually submitted. Every block carries
 *  its own decision — the entry and each model are approved or rejected
 *  separately. */
function Changes({
  review,
  reviewPermissions,
}: {
  review: EntryReview;
  reviewPermissions: ReviewPermissions | null;
}) {
  // No published revision behind the entry means nothing for a model to go live
  // under, and the backend refuses it. Same condition the "new" badge reads, so
  // what the block says and what its buttons do cannot drift apart.
  const entryUnpublished = review.entry !== null && review.entry.active === null;
  const modelsBlockedReason =
    reviewPermissions && entryUnpublished
      ? "Approve the entry first — it has no published revision yet."
      : null;

  return (
    <>
      {review.entry ? (
        review.entry.proposed.entry_state === "deleted" ? (
          <DeletionBlock
            reviewPermissions={reviewPermissions}
            title="Entry"
            what="entry"
            target={review.entry.target}
            removes={entryRemovals(review.entry.active)}
            publishedHref={`/entries/${encodeURIComponent(review.entry_id)}`}
          />
        ) : (
          <DiffBlock
            reviewPermissions={reviewPermissions}
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
            reviewPermissions={reviewPermissions}
            key={model.model_id}
            title={model.proposed.title?.trim() || model.model_id}
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
            reviewPermissions={reviewPermissions}
            key={model.model_id}
            title={model.proposed.title?.trim() || model.model_id}
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
  reviewPermissions,
}: {
  title: string;
  what: "entry" | "model";
  target: RevisionTarget;
  removes: string[];
  publishedHref: string;
  blockedReason?: string | null;
  reviewPermissions: ReviewPermissions | null;
}) {
  return (
    <Block
      title={title}
      badge="deletion"
      reviewPermissions={reviewPermissions}
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
  reviewPermissions,
}: {
  title: string;
  badge: string;
  sections: DiffSection[];
  target: RevisionTarget;
  previewHref: string;
  blockedReason?: string | null;
  reviewPermissions: ReviewPermissions | null;
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
      reviewPermissions={reviewPermissions}
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
  reviewPermissions,
  children,
}: {
  title: string;
  badge: string;
  badgeTone?: "danger";
  previewHref: string;
  previewLabel?: string;
  target: RevisionTarget;
  blockedReason?: string | null;
  reviewPermissions: ReviewPermissions | null;
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
        {reviewPermissions ? (
          <ReviewDecision
            target={target}
            canApprove={reviewPermissions.canApprove}
            canReject={reviewPermissions.canReject}
            blockedReason={blockedReason}
          />
        ) : null}
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

function round(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}
