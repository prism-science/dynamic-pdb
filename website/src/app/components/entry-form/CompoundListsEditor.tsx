"use client";

import { useId, useState } from "react";

import {
  COMPOUND_LIST_NAMES,
  addTypedCompounds,
  confirmCompound,
  moveCompound,
  otherCompoundList,
  removeCompound,
  setCompoundListMode,
} from "./compounds";
import { Field, sentence } from "./FormFields";
import type { CompoundListFields } from "./fields";
import { inlineMessages, type FieldMessage } from "./messages";
import type {
  CompoundListMode,
  CompoundListName,
  CompoundsDraft,
} from "./types";
import styles from "./form.module.css";

const MODE_CHOICES: { mode: CompoundListMode; label: string }[] = [
  { mode: "unknown", label: "Not known" },
  { mode: "none", label: "None" },
  { mode: "list", label: "Listed" },
];

const LIST_LABELS: Record<CompoundListName, string> = {
  ligands: "ligands",
  cofactors: "cofactors",
};

/**
 * Ligands and cofactors, edited together because a compound belongs in
 * exactly one of them. One box takes CCD IDs and sorts each into its list
 * with the reference list; the depositor can move it, keep a compound the
 * parser was unsure about, or remove it. Each list says separately whether
 * it is not known, known to be empty, or listed.
 */
export default function CompoundListsEditor({
  compounds,
  onChange,
  fields,
  messages,
  source = null,
}: {
  compounds: CompoundsDraft;
  onChange: (next: CompoundsDraft) => void;
  fields: CompoundListFields;
  messages: readonly FieldMessage[];
  source?: string | null;
}) {
  const inputId = useId();
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  // Runs on Enter, on Add and when the box loses focus, so IDs typed just
  // before submitting are not left behind in the box.
  const add = () => {
    const result = addTypedCompounds(compounds, draft);
    if (result.compounds !== compounds) {
      onChange(result.compounds);
    }
    setDraft(result.rejected);
    setError(result.error);
  };

  return (
    <div className={styles.compoundEditor}>
      <label className={styles.subLabel} htmlFor={inputId}>
        Add ligands or cofactors
      </label>
      <div className={styles.compoundAddRow}>
        <input
          id={inputId}
          className={styles.input}
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            setError(null);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              add();
            }
          }}
          onBlur={add}
          placeholder="CCD IDs, for example ATP HEM ZN"
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
        <button
          type="button"
          className={styles.secondary}
          onClick={add}
          disabled={!draft.trim()}
        >
          Add
        </button>
      </div>
      {error ? <p className={styles.inlineError}>{sentence(error)}</p> : null}

      <div className={styles.compoundLists}>
        {COMPOUND_LIST_NAMES.map((list) => (
          <Field
            key={list}
            id={fields[list]}
            messages={inlineMessages(messages, fields[list])}
            source={source}
          >
            <ModeChoice
              list={list}
              mode={compounds[list].mode}
              onChange={(mode) =>
                onChange(setCompoundListMode(compounds, list, mode))
              }
            />
            <CompoundChips
              list={list}
              compounds={compounds}
              onChange={onChange}
            />
          </Field>
        ))}
      </div>
    </div>
  );
}

function ModeChoice({
  list,
  mode,
  onChange,
}: {
  list: CompoundListName;
  mode: CompoundListMode;
  onChange: (mode: CompoundListMode) => void;
}) {
  return (
    <div
      className={styles.modeChoice}
      role="radiogroup"
      aria-label={`Are the ${LIST_LABELS[list]} known?`}
    >
      {MODE_CHOICES.map((choice) => (
        <button
          key={choice.mode}
          type="button"
          role="radio"
          aria-checked={mode === choice.mode}
          className={styles.modeOption}
          data-active={mode === choice.mode ? "true" : undefined}
          onClick={() => onChange(choice.mode)}
        >
          {choice.label}
        </button>
      ))}
    </div>
  );
}

function CompoundChips({
  list,
  compounds,
  onChange,
}: {
  list: CompoundListName;
  compounds: CompoundsDraft;
  onChange: (next: CompoundsDraft) => void;
}) {
  const { mode, items } = compounds[list];
  switch (mode) {
    case "unknown":
      return <p className={styles.compoundEmpty}>We have not checked.</p>;
    case "none":
      return (
        <p className={styles.compoundEmpty}>Checked, and none are present.</p>
      );
    case "list":
      break;
    default:
      throw new Error(`unknown compound list mode: ${String(mode)}`);
  }
  if (items.length === 0) {
    return (
      <p className={styles.compoundEmpty}>
        Add CCD IDs above. An empty list is saved as none.
      </p>
    );
  }
  const other = otherCompoundList(list);
  return (
    <ul className={styles.compoundChips}>
      {items.map((item) => (
        <li
          key={item.id}
          className={styles.compoundChip}
          data-review={item.review ? "true" : undefined}
        >
          <div className={styles.compoundChipRow}>
            <code>{item.id}</code>
            {item.review ? (
              <span className={styles.reviewTag}>needs review</span>
            ) : null}
            <span className={styles.compoundActions}>
              {item.review ? (
                <button
                  type="button"
                  className={styles.chipAction}
                  onClick={() => onChange(confirmCompound(compounds, list, item.id))}
                >
                  Keep
                </button>
              ) : null}
              <button
                type="button"
                className={styles.chipAction}
                onClick={() => onChange(moveCompound(compounds, list, item.id))}
              >
                Move to {LIST_LABELS[other]}
              </button>
              <button
                type="button"
                className={styles.chipRemove}
                onClick={() => onChange(removeCompound(compounds, list, item.id))}
                aria-label={`Remove ${item.id}`}
              >
                ×
              </button>
            </span>
          </div>
          {item.review ? (
            <p className={styles.reviewReason}>{sentence(item.review)}</p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}
