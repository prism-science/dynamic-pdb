"use client";

import { isEmptySelection, selectionSize, selectionSizeText } from "@/lib/download-selection";

import { useDownloadSelection, type ArchiveState } from "./DownloadSelectionProvider";
import SelectionDrawer from "./SelectionDrawer";
import styles from "./download-selection.module.css";

/**
 * The bar at the bottom of /browse and entry pages: how much is selected, how
 * the archive is coming along, and the way into the drawer. Hidden while
 * nothing is selected, no archive is followed and the drawer is closed.
 */
export default function SelectionBar() {
  const context = useDownloadSelection();
  if (context === null || !context.ready) {
    return null;
  }
  const { selection } = context;
  const empty = isEmptySelection(selection);
  // Kept while the drawer is open, so clearing the selection there does not close it.
  if (empty && selection.download === null && context.notice === null && !context.drawerOpen) {
    return null;
  }
  return (
    <>
      <div className={styles.bar} role="region" aria-label="Download selection">
        <p className={styles.barCount} aria-live="polite">
          {empty ? "Nothing selected" : `${capitalize(selectionSizeText(selectionSize(selection)))} selected`}
        </p>
        <ArchiveBadge archive={context.archives[0]?.state ?? { kind: "none" }} />
        {context.notice !== null && !context.drawerOpen ? (
          <p className={styles.barNotice} role="alert">
            {context.notice}{" "}
            <button type="button" className={styles.linkButton} onClick={context.dismissNotice}>
              Dismiss
            </button>
          </p>
        ) : null}
        <button type="button" className={styles.primaryButton} onClick={() => context.setDrawerOpen(true)}>
          Review and download
        </button>
      </div>
      <SelectionDrawer />
    </>
  );
}

/** The archive's state in a few words; a status region, so it is announced while the drawer is closed. */
function ArchiveBadge({ archive }: { archive: ArchiveState }) {
  const text = archiveBadgeText(archive);
  return (
    <p
      className={text === null ? styles.barArchiveEmpty : styles.barArchive}
      role="status"
      data-archive={archive.kind === "view" ? archive.view.kind : archive.kind}
    >
      {text}
    </p>
  );
}

function archiveBadgeText(archive: ArchiveState): string | null {
  switch (archive.kind) {
    case "none":
      return null;
    case "loading":
      return "Archive: checking…";
    case "problem":
    case "unknown":
      return "Archive: needs attention";
    case "view":
      switch (archive.view.kind) {
        case "in_progress":
          return `Archive: ${archive.view.step.toLowerCase()}…`;
        case "preview":
          return archive.view.preview.overLimit ? "Archive too large" : "Archive preview ready";
        case "ready":
          return "Archive ready";
        case "blocked":
          return "Archive too large";
        case "failed":
          return "Archive failed";
        case "expired":
          return "Archive expired";
        default:
          throw new Error(`unknown archive view: ${String((archive.view as { kind: never }).kind)}`);
      }
    default:
      throw new Error(`unknown archive state: ${String((archive as { kind: never }).kind)}`);
  }
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
