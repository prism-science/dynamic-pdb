"use client";

import styles from "./form.module.css";

/** Asks whether to continue a saved draft before the form writes over it. */
export default function DraftRestorePrompt({
  summary,
  onContinue,
  onDiscard,
}: {
  /** What the draft holds, for example “Lysozyme” · 2 uploaded files. */
  summary: string;
  onContinue: () => void;
  onDiscard: () => void;
}) {
  return (
    <div className={styles.modalBackdrop} role="dialog" aria-modal="true">
      <div className={styles.modalCard}>
        <h2 className={styles.modalTitle}>Continue where you left off?</h2>
        <p className={styles.modalText}>
          You have a saved draft: {summary}. Continue editing it, or start over
          with a blank form.
        </p>
        <div className={styles.modalActions}>
          <button type="button" className={styles.secondary} onClick={onDiscard}>
            Start fresh
          </button>
          <button type="button" className={styles.primary} onClick={onContinue}>
            Continue
          </button>
        </div>
      </div>
    </div>
  );
}

/** “Title” · N uploaded files, or a fallback when neither is set. */
export function draftSummary(
  title: string,
  fileCount: number,
  fallback: string,
): string {
  const parts: string[] = [];
  if (title.trim()) {
    parts.push(`“${title.trim()}”`);
  }
  if (fileCount > 0) {
    parts.push(`${fileCount} uploaded file${fileCount === 1 ? "" : "s"}`);
  }
  return parts.length > 0 ? parts.join(" · ") : fallback;
}
