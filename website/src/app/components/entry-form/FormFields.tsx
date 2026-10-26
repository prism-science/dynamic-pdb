"use client";

import type { ReactNode } from "react";

import { FIELDS, SECTIONS, type FieldId, type SectionId } from "./fields";
import type { FieldMessage } from "./messages";
import styles from "./form.module.css";

// The renderer for the field registry: a section with the page's heading,
// and a field with its label, a Required/Optional flag or its source, the
// help text, and its messages. The inputs themselves stay with each form,
// because a title, a chip list and a crystal card share nothing but this
// frame.

export function FormSection({
  id,
  children,
}: {
  id: SectionId;
  children: ReactNode;
}) {
  const section = SECTIONS[id];
  const headingId = `form-section-${id}`;
  return (
    <section className={styles.formSection} aria-labelledby={headingId}>
      <header className={styles.formSectionHead}>
        <h2 id={headingId} className={styles.formSectionTitle}>
          {section.title}
        </h2>
        <p className={styles.formSectionHint}>{section.hint}</p>
      </header>
      {children}
    </section>
  );
}

export function Field({
  id,
  htmlFor,
  messages = [],
  source = null,
  children,
}: {
  id: FieldId;
  /** The input the label names; a field made of several inputs has none. */
  htmlFor?: string;
  messages?: readonly FieldMessage[];
  /** Where a filled-in value came from, such as "from PDB 7L0B". Replaces
   *  the Required/Optional flag. */
  source?: string | null;
  children: ReactNode;
}) {
  const spec = FIELDS[id];
  return (
    <div className={styles.formField}>
      <div className={styles.fieldHead}>
        {htmlFor ? (
          <label className={styles.fieldLabel} htmlFor={htmlFor}>
            {spec.label}
          </label>
        ) : (
          <span className={styles.fieldLabel}>{spec.label}</span>
        )}
        {source ? (
          <span className={styles.sourceBadge}>{source}</span>
        ) : (
          <span
            className={styles.fieldFlag}
            data-required={spec.required ? "true" : undefined}
          >
            {spec.required ? "Required" : "Optional"}
          </span>
        )}
      </div>
      {children}
      <p className={styles.fieldHelp}>{spec.help}</p>
      <FieldMessages messages={messages} />
    </div>
  );
}

export function FieldMessages({
  messages,
}: {
  messages: readonly FieldMessage[];
}) {
  return (
    <>
      {messages.map((message, index) => (
        <p
          key={`${message.field}-${message.item ?? ""}-${index}`}
          className={message.blocking ? styles.inlineError : styles.inlineWarning}
        >
          {sentence(message.message)}
        </p>
      ))}
    </>
  );
}

/** A value Dynamic PDB filled in, shown as text rather than as an input. */
export function ReadOnlyValue({ children }: { children: ReactNode }) {
  return <p className={styles.readOnlyValue}>{children}</p>;
}

const VISIBLE_BLOCKERS = 8;

/** Why the submit button is disabled, one line per problem. */
export function SubmitBlockers({
  action,
  lines,
}: {
  action: string;
  lines: string[];
}) {
  if (lines.length === 0) {
    return null;
  }
  return (
    <div className={styles.blockers} role="status">
      <strong>Before you can {action}:</strong>
      <ul>
        {lines.slice(0, VISIBLE_BLOCKERS).map((line) => (
          <li key={line}>{line}</li>
        ))}
        {lines.length > VISIBLE_BLOCKERS ? (
          <li>and {lines.length - VISIBLE_BLOCKERS} more</li>
        ) : null}
      </ul>
    </div>
  );
}

/** "must be greater than 0" → "Must be greater than 0." */
export function sentence(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) {
    return trimmed;
  }
  const capitalised = trimmed[0].toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capitalised) ? capitalised : `${capitalised}.`;
}
