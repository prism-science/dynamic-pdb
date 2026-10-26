"use client";

import { useState, type KeyboardEvent } from "react";

import { sentence } from "./FormFields";
import styles from "./form.module.css";

/**
 * A list typed one value at a time: Enter (or a separator) turns the text
 * into a chip, Backspace in an empty box takes the last one back. A value
 * that fails `validate` stays in the box with the reason under it, so a typo
 * is fixed instead of saved.
 */
export default function ChipListInput({
  id,
  values,
  onChange,
  placeholder,
  normalize = (raw) => raw.trim(),
  validate,
  separators,
}: {
  id?: string;
  values: string[];
  onChange: (next: string[]) => void;
  placeholder: string;
  normalize?: (raw: string) => string;
  validate?: (value: string) => string | null;
  /** Splits typed or pasted text into several values, for example commas
   *  between mutations. Authors, whose names contain commas, pass none. */
  separators?: RegExp;
}) {
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  const commit = (raw: string) => {
    const parts = (separators ? raw.split(separators) : [raw])
      .map(normalize)
      .filter(Boolean);
    if (parts.length === 0) {
      setDraft("");
      return;
    }
    const added: string[] = [];
    const rejected: string[] = [];
    let firstError: string | null = null;
    for (const value of parts) {
      const problem =
        validate?.(value) ??
        (values.includes(value) || added.includes(value)
          ? `${value} is already listed`
          : null);
      if (problem) {
        rejected.push(value);
        firstError ??= problem;
      } else {
        added.push(value);
      }
    }
    if (added.length > 0) {
      onChange([...values, ...added]);
    }
    setDraft(rejected.join(", "));
    setError(firstError);
  };

  const removeAt = (index: number) => {
    onChange(values.filter((_, position) => position !== index));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" || (separators && separators.test(event.key))) {
      event.preventDefault();
      commit(draft);
    } else if (event.key === "Backspace" && draft === "" && values.length > 0) {
      removeAt(values.length - 1);
    }
  };

  return (
    <>
      <div className={styles.authorsInput}>
        {values.map((value, index) => (
          <span key={`${value}-${index}`} className={styles.authorChip}>
            {value}
            <button
              type="button"
              className={styles.chipRemove}
              onClick={() => removeAt(index)}
              aria-label={`Remove ${value}`}
            >
              ×
            </button>
          </span>
        ))}
        <input
          id={id}
          className={styles.authorsField}
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            setError(null);
          }}
          onKeyDown={onKeyDown}
          onBlur={() => commit(draft)}
          placeholder={values.length > 0 ? "Add another…" : placeholder}
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
      </div>
      {error ? <p className={styles.inlineError}>{sentence(error)}</p> : null}
    </>
  );
}
