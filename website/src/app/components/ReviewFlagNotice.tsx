import type { ReviewStatus } from "@/lib/review-flags";

import styles from "./MissingInfoNotice.module.css";

export const REVIEW_FLAG_TOOLTIP =
  "A reviewer has been asked to check this record. It stays public meanwhile.";

/**
 * The "flagged for review" badge of a record (researcher answer 7). Everyone
 * sees the badge; reviewers and admins, whose responses carry the issues, can
 * open it to see which. Renders nothing for a record that is not flagged.
 */
export default function ReviewFlagNotice({ status }: { status: ReviewStatus }) {
  if (!status.flagged) {
    return null;
  }
  const badge = (
    <span className={styles.badge} title={REVIEW_FLAG_TOOLTIP}>
      Flagged for review
    </span>
  );
  if (status.issueLines === null || status.issueLines.length === 0) {
    return <p className={styles.notice}>{badge}</p>;
  }
  return (
    <details className={styles.notice}>
      <summary className={styles.summary}>
        {badge}
        <span className={styles.toggle}>Which issues</span>
      </summary>
      <p className={styles.lead}>Open review issues, shown to reviewers and admins:</p>
      <ul className={styles.lines}>
        {status.issueLines.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
    </details>
  );
}
