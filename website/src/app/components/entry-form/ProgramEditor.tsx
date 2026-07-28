"use client";

import { PlusIcon } from "./icons";
import type { ProgramDraft } from "./types";
import styles from "./form.module.css";

export default function ProgramEditor({
  program,
  setProgram,
}: {
  program: ProgramDraft | null;
  setProgram: (next: ProgramDraft | null) => void;
}) {
  const add = () =>
    setProgram({
      id: crypto.randomUUID(),
      name: "",
      version: "",
      description: "",
    });
  const patch = (next: Partial<ProgramDraft>) => {
    if (!program) {
      return;
    }
    setProgram({ ...program, ...next });
  };

  return (
    <div className={styles.filesEditor}>
      {!program ? (
        <button type="button" className={styles.fileDrop} onClick={add}>
          <PlusIcon />
          Add program
        </button>
      ) : (
        <div className={styles.metricCard}>
          <div className={styles.metricTop}>
            <span className={styles.programTag}>Program</span>
            <button
              type="button"
              className={styles.remove}
              onClick={() => setProgram(null)}
              aria-label="Remove program"
            >
              ×
            </button>
          </div>
          <div className={styles.programGrid}>
            <label className={styles.metricField}>
              <span>Name</span>
              <input
                className={styles.input}
                value={program.name}
                onChange={(event) => patch({ name: event.target.value })}
                placeholder="e.g. phenix.refine"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </label>
            <label className={styles.metricField}>
              <span>Version</span>
              <input
                className={styles.input}
                value={program.version}
                onChange={(event) => patch({ version: event.target.value })}
                placeholder="e.g. 1.21.2"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </label>
            <label className={`${styles.metricField} ${styles.programDescription}`}>
              <span>Description</span>
              <textarea
                className={styles.textarea}
                value={program.description}
                onChange={(event) =>
                  patch({ description: event.target.value })
                }
                placeholder="e.g. Reciprocal-space refinement against processed reflections"
                rows={2}
              />
            </label>
          </div>
        </div>
      )}
    </div>
  );
}
