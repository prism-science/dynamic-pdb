"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import type { ParsedFile } from "./types";
import styles from "./form.module.css";

// How many candidates the dropdown shows before it stops and asks for a
// narrower query. Long enough to pick from, short enough not to become the
// list this control replaced.
const VISIBLE_OPTIONS = 6;

// Forty chips are the same wall the checkbox list was. Past this the tail
// becomes a count that expands on demand.
const VISIBLE_TOKENS = 6;

/**
 * One side of a run's file links.
 *
 * Shows what is linked and nothing else. The two full checkbox lists this
 * replaces cost 2 x files x programs rows — 48 of them for a twelve-file
 * deposit with two runs, of which four were ticked. Here the row count follows
 * the links, and an entry with none renders an empty field.
 */
export default function FileLinkField({
  label,
  files,
  selected,
  onChange,
}: {
  label: string;
  files: ParsedFile[];
  selected: string[];
  onChange: (next: string[]) => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);

  const chosen = selected
    .map((id) => files.find((file) => file.id === id))
    .filter((file): file is ParsedFile => Boolean(file));

  const matches = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const taken = new Set(selected);
    return files.filter(
      (file) =>
        !taken.has(file.id) &&
        (needle === "" ||
          file.name.toLowerCase().includes(needle) ||
          file.type.toLowerCase().includes(needle) ||
          file.level.toLowerCase() === needle),
    );
  }, [files, query, selected]);

  // Clicking a chip's remove button must not reopen the dropdown, and clicking
  // away must close it.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onDown = (event: MouseEvent) => {
      if (!wrapper.current?.contains(event.target as Node)) {
        setOpen(false);
        setQuery("");
      }
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const add = (id: string) => {
    onChange([...selected, id]);
    setQuery("");
  };

  const addAll = () => {
    onChange([...selected, ...matches.map((file) => file.id)]);
    setQuery("");
    setOpen(false);
  };

  return (
    <div className={styles.linkColumn} ref={wrapper}>
      <span className={styles.linkLabel}>{label}</span>

      <div
        className={styles.tokenField}
        data-open={open ? "true" : undefined}
        onClick={() => setOpen(true)}
      >
        {(expanded ? chosen : chosen.slice(0, VISIBLE_TOKENS)).map((file) => (
          <span key={file.id} className={styles.token}>
            <span className={styles.tokenLevel} data-level={file.level}>
              {file.level}
            </span>
            <span className={styles.tokenName}>{file.name}</span>
            <button
              type="button"
              className={styles.tokenRemove}
              onClick={(event) => {
                event.stopPropagation();
                onChange(selected.filter((id) => id !== file.id));
              }}
              aria-label={`Unlink ${file.name}`}
            >
              ×
            </button>
          </span>
        ))}
        {!expanded && chosen.length > VISIBLE_TOKENS ? (
          <button
            type="button"
            className={styles.tokenCount}
            onClick={(event) => {
              event.stopPropagation();
              setExpanded(true);
            }}
          >
            +{chosen.length - VISIBLE_TOKENS} more
          </button>
        ) : null}
        {expanded && chosen.length > VISIBLE_TOKENS ? (
          <button
            type="button"
            className={styles.tokenCount}
            onClick={(event) => {
              event.stopPropagation();
              setExpanded(false);
            }}
          >
            show fewer
          </button>
        ) : null}
        <input
          className={styles.tokenInput}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && matches.length > 0) {
              event.preventDefault();
              add(matches[0].id);
            }
            if (
              event.key === "Backspace" &&
              query === "" &&
              selected.length > 0
            ) {
              onChange(selected.slice(0, -1));
            }
            if (event.key === "Escape") {
              setOpen(false);
              setQuery("");
            }
          }}
          placeholder={chosen.length === 0 ? "add…" : ""}
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
      </div>

      {open ? (
        <div className={styles.tokenPopover}>
          {matches.length === 0 ? (
            <p className={styles.tokenEmpty}>
              {files.length === selected.length
                ? "Every file is already linked."
                : "Nothing matches."}
            </p>
          ) : (
            <>
              <div className={styles.tokenPopoverHead}>
                <span>
                  {query.trim()
                    ? `${matches.length} of ${files.length} match “${query.trim()}”`
                    : `${matches.length} available`}
                </span>
                {/* An ensemble is dozens of files of one kind; nobody should
                    click them one at a time. */}
                {matches.length > 1 ? (
                  <button
                    type="button"
                    className={styles.tokenBulk}
                    onClick={addAll}
                  >
                    Add all {matches.length}
                  </button>
                ) : null}
              </div>
              {matches.slice(0, VISIBLE_OPTIONS).map((file) => (
                <button
                  key={file.id}
                  type="button"
                  className={styles.tokenOption}
                  onClick={() => add(file.id)}
                >
                  <span className={styles.tokenLevel} data-level={file.level}>
                    {file.level}
                  </span>
                  <span className={styles.tokenOptionName}>{file.name}</span>
                  <span className={styles.tokenOptionMeta}>{file.type}</span>
                </button>
              ))}
              {matches.length > VISIBLE_OPTIONS ? (
                <p className={styles.tokenMore}>
                  {matches.length - VISIBLE_OPTIONS} more — narrow the search
                </p>
              ) : null}
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
