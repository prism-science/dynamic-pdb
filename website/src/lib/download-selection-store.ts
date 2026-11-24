import {
  emptySelection,
  limitMessage,
  parseStoredSelection,
  reusableSelectionId,
  selectionFingerprint,
  selectionRequest,
  serializeSelection,
  withDownload,
  withFrozen,
  withRetry,
  type ArchiveActionResult,
  type DownloadProblem,
  type FileChoice,
  type SelectionChange,
  type StartDownloadInput,
  type StartDownloadResult,
  type WorkingSelection,
} from "@/lib/download-selection";

/**
 * The download selection of one browser tab, outside React: pages come and go
 * (each mounts its own panel), but the selection, a download being started and
 * the round of archive tracking live here, so a start that finishes after the
 * reader moved to another page lands in the selection that page shows. The
 * browser's storage keeps the selection across reloads and other tabs.
 */

export type SelectionStoreState = {
  /** False until the stored selection was read. */
  loaded: boolean;
  selection: WorkingSelection;
  /** A change the API's limits refused, or a storage problem, until dismissed. */
  notice: string | null;
  /** The operation waiting for the server; another one waits for it. */
  pending: PendingOperation | null;
  /** Why the last operation failed, until the next one. */
  problem: OperationProblem | null;
  /**
   * Counts successful operations, so the archives are followed afresh after
   * each, even when the server returns an archive already followed.
   */
  startRound: number;
};

/**
 * start: save and plan the preview. prepare: build the planned archive. retry:
 * fetch failed files again. renew: ask again for an expired or failed archive.
 */
export type PendingOperation = "start" | "prepare" | "retry" | "renew";

export type OperationProblem = {
  operation: PendingOperation;
  /** The archive the operation was about; null for a start. */
  archiveId: string | null;
  problem: DownloadProblem;
};

export type SelectionStorage = {
  read: () => string | null;
  /** False when the browser refused to keep the value. */
  write: (value: string) => boolean;
};

export type StartDownload = (input: StartDownloadInput) => Promise<StartDownloadResult>;
export type ArchiveAction = (downloadId: string) => Promise<ArchiveActionResult>;
export type RenewArchive = (selectionId: string, fileChoice: FileChoice) => Promise<ArchiveActionResult>;

export type SelectionStore = {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => SelectionStoreState;
  /** Reads the stored selection once; later calls do nothing. */
  load: () => void;
  /** Reads the stored selection again, after another tab changed it. */
  reload: () => void;
  apply: (change: (selection: WorkingSelection) => SelectionChange) => void;
  update: (change: (selection: WorkingSelection) => WorkingSelection) => void;
  dismissNotice: () => void;
  /** Saves the selection (unless saved unchanged) and plans its archive's preview. */
  startDownload: (fileChoice: FileChoice) => Promise<void>;
  /** Builds a followed archive whose preview the reader has seen. */
  prepareDownload: (archiveId: string) => Promise<void>;
  /** Asks for a small archive of one followed archive's retryable failed files. */
  retryFailedFiles: (archiveId: string) => Promise<void>;
  /**
   * Asks again for the followed archive once it expired or failed: a new
   * archive of its own saved selection and file choice, whatever the working
   * selection holds now.
   */
  renewDownload: (archiveId: string) => Promise<void>;
};

export const INITIAL_SELECTION_STORE_STATE: SelectionStoreState = Object.freeze({
  loaded: false,
  selection: emptySelection(),
  notice: null,
  pending: null,
  problem: null,
  startRound: 0,
});

export const UNREADABLE_SELECTION_NOTICE =
  "The selection kept in this browser could not be read, so it starts empty.";
export const UNSTORED_SELECTION_NOTICE =
  "This browser does not keep the selection, so it lasts only while this tab is open.";

