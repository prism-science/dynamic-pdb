"use client";

import type { ReactNode } from "react";

import {
  SAME_AS_ARCHIVE_HINT,
  archiveValueSource,
  type ArchiveFieldState,
} from "./archiveChoices";
import styles from "./form.module.css";

/**
 * The choice under a field the PDB archive filled. While the archive's value
 * shows, it offers "Use my own value" and says what the depositor typed when
 * that differs. Once the field is taken over, the archive's value stays beside
 * the depositor's, with "Use the PDB value" to go back to it. A field the
 * archive did not fill (no state) shows nothing here.
 */
export default function ArchiveChoice({
  state,
  fieldLabel,
  archiveValue,
  typedValue,
  onOwn,
}: {
  state: ArchiveFieldState | undefined;
  /** Names the field in the buttons' accessible names, since every filled
   *  field has the same buttons. */
  fieldLabel: string;
  /** The archive's value, shown beside the depositor's own. */
  archiveValue: ReactNode;
  /** What the depositor typed, shown when it differs from the archive's. */
  typedValue: ReactNode;
  onOwn: (own: boolean) => void;
}) {
  if (!state) {
    return null;
  }
  if (!state.own) {
    return (
      <div className={styles.archiveChoice}>
        {state.conflict ? <p className={styles.archiveNote}>{typedValue}</p> : null}
        <button
          type="button"
          className={styles.archiveChoiceBtn}
          aria-label={`Use my own value for ${fieldLabel}`}
          onClick={() => onOwn(true)}
        >
          Use my own value
        </button>
      </div>
    );
  }
  return (
    <div className={styles.archiveChoice}>
      <div className={styles.archiveNote}>
        {archiveValueSource(state.lock)}: {archiveValue}
      </div>
      {state.sameAsArchive ? (
        <p className={styles.fieldHelp}>{SAME_AS_ARCHIVE_HINT}</p>
      ) : null}
      <button
        type="button"
        className={styles.archiveChoiceBtn}
        aria-label={`Use the PDB value for ${fieldLabel}`}
        onClick={() => onOwn(false)}
      >
        Use the PDB value
      </button>
    </div>
  );
}

/** What a conflict says the depositor typed: You typed: “Lysozyme”. */
export function typedText(value: string): string {
  return `You typed: “${value.trim()}”`;
}
