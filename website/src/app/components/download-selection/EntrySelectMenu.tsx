"use client";

import { useEffect, useRef, useState } from "react";

import { isModelChecked, setModelChecked, type SelectedModel } from "@/lib/download-selection";

import SelectModelCheckbox from "./SelectModelCheckbox";
import { useDownloadSelection } from "./DownloadSelectionProvider";
import styles from "./download-selection.module.css";

export type EntrySelectModel = SelectedModel & {
  /** The entry's Deposited model: the one that holds the PDB's original mmCIF. */
  deposited: boolean;
};

/**
 * "Select for download" on an entry page: the entry's models, each with a
 * checkbox, next to the Download Files menu. The Deposited model is labelled,
 * because "Original mmCIF" archives take the PDB's file only from it.
 */
export default function EntrySelectMenu({ models }: { models: EntrySelectModel[] }) {
  const context = useDownloadSelection();
  const [open, setOpen] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    function onPointerDown(event: MouseEvent) {
      if (!wrapper.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  if (context === null || models.length === 0) {
    return null;
  }
  const selectedCount = models.filter((model) => isModelChecked(context.selection, model.id, null)).length;
  const allSelected = selectedCount === models.length;

  return (
    <div className={styles.entryMenu} ref={wrapper}>
      <button
        type="button"
        className={styles.entryMenuButton}
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-controls="entry-select-menu"
      >
        Select for download
        {selectedCount > 0 ? <span className={styles.entryMenuCount}>{selectedCount}</span> : null}
        <span className={styles.caret} aria-hidden="true" />
      </button>
      {open ? (
        <div className={styles.entryMenuPanel} id="entry-select-menu">
          <ul className={styles.entryMenuList} aria-label="Models of this entry">
            {models.map((model) => (
              <li key={model.id}>
                <SelectModelCheckbox
                  model={model}
                  searchQuery={null}
                  label={model.deposited ? `${model.title} (Deposited: holds the PDB's original mmCIF)` : model.title}
                />
              </li>
            ))}
          </ul>
          <button
            type="button"
            className={styles.linkButton}
            disabled={!context.ready}
            onClick={() =>
              context.apply((selection) => {
                let next = selection;
                for (const model of models) {
                  const change = setModelChecked(next, model, !allSelected, null);
                  if ("limit" in change) {
                    return change;
                  }
                  next = change.selection;
                }
                return { selection: next };
              })
            }
          >
            {allSelected ? "Unselect all models of this entry" : `Select all ${models.length} models of this entry`}
          </button>
        </div>
      ) : null}
    </div>
  );
}
