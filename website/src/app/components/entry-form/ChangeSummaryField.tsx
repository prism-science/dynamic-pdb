"use client";

import { useId } from "react";

import styles from "./form.module.css";

/** An optional note for the reviewer of an edit. It is not a change by
 *  itself: an edit must change at least one field. */
export default function ChangeSummaryField({
  value,
  onChange,
}: {
  value: string;
  onChange: (next: string) => void;
}) {
  const inputId = useId();
  return (
    <div className={styles.formField}>
      <div className={styles.fieldHead}>
        <label className={styles.fieldLabel} htmlFor={inputId}>
          Change summary
        </label>
        <span className={styles.fieldFlag}>Optional</span>
      </div>
      <textarea
        id={inputId}
        className={styles.textarea}
        rows={2}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="e.g. Corrected the growth pH from the lab notebook."
      />
      <p className={styles.fieldHelp}>
        What you changed and why. A reviewer reads it before the change is published.
      </p>
    </div>
  );
}
