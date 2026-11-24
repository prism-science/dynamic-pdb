"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState } from "react";

import { scriptPageHref } from "@/lib/download-instructions";
import {
  clearedSelection,
  FILE_CHOICES,
  fileChoiceText,
  isEmptySelection,
  removeLeftOut,
  removePicked,
  removeWholeSearch,
  reusableSelectionId,
  selectionSize,
  selectionSizeText,
  withAddDepositedModels,
  type FileChoice,
  type SelectedModel,
} from "@/lib/download-selection";

import { useDownloadSelection } from "./DownloadSelectionProvider";
import FollowedArchives, { ProblemNotice, SELECTION_REVIEW_ID } from "./FollowedArchives";
import SelectionFileChecks from "./SelectionFileChecks";
import styles from "./download-selection.module.css";

/** Models listed before "Show all". */
const MODELS_SHOWN = 50;

/**
 * The drawer of the download panel: review the selection, pick the files,
 * check what the archive will hold, build it and follow it until it can be
 * downloaded. A modal dialog, so focus stays inside and Escape closes it.
 */
export default function SelectionDrawer() {
  const context = useDownloadSelection();
  const dialog = useRef<HTMLDialogElement>(null);
  const headingId = useId();
  const open = context?.drawerOpen ?? false;

  useEffect(() => {
    const element = dialog.current;
    if (element === null) {
      return;
    }
    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  if (context === null) {
    return null;
  }
  return (
    <dialog
      ref={dialog}
      className={styles.drawer}
      aria-labelledby={headingId}
      onClose={() => context.setDrawerOpen(false)}
    >
      <div className={styles.drawerHead}>
        <h2 id={headingId} className={styles.drawerTitle}>
          Download selection
        </h2>
        <button
          type="button"
          className={styles.closeButton}
          aria-label="Close"
          onClick={() => context.setDrawerOpen(false)}
        >
          ×
        </button>
      </div>
      {open ? <DrawerBody /> : null}
    </dialog>
  );
}

function DrawerBody() {
  const context = useDownloadSelection();
  const [fileChoice, setFileChoice] = useState<FileChoice | null>(null);
  const fileChoiceName = useId();
  if (context === null) {
    return null;
  }
  const { selection, pending, problem } = context;
  const empty = isEmptySelection(selection);
  const reusableId = reusableSelectionId(selection);
  const startProblem = problem !== null && problem.operation === "start" ? problem.problem : null;

  return (
    <div className={styles.drawerBody}>
      {context.notice !== null ? (
        <p className={styles.notice} role="alert">
          {context.notice}{" "}
          <button type="button" className={styles.linkButton} onClick={context.dismissNotice}>
            Dismiss
          </button>
        </p>
      ) : null}

      {empty ? (
        <p className={styles.muted}>
          Nothing selected yet. Tick models in search results or on entry pages, or select all results of a search.
        </p>
      ) : (
        <SelectionReview />
      )}

      {!empty ? (
        <>
          <section className={styles.section} aria-label="Options">
            <label className={styles.checkLabel}>
              <input
                type="checkbox"
                className={styles.checkbox}
                checked={selection.addDepositedModels}
                onChange={(event) => {
                  const add = event.currentTarget.checked;
                  context.update((current) => withAddDepositedModels(current, add));
                }}
              />
              <span>Also select each entry&rsquo;s Deposited model</span>
            </label>
            <p className={styles.help}>
              The PDB&rsquo;s original mmCIF belongs to the Deposited model. Computed models (for example qFit or
              ensembles) do not hold it.
            </p>
          </section>

          <fieldset className={styles.section}>
            <legend className={styles.sectionTitle}>Files</legend>
            {FILE_CHOICES.map((choice) => {
              const text = fileChoiceText(choice);
              return (
                <label key={choice} className={styles.choice}>
                  <input
                    type="radio"
                    name={fileChoiceName}
                    value={choice}
                    checked={fileChoice === choice}
                    onChange={() => setFileChoice(choice)}
                  />
                  <span>
                    <span className={styles.choiceLabel}>{text.label}</span>
                    <span className={styles.help}>{text.description}</span>
                  </span>
                </label>
              );
            })}
            {fileChoice === "original_mmcif" && !selection.addDepositedModels ? (
              <p className={styles.hint} role="note">
                Only Deposited models hold the original mmCIF. Tick &ldquo;Also select each entry&rsquo;s Deposited
                model&rdquo; to get it for computed models too.
              </p>
            ) : null}
          </fieldset>

          <div className={styles.startRow}>
            <button
              type="button"
              className={styles.primaryButton}
              disabled={!context.ready || pending !== null || fileChoice === null}
              onClick={() => {
                if (fileChoice !== null) {
                  context.startDownload(fileChoice);
                }
              }}
            >
              {pending === "start"
                ? reusableId === null
                  ? "Saving the selection…"
                  : "Asking for the archive…"
                : "Check the archive"}
            </button>
            {fileChoice === null ? (
              <span className={styles.muted}>Choose the files first.</span>
            ) : (
              <span className={styles.muted}>Shows what the archive holds before anything is built.</span>
            )}
          </div>
          {startProblem !== null ? <ProblemNotice problem={startProblem} /> : null}
        </>
      ) : null}

      <FollowedArchives />
    </div>
  );
}

function SelectionReview() {
  const context = useDownloadSelection();
  const [showAll, setShowAll] = useState(false);
  if (context === null) {
    return null;
  }
  const { selection } = context;
  const frozen = reusableSelectionId(selection) !== null ? selection.frozen : null;
  const shownModels = showAll ? selection.picked : selection.picked.slice(0, MODELS_SHOWN);

  return (
    <>
      <p className={styles.size} id={SELECTION_REVIEW_ID} tabIndex={-1}>
        {frozen !== null
          ? `Saved: ${countText(frozen.modelCount, "model")} in ${countText(frozen.entryCount, "entry", "entries")}.`
          : `${capitalize(selectionSizeText(selectionSize(selection)))}. The exact count comes when the selection is saved.`}
      </p>
      {frozen !== null ? (
        <p className={styles.help} data-selection-id={frozen.id}>
          Selection ID <code className={styles.selectionId}>{frozen.id}</code> &middot;{" "}
          <Link href={scriptPageHref(frozen.id)}>Use the script</Link> for any size
        </p>
      ) : null}
      {frozen !== null ? <SelectionFileChecks key={frozen.id} selectionId={frozen.id} picked={selection.picked} /> : null}

      {selection.searches.length > 0 ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Whole searches ({selection.searches.length})</h3>
          <ul className={styles.reviewList}>
            {selection.searches.map((search) => (
              <li key={search.query} className={styles.reviewItem}>
                <span className={styles.reviewText}>
                  <span>{search.label}</span>
                  <span className={styles.muted}>
                    {search.countExact
                      ? `${countText(search.count, "result")} when selected`
                      : `more than ${search.count.toLocaleString("en-US")} results when selected`}
                  </span>
                </span>
                <Link className={styles.linkButton} href={search.query ? `/browse?${search.query}` : "/browse"}>
                  Open
                </Link>
                <button
                  type="button"
                  className={styles.linkButton}
                  onClick={() => context.update((current) => removeWholeSearch(current, search.query))}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {selection.picked.length > 0 ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Models ({selection.picked.length})</h3>
          <ModelList
            models={shownModels}
            actionLabel="Remove"
            onAction={(model) => context.update((current) => removePicked(current, model.id))}
          />
          {selection.picked.length > MODELS_SHOWN ? (
            <button type="button" className={styles.linkButton} onClick={() => setShowAll((value) => !value)}>
              {showAll ? "Show fewer" : `Show all ${selection.picked.length.toLocaleString("en-US")}`}
            </button>
          ) : null}
        </section>
      ) : null}

      {selection.leftOut.length > 0 ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Left out ({selection.leftOut.length})</h3>
          <p className={styles.help}>These models stay out of the whole selection, whichever part would add them.</p>
          <ModelList
            models={selection.leftOut}
            actionLabel="Take back"
            onAction={(model) => context.update((current) => removeLeftOut(current, model.id))}
          />
        </section>
      ) : null}

      <button
        type="button"
        className={styles.linkButton}
        onClick={() => context.update((current) => clearedSelection(current))}
      >
        Clear selection
      </button>
    </>
  );
}

function ModelList({
  models,
  actionLabel,
  onAction,
}: {
  models: SelectedModel[];
  actionLabel: string;
  onAction: (model: SelectedModel) => void;
}) {
  return (
    <ul className={styles.reviewList}>
      {models.map((model) => (
        <li key={model.id} className={styles.reviewItem} data-review-model={model.id}>
          <span className={styles.reviewText}>
            <Link href={`/entries/${encodeURIComponent(model.entryId)}/models/${encodeURIComponent(model.id)}`}>
              {model.title}
            </Link>
            <span className={styles.muted}>{model.entryLabel}</span>
          </span>
          <button
            type="button"
            className={styles.linkButton}
            aria-label={`${actionLabel} ${model.title}`}
            onClick={() => onAction(model)}
          >
            {actionLabel}
          </button>
        </li>
      ))}
    </ul>
  );
}

function countText(count: number, singular: string, plural = `${singular}s`): string {
  return `${count.toLocaleString("en-US")} ${count === 1 ? singular : plural}`;
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
