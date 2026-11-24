"use client";

import { createSelectionStore } from "@/lib/download-selection-store";
import { SELECTION_STORAGE_KEY } from "@/lib/download-selection";

import { prepareArchiveAction, renewArchiveAction, retryArchiveAction, startDownloadAction } from "./actions";

/**
 * The tab's one selection store: every page's panel reads and changes it, so
 * the selection and a download being started survive moving between pages.
 * Only read and changed in the browser; server renders use the initial state.
 */
export const selectionStore = createSelectionStore({
  storage: {
    read: () => {
      try {
        return window.localStorage.getItem(SELECTION_STORAGE_KEY);
      } catch {
        return null;
      }
    },
    write: (value) => {
      try {
        window.localStorage.setItem(SELECTION_STORAGE_KEY, value);
        return true;
      } catch {
        return false;
      }
    },
  },
  start: startDownloadAction,
  prepare: prepareArchiveAction,
  retry: retryArchiveAction,
  renew: renewArchiveAction,
});

let followingOtherTabs = false;

/** Reads the stored selection once, and follows changes other tabs make. */
export function loadSelectionStore(): void {
  selectionStore.load();
  if (followingOtherTabs) {
    return;
  }
  followingOtherTabs = true;
  window.addEventListener("storage", (event) => {
    if (event.key === SELECTION_STORAGE_KEY) {
      selectionStore.reload();
    }
  });
}
