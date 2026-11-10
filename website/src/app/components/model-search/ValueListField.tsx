"use client";

import { useId, type KeyboardEvent } from "react";

import {
  MAX_FILTER_VALUE_LENGTH,
  MAX_FILTER_VALUES,
  type TextListFilter,
} from "@/lib/model-search";

import styles from "./model-search.module.css";

/**
 * A free-text list filter as chips: each chip is one value, and one repeated
 * parameter in the URL (organism=A&organism=B). Values are never split on
 * commas, so a name that holds one survives.
 *
 * Controlled: the panel owns both the values and the text still being typed,
 * so Apply can take a value that was typed but not yet added.
 */
export default function ValueListField({
  filter,
  values,
  draft,
  problem,
  onValuesChange,
  onDraftChange,
}: {
  filter: TextListFilter;
  values: string[];
  draft: string;
  problem: string | null;
  onValuesChange: (values: string[]) => void;
  onDraftChange: (draft: string) => void;
}) {
  const inputId = useId();
  const helpId = useId();
  const problemId = useId();
  const listId = useId();
  const full = values.length >= MAX_FILTER_VALUES;

  function add() {
    const value = draft.trim();
    if (value === "" || full) {
      return;
    }
    const known = values.some((existing) => existing.toLowerCase() === value.toLowerCase());
    onValuesChange(known ? values : [...values, value]);
    onDraftChange("");
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    // Enter on typed text adds it; on an empty field it applies the filters.
    if (event.key === "Enter" && draft.trim() !== "") {
      event.preventDefault();
      add();
    }
  }

  const describedBy = [filter.help ? helpId : null, problem ? problemId : null]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={styles.field}>
      <label className={styles.fieldLabel} htmlFor={inputId}>
        {filter.label}
      </label>
      {filter.help ? (
        <p className={styles.fieldHelp} id={helpId}>
          {filter.help}
        </p>
      ) : null}
      {values.length > 0 ? (
        <ul className={styles.chips} aria-label={`${filter.label} values`}>
          {values.map((value, index) => (
            // A hand-edited URL may repeat a value, so the index tells chips apart.
            <li key={`${index}-${value}`} className={styles.chip}>
              <span>{value}</span>
              <button
                type="button"
                className={styles.chipRemove}
                aria-label={`Remove ${value} from ${filter.label}`}
                onClick={() => onValuesChange(values.filter((_, position) => position !== index))}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <div className={styles.chipInputRow}>
        <input
          id={inputId}
          className={styles.input}
          type="text"
          value={draft}
          placeholder={full ? `At most ${MAX_FILTER_VALUES} values` : filter.placeholder}
          maxLength={MAX_FILTER_VALUE_LENGTH}
          disabled={full}
          list={filter.suggestions ? listId : undefined}
          aria-invalid={problem ? true : undefined}
          aria-describedby={describedBy || undefined}
          onChange={(event) => onDraftChange(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <button
          type="button"
          className={styles.addButton}
          onClick={add}
          disabled={full || draft.trim() === ""}
          aria-label={`Add to ${filter.label}`}
        >
          Add
        </button>
      </div>
      {filter.suggestions ? (
        <datalist id={listId}>
          {filter.suggestions.map((suggestion) => (
            <option key={suggestion} value={suggestion} />
          ))}
        </datalist>
      ) : null}
      {problem ? (
        <p className={styles.fieldProblem} id={problemId} role="alert">
          {problem}
        </p>
      ) : null}
    </div>
  );
}
