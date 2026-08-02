"use client";

import { useEffect, useRef, useState } from "react";

import { parseExtExperimentRef, type ExtExperiment } from "@/lib/api/ext";

import { PlusIcon } from "./icons";
import styles from "./form.module.css";

export const EXT_LINK_HINT =
  "Its files become available under Baseline data, and name and description are prefilled.";
export const EXT_LINK_INVALID = "That doesn't look like an Ext experiment link.";

export default function ExtSourceField({
  experiment,
  error,
  loading,
  onLink,
  onUnlink,
}: {
  experiment: ExtExperiment | null;
  error: string | null;
  loading: boolean;
  onLink: (experimentId: string) => void;
  onUnlink: () => void;
}) {
  // Most structures have no Ext experiment, so the field stays collapsed to a
  // single line until it is asked for.
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);
  // A failed lookup lives in the parent; hide it as soon as the user starts
  // editing so a stale message doesn't sit under the field they're fixing.
  const [errorDismissed, setErrorDismissed] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (experiment) {
      setOpen(false);
      setValue("");
      setParseError(null);
    }
  }, [experiment]);

  useEffect(() => {
    setErrorDismissed(false);
  }, [error]);

  const link = (raw: string) => {
    const experimentId = parseExtExperimentRef(raw);
    if (!experimentId) {
      setParseError(EXT_LINK_INVALID);
      return;
    }
    setParseError(null);
    onLink(experimentId);
  };

  const collapse = () => {
    setOpen(false);
    setValue("");
    setParseError(null);
    setErrorDismissed(true);
  };

  if (experiment) {
    return (
      <div className={styles.sourceSection}>
        <div className={styles.sourceRow}>
          <span className={styles.sourceMark}>Ext</span>
          <span className={styles.sourceMeta}>
            <strong>{experiment.name}</strong>
            <span>{experiment.id}</span>
          </span>
          <a
            className={styles.sourceLink}
            href={experiment.web_url}
            target="_blank"
            rel="noreferrer"
          >
            Open
          </a>
          <button
            type="button"
            className={styles.remove}
            onClick={onUnlink}
            aria-label="Unlink source experiment"
            title="Unlink source experiment"
          >
            ×
          </button>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className={styles.sourceSection}>
        <div className={styles.sourceRow} aria-busy="true">
          <span className={styles.sourceMark}>Ext</span>
          <span className={styles.sourceMeta}>
            <strong>Loading experiment…</strong>
          </span>
        </div>
      </div>
    );
  }

  const message = parseError ?? (errorDismissed ? null : error);

  // A link that arrived broken (bad query param) opens the field on its own so
  // the message has something to sit under.
  if (!open && !message) {
    return (
      <div className={styles.sourceSection}>
        <button
          type="button"
          className={styles.sourceToggle}
          onClick={() => {
            setOpen(true);
            // The field only exists once expanded, so focus it on the next tick.
            window.requestAnimationFrame(() => inputRef.current?.focus());
          }}
        >
          <PlusIcon />
          Link an Ext experiment
        </button>
      </div>
    );
  }

  return (
    <div className={styles.sourceSection}>
      <div className={styles.sourceInputRow}>
        <input
          ref={inputRef}
          id="ext-experiment-link"
          aria-label="Ext experiment link"
          className={styles.input}
          type="text"
          inputMode="url"
          value={value}
          aria-invalid={message ? true : undefined}
          aria-describedby="ext-experiment-note"
          onChange={(event) => {
            setValue(event.target.value);
            setParseError(null);
            setErrorDismissed(true);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              link(value);
            } else if (event.key === "Escape") {
              collapse();
            }
          }}
          onPaste={(event) => {
            // Pasting a link is the whole point of this field, so resolve it
            // right away instead of making the user also press Link.
            const pasted = event.clipboardData.getData("text");
            if (parseExtExperimentRef(pasted)) {
              event.preventDefault();
              setValue(pasted.trim());
              link(pasted);
            }
          }}
          placeholder="https://extshell.org/experiments/6d95a158-…"
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
        <button
          type="button"
          className={styles.sourceLinkBtn}
          onClick={() => link(value)}
          disabled={value.trim() === ""}
        >
          Link
        </button>
        <button
          type="button"
          className={styles.remove}
          onClick={collapse}
          aria-label="Cancel linking an experiment"
        >
          ×
        </button>
      </div>
      {message ? (
        <span
          className={styles.sourceError}
          id="ext-experiment-note"
          role="alert"
        >
          {message}
        </span>
      ) : (
        <span className={styles.sourceHint} id="ext-experiment-note">
          {EXT_LINK_HINT}
        </span>
      )}
    </div>
  );
}
