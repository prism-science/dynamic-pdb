"use client";

import type { FileReadState, UploadedStructure } from "./useFileExtraction";
import styles from "./form.module.css";

/**
 * One line per uploaded mmCIF the server is reading or failed to read. A
 * failure keeps the draft and what the browser read; Retry asks again.
 */
export default function FileReadStatus({
  states,
  onRetry,
}: {
  states: Record<string, FileReadState>;
  onRetry: (file: UploadedStructure) => void;
}) {
  const lines = Object.values(states);
  if (lines.every((state) => state.status === "read")) {
    return null;
  }
  return (
    <div role="status">
      {lines.map((state) => {
        switch (state.status) {
          case "reading":
            return (
              <p key={state.file.id} className={styles.sectionNote}>
                Reading the values in {state.file.name} on the server…
              </p>
            );
          case "failed":
            return (
              <div key={state.file.id} className={styles.sectionNote} data-state="failed">
                <span>
                  Could not read {state.file.name} on the server: {state.message}. The
                  form keeps what it read in the browser.
                </span>
                <button type="button" className={styles.secondary} onClick={() => onRetry(state.file)}>
                  Read {state.file.name} again
                </button>
              </div>
            );
          case "read":
            // Read: the values are in the form, with their source badges.
            return null;
          default:
            throw new Error(`unknown file read status: ${String((state as { status: string }).status)}`);
        }
      })}
    </div>
  );
}