export function createSelectionStore(dependencies: {
  storage: SelectionStorage;
  start: StartDownload;
  prepare: ArchiveAction;
  retry: ArchiveAction;
  renew: RenewArchive;
}): SelectionStore {
  let state: SelectionStoreState = INITIAL_SELECTION_STORE_STATE;
  const listeners = new Set<() => void>();

  const set = (changes: Partial<SelectionStoreState>) => {
    state = { ...state, ...changes };
    for (const listener of listeners) {
      listener();
    }
  };

  const commit = (selection: WorkingSelection, changes: Partial<SelectionStoreState> = {}) => {
    const kept = dependencies.storage.write(serializeSelection(selection));
    set({ ...changes, selection, ...(kept ? {} : { notice: UNSTORED_SELECTION_NOTICE }) });
  };

  /**
   * Runs prepare, retry or renew on one archive; on success, next is the
   * selection to keep and the archives are followed afresh.
   */
  const runArchiveAction = async (
    operation: Exclude<PendingOperation, "start">,
    archiveId: string,
    action: ArchiveAction,
    next: (result: Extract<ArchiveActionResult, { status: "done" }>) => WorkingSelection,
  ) => {
    set({ pending: operation, problem: null });
    let result: ArchiveActionResult;
    try {
      result = await action(archiveId);
    } catch {
      set({ pending: null, problem: unreachable(operation, archiveId) });
      return;
    }
    switch (result.status) {
      case "done":
        commit(next(result), { pending: null, startRound: state.startRound + 1 });
        return;
      case "failed":
        set({ pending: null, problem: { operation, archiveId, problem: result.problem } });
        return;
      default:
        set({ pending: null });
        throw new Error(`unknown ${operation} result: ${String((result as { status: never }).status)}`);
    }
  };

  const readStored = () => {
    const stored = parseStoredSelection(dependencies.storage.read());
    set({
      loaded: true,
      selection: stored.selection,
      ...(stored.problem === "unreadable" ? { notice: UNREADABLE_SELECTION_NOTICE } : {}),
    });
  };

  return {
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSnapshot: () => state,
    load: () => {
      if (!state.loaded) {
        readStored();
      }
    },
    reload: readStored,
    apply: (change) => {
      const result = change(state.selection);
      if ("limit" in result) {
        set({ notice: limitMessage(result.limit) });
        return;
      }
      commit(result.selection);
    },
    update: (change) => commit(change(state.selection)),
    dismissNotice: () => set({ notice: null }),
    startDownload: async (fileChoice) => {
      if (state.pending !== null) {
        return;
      }
      const startedFrom = state.selection;
      const fingerprint = selectionFingerprint(startedFrom);
      set({ pending: "start", problem: null });
      let result: StartDownloadResult;
      try {
        result = await dependencies.start({
          selection: selectionRequest(startedFrom),
          savedSelectionId: reusableSelectionId(startedFrom),
          fileChoice,
        });
      } catch {
        set({ pending: null, problem: unreachable("start", null) });
        return;
      }
      // Merged into the selection as it is now: the reader may have changed it
      // meanwhile. A saved selection is kept even when asking for its archive
      // failed, so trying again reuses it instead of saving another.
      const current = state.selection;
      const withSaved =
        result.saved === null ? current : withFrozen(current, { ...result.saved, fingerprint });
      switch (result.status) {
        case "started": {
          const followed = current.download;
          commit(
            withDownload(withSaved, {
              id: result.download.id,
              selectionId: result.download.attributes.selection_id,
              fileChoice: result.download.attributes.file_choice,
              // The server returns the archive already asked for; its retries stay.
              retryIds: followed !== null && followed.id === result.download.id ? followed.retryIds : [],
            }),
            { pending: null, startRound: state.startRound + 1 },
          );
          return;
        }
        case "failed":
          commit(withSaved, { pending: null, problem: { operation: "start", archiveId: null, problem: result.problem } });
          return;
        default:
          set({ pending: null });
          throw new Error(`unknown start result: ${String((result as { status: never }).status)}`);
      }
    },
    prepareDownload: async (archiveId) => {
      if (state.pending !== null || !isFollowed(state.selection, archiveId)) {
        return;
      }
      await runArchiveAction("prepare", archiveId, dependencies.prepare, () => state.selection);
    },
    retryFailedFiles: async (archiveId) => {
      if (state.pending !== null || !isFollowed(state.selection, archiveId)) {
        return;
      }
      await runArchiveAction("retry", archiveId, dependencies.retry, (result) =>
        // Another tab may have moved the selection to another archive meanwhile:
        // a retry belongs only under the archive it retries.
        isFollowed(state.selection, archiveId) ? withRetry(state.selection, result.download.id) : state.selection,
      );
    },
    renewDownload: async (archiveId) => {
      const expired = state.selection.download;
      // Only the first archive is asked for again here; a retry is asked for
      // again through the archive it retries.
      if (state.pending !== null || expired === null || expired.id !== archiveId) {
        return;
      }
      const { selectionId, fileChoice } = expired;
      await runArchiveAction(
        "renew",
        archiveId,
        () => dependencies.renew(selectionId, fileChoice),
        (result) => {
          const now = state.selection.download;
          // Another tab may have moved the selection to another archive meanwhile.
          if (now === null || now.id !== archiveId) {
            return state.selection;
          }
          return withDownload(state.selection, {
            id: result.download.id,
            selectionId,
            fileChoice,
            // The retries belong to the expired archive, unless the server returned it again.
            retryIds: result.download.id === archiveId ? now.retryIds : [],
          });
        },
      );
    },
  };
}

function unreachable(operation: PendingOperation, archiveId: string | null): OperationProblem {
  return { operation, archiveId, problem: { code: "unreachable", detail: null, retryAfterSeconds: null } };
}

/** Whether the selection follows this archive: the first one or one of its retries. */
function isFollowed(selection: WorkingSelection, archiveId: string): boolean {
  const download = selection.download;
  return download !== null && (download.id === archiveId || download.retryIds.includes(archiveId));
}
