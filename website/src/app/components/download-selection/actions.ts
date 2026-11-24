"use server";

import {
  downloadProblem,
  isDownloadId,
  isSelectionId,
  isUnknownSelection,
  prepareArchive,
  requestArchive,
  retryArchive,
  saveSelection,
} from "@/lib/api/downloads";
import { getAuthSession } from "@/lib/auth/session";
import {
  isFileChoice,
  MAX_LEFT_OUT_MODELS,
  MAX_PICKED_MODELS,
  MAX_WHOLE_SEARCHES,
  type ArchiveActionResult,
  type SavedSelection,
  type StartDownloadInput,
  type StartDownloadResult,
} from "@/lib/download-selection";

/**
 * Saves the working selection (unless it was saved already) and asks for its
 * archive's preview (prepare false: the worker adds the files up and stops),
 * as the signed-in reader, so a reviewer may save searches that use the review
 * filter. A saved selection that the server no longer knows is saved again
 * once. prepareArchiveAction then builds the archive.
 */
export async function startDownloadAction(input: StartDownloadInput): Promise<StartDownloadResult> {
  const request = readStartInput(input);
  if (request === null) {
    return {
      status: "failed",
      saved: null,
      problem: { code: "invalid", detail: "The selection is not in a form this page can send.", retryAfterSeconds: null },
    };
  }
  const session = await getAuthSession();
  if (request.savedSelectionId !== null) {
    try {
      const download = await requestArchive(request.savedSelectionId, request.fileChoice, false);
      return { status: "started", saved: null, download };
    } catch (error) {
      // Only a selection the server no longer knows is saved anew.
      if (!isUnknownSelection(error)) {
        return { status: "failed", saved: null, problem: downloadProblem(error) };
      }
    }
  }
  let saved: SavedSelection;
  try {
    saved = await saveSelection(request.selection, session?.token);
  } catch (error) {
    return { status: "failed", saved: null, problem: downloadProblem(error) };
  }
  try {
    const download = await requestArchive(saved.id, request.fileChoice, false);
    return { status: "started", saved, download };
  } catch (error) {
    return { status: "failed", saved, problem: downloadProblem(error) };
  }
}

/** Builds an archive whose preview the reader has seen. */
export async function prepareArchiveAction(downloadId: string): Promise<ArchiveActionResult> {
  return archiveAction(downloadId, prepareArchive);
}

/** Asks for a small archive of a ready archive's failed files that a retry may fetch. */
export async function retryArchiveAction(downloadId: string): Promise<ArchiveActionResult> {
  return archiveAction(downloadId, retryArchive);
}

/**
 * Asks again for an expired or failed archive: a new archive of the same saved
 * selection and file choice, never of the working selection, which the reader
 * may have changed or cleared since. As for a start, the preview comes first.
 */
export async function renewArchiveAction(selectionId: unknown, fileChoice: unknown): Promise<ArchiveActionResult> {
  if (typeof selectionId !== "string" || !isSelectionId(selectionId) || !isFileChoice(fileChoice)) {
    return {
      status: "failed",
      problem: { code: "invalid", detail: "Not a saved selection and file choice.", retryAfterSeconds: null },
    };
  }
  try {
    return { status: "done", download: await requestArchive(selectionId, fileChoice, false) };
  } catch (error) {
    return { status: "failed", problem: downloadProblem(error) };
  }
}

async function archiveAction(
  downloadId: unknown,
  call: (downloadId: string) => ReturnType<typeof prepareArchive>,
): Promise<ArchiveActionResult> {
  if (typeof downloadId !== "string" || !isDownloadId(downloadId)) {
    return {
      status: "failed",
      problem: { code: "invalid", detail: "Not an archive request ID.", retryAfterSeconds: null },
    };
  }
  try {
    return { status: "done", download: await call(downloadId) };
  } catch (error) {
    return { status: "failed", problem: downloadProblem(error) };
  }
}

/**
 * The input checked again on the server: a Server Function can be called with
 * any body, not only the panel's. The API checks the values themselves.
 */
function readStartInput(input: unknown): StartDownloadInput | null {
  if (typeof input !== "object" || input === null) {
    return null;
  }
  const { selection, savedSelectionId, fileChoice } = input as Record<string, unknown>;
  if (typeof selection !== "object" || selection === null || !isFileChoice(fileChoice)) {
    return null;
  }
  if (savedSelectionId !== null && typeof savedSelectionId !== "string") {
    return null;
  }
  const body = selection as Record<string, unknown>;
  const modelIds = stringList(body.model_ids, MAX_PICKED_MODELS);
  const searches = stringList(body.searches, MAX_WHOLE_SEARCHES);
  const excluded = stringList(body.exclude_model_ids, MAX_LEFT_OUT_MODELS);
  if (modelIds === null || searches === null || excluded === null || typeof body.add_deposited_models !== "boolean") {
    return null;
  }
  return {
    selection: {
      model_ids: modelIds,
      searches,
      exclude_model_ids: excluded,
      add_deposited_models: body.add_deposited_models,
    },
    savedSelectionId,
    fileChoice,
  };
}

function stringList(value: unknown, maxItems: number): string[] | null {
  if (!Array.isArray(value) || value.length > maxItems) {
    return null;
  }
  return value.every((item) => typeof item === "string") ? (value as string[]) : null;
}
