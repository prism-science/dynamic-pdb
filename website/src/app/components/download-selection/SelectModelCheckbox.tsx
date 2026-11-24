"use client";

import { isModelChecked, setModelChecked, type SelectedModel } from "@/lib/download-selection";

import { useDownloadSelection } from "./DownloadSelectionProvider";
import styles from "./download-selection.module.css";

/**
 * The checkbox that puts one model in the download selection. searchQuery is
 * the search the model is listed in (null on an entry page): on the results
 * of a search selected whole, unchecking leaves the model out of it.
 */
export default function SelectModelCheckbox({
  model,
  searchQuery,
  label,
}: {
  model: SelectedModel;
  searchQuery: string | null;
  /** Visible text beside the box; without it the box has a label for screen readers only. */
  label?: string;
}) {
  const context = useDownloadSelection();
  if (context === null) {
    return null;
  }
  const checked = isModelChecked(context.selection, model.id, searchQuery);
  const accessibleName = `Select ${model.title}, ${model.entryLabel}, for download`;
  return (
    <label className={label ? styles.checkLabel : styles.rowCheck} data-model-checkbox={model.id}>
      <input
        type="checkbox"
        className={styles.checkbox}
        checked={checked}
        disabled={!context.ready}
        aria-label={label ? undefined : accessibleName}
        onChange={(event) => {
          const nextChecked = event.currentTarget.checked;
          context.apply((selection) => setModelChecked(selection, model, nextChecked, searchQuery));
        }}
      />
      {label ? <span>{label}</span> : null}
    </label>
  );
}
