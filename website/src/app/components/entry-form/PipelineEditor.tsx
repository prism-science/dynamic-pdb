"use client";

import FileLinkField from "./FileLinkField";
import { FieldMessages } from "./FormFields";
import { FIELDS } from "./fields";
import { PlusIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import { emptyProgramDraft } from "./helpers";
import type { ParsedFile, ProgramDraft } from "./types";
import styles from "./form.module.css";

/**
 * The runs that made this model, and which files each one touched.
 *
 * Linking is done from the program rather than from the file row: a deposit
 * has two or three runs and a dozen files, so this is three controls instead
 * of twelve, and there is one place where an edge is written.
 */
export default function PipelineEditor({
  programs,
  files,
  baseline = [],
  messages,
  onChange,
}: {
  programs: ProgramDraft[];
  files: ParsedFile[];
  /** Files that already belong to the entry and can be fed to a run here. */
  baseline?: ParsedFile[];
  /** Messages about a step are keyed by its program id. */
  messages: readonly FieldMessage[];
  onChange: (next: ProgramDraft[]) => void;
}) {
  const candidates = [...baseline, ...files];

  const patch = (id: string, next: Partial<ProgramDraft>) =>
    onChange(
      programs.map((program) =>
        program.id === id ? { ...program, ...next } : program,
      ),
    );

  const setLinks = (
    program: ProgramDraft,
    key: "inputFileIds" | "outputFileIds",
    next: string[],
  ) => {
    // A file is either fed to a run or made by it, never both, so anything
    // added on one side leaves the other.
    const other = key === "inputFileIds" ? "outputFileIds" : "inputFileIds";
    const added = new Set(next);
    patch(program.id, {
      [key]: next,
      [other]: program[other].filter((id) => !added.has(id)),
    } as Partial<ProgramDraft>);
  };

  return (
    <div className={styles.filesEditor}>
      {programs.map((program, index) => (
        <div key={program.id} className={styles.metricCard}>
          <div className={styles.metricTop}>
            <span className={styles.programTag}>Step {index + 1}</span>
            {program.origin === "parsed" && program.originFile ? (
              <span className={styles.programSource}>
                read from {program.originFile}
              </span>
            ) : null}
            <button
              type="button"
              className={styles.remove}
              onClick={() =>
                onChange(programs.filter((item) => item.id !== program.id))
              }
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
                onChange={(event) => patch(program.id, { name: event.target.value })}
                placeholder="e.g. phenix.refine"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
              <FieldMessages
                messages={inlineMessages(messages, "run.name", program.id)}
              />
            </label>
            <label className={styles.metricField}>
              <span>Version</span>
              <input
                className={styles.input}
                value={program.version}
                onChange={(event) =>
                  patch(program.id, { version: event.target.value })
                }
                placeholder="e.g. 1.21.2"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
              <FieldMessages
                messages={inlineMessages(messages, "run.version", program.id)}
              />
            </label>
            <label className={`${styles.metricField} ${styles.programDescription}`}>
              <span>Description</span>
              <textarea
                className={styles.textarea}
                value={program.description}
                onChange={(event) =>
                  patch(program.id, { description: event.target.value })
                }
                placeholder="Optional — e.g. reciprocal-space refinement against processed reflections"
                rows={2}
              />
              <FieldMessages
                messages={inlineMessages(messages, "run.description", program.id)}
              />
            </label>
            <label className={`${styles.metricField} ${styles.programDescription}`}>
              <span>{FIELDS["run.command"].label} (optional)</span>
              <textarea
                className={`${styles.textarea} ${styles.codeInput}`}
                value={program.command}
                onChange={(event) =>
                  patch(program.id, { command: event.target.value })
                }
                placeholder="e.g. phenix.refine model.pdb data.mtz refinement.main.number_of_macro_cycles=5"
                rows={2}
                spellCheck={false}
              />
              <FieldMessages
                messages={inlineMessages(messages, "run.command", program.id)}
              />
            </label>
            <label className={`${styles.metricField} ${styles.programDescription}`}>
              <span>{FIELDS["run.parameters"].label} (optional)</span>
              <textarea
                className={`${styles.textarea} ${styles.codeInput}`}
                value={program.parameters}
                onChange={(event) =>
                  patch(program.id, { parameters: event.target.value })
                }
                placeholder='e.g. {"cycles": 5, "weight": "auto"}'
                rows={3}
                spellCheck={false}
              />
              <FieldMessages
                messages={inlineMessages(messages, "run.parameters", program.id)}
              />
            </label>
          </div>
          <FieldMessages
            messages={inlineMessages(messages, "run.files", program.id)}
          />

          {candidates.length > 0 ? (
            <div className={styles.linkGrid}>
              <FileLinkField
                label="Inputs"
                files={candidates}
                selected={program.inputFileIds}
                onChange={(next) => setLinks(program, "inputFileIds", next)}
              />
              <FileLinkField
                label="Outputs"
                files={candidates}
                selected={program.outputFileIds}
                onChange={(next) => setLinks(program, "outputFileIds", next)}
              />
            </div>
          ) : null}

          {program.expectedInputs.length > 0 ? (
            <p className={styles.expectedNote}>
              Declared in {program.expectedInputs[0].source}, not uploaded:{" "}
              {program.expectedInputs.map((item) => item.name).join(", ")}
            </p>
          ) : null}
        </div>
      ))}

      <button
        type="button"
        className={styles.fileDrop}
        onClick={() => onChange([...programs, emptyProgramDraft()])}
      >
        <PlusIcon />
        Add program
      </button>
    </div>
  );
}
