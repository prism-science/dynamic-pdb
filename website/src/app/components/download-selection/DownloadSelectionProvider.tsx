"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { createArchivePoller } from "@/lib/archive-poller";
import {
  downloadInProgress,
  downloadView,
  type DownloadAttributes,
  type DownloadProblem,
  type DownloadView,
  type FileChoice,
  type SelectionChange,
  type WorkingSelection,
} from "@/lib/download-selection";
import {
  INITIAL_SELECTION_STORE_STATE,
  type OperationProblem,
  type PendingOperation,
} from "@/lib/download-selection-store";

import { loadSelectionStore, selectionStore } from "./selectionStore";

/** Where an archive the panel follows stands. */
export type ArchiveState =
  | { kind: "none" }
  | { kind: "loading" }
  | { kind: "view"; view: DownloadView }
  | { kind: "problem"; problem: DownloadProblem }
  /** The API sent something this website does not know; polling stops. */
  | { kind: "unknown"; message: string };

/** One archive the panel follows: the first, or a retry of failed files. */
export type FollowedArchive = { id: string; isRetry: boolean; state: ArchiveState };

type DownloadSelectionContextValue = {
  /** False until the stored selection is read; checkboxes wait for it. */
  ready: boolean;
  selection: WorkingSelection;
  /** A change the API's limits refused, or a storage problem, until dismissed. */
  notice: string | null;
  dismissNotice: () => void;
  /** Applies a change that may break a limit; a refused change leaves a notice. */
  apply: (change: (selection: WorkingSelection) => SelectionChange) => void;
  /** Applies a change that cannot break a limit. */
  update: (change: (selection: WorkingSelection) => WorkingSelection) => void;
  /** Saves the selection (unless it was saved unchanged) and plans its archive's preview. */
  startDownload: (fileChoice: FileChoice) => void;
  /** Builds a followed archive whose preview the reader has seen. */
  prepareDownload: (archiveId: string) => void;
  /** Asks for a small archive of one followed archive's retryable failed files. */
  retryFailedFiles: (archiveId: string) => void;
  /** Asks again for an expired or failed archive, with its own saved selection and file choice. */
  renewDownload: (archiveId: string) => void;
  /** The operation waiting for the server; another one waits for it. */
  pending: PendingOperation | null;
  /** Why the last operation failed, until the next one. */
  problem: OperationProblem | null;
  /** The first archive, then its retries, oldest first; empty when none is followed. */
  archives: FollowedArchive[];
  /** Asks for the archives' status again, after a problem or to check a link. */
  recheckArchives: () => void;
  drawerOpen: boolean;
  setDrawerOpen: (open: boolean) => void;
};

const DownloadSelectionContext = createContext<DownloadSelectionContextValue | null>(null);

/** The panel's state, or null where the page offers no bulk download. */
export function useDownloadSelection(): DownloadSelectionContextValue | null {
  return useContext(DownloadSelectionContext);
}

const serverSnapshot = () => INITIAL_SELECTION_STORE_STATE;

/**
 * The panel of one page, over the tab's selection store (selectionStore.ts):
 * the selection and an operation waiting for the server outlive the page, so
 * moving to another page keeps both. Each page follows the archives itself.
 * Mounted only where the server offers bulk download.
 */
export default function DownloadSelectionProvider({ children }: { children: ReactNode }) {
  const state = useSyncExternalStore(selectionStore.subscribe, selectionStore.getSnapshot, serverSnapshot);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const download = state.loaded ? state.selection.download : null;
  const archiveIds = useMemo(
    () => (download === null ? [] : [download.id, ...download.retryIds]),
    [download],
  );
  const followed = useArchiveStatuses(archiveIds, state.startRound);

  useEffect(() => {
    loadSelectionStore();
  }, []);

  const startDownload = useCallback((fileChoice: FileChoice) => {
    void selectionStore.startDownload(fileChoice);
  }, []);
  const prepareDownload = useCallback((archiveId: string) => {
    void selectionStore.prepareDownload(archiveId);
  }, []);
  const retryFailedFiles = useCallback((archiveId: string) => {
    void selectionStore.retryFailedFiles(archiveId);
  }, []);
  const renewDownload = useCallback((archiveId: string) => {
    void selectionStore.renewDownload(archiveId);
  }, []);

  const archives = useMemo<FollowedArchive[]>(
    () =>
      archiveIds.map((id, index) => ({
        id,
        isRetry: index > 0,
        state: followed.states[id] ?? { kind: "loading" },
      })),
    [archiveIds, followed.states],
  );

  const value = useMemo<DownloadSelectionContextValue>(
    () => ({
      ready: state.loaded,
      selection: state.selection,
      notice: state.notice,
      dismissNotice: selectionStore.dismissNotice,
      apply: selectionStore.apply,
      update: selectionStore.update,
      startDownload,
      prepareDownload,
      retryFailedFiles,
      renewDownload,
      pending: state.pending,
      problem: state.problem,
      archives,
      recheckArchives: followed.recheck,
      drawerOpen,
      setDrawerOpen,
    }),
    [state, startDownload, prepareDownload, retryFailedFiles, renewDownload, archives, followed.recheck, drawerOpen],
  );

  return <DownloadSelectionContext.Provider value={value}>{children}</DownloadSelectionContext.Provider>;
}

/**
 * Follows each archive through the website's route, one poller each (see
 * createArchivePoller), until it is ready, blocked, failed or expired. A new
 * operation (startRound) or a recheck follows them all afresh.
 */
function useArchiveStatuses(
  archiveIds: string[],
  startRound: number,
): { states: Record<string, ArchiveState>; recheck: () => void } {
  const [states, setStates] = useState<Record<string, ArchiveState>>({});
  const [attempt, setAttempt] = useState(0);
  const key = archiveIds.join(",");

  useEffect(() => {
    const ids = key === "" ? [] : key.split(",");
    setStates(Object.fromEntries(ids.map((id) => [id, { kind: "loading" } as ArchiveState])));
    const pollers = ids.map((id) =>
      createArchivePoller<ArchiveState>({
        read: (signal) => readArchiveStatus(id, signal),
        keepPolling: (outcome) => outcome.kind === "view" && downloadInProgress(outcome.view),
        onOutcome: (outcome) => setStates((current) => ({ ...current, [id]: outcome })),
        isHidden: () => document.hidden,
        setTimer: (callback, ms) => setTimeout(callback, ms),
        clearTimer: (timer) => clearTimeout(timer as ReturnType<typeof setTimeout>),
      }),
    );
    const onVisibility = () => {
      for (const poller of pollers) {
        poller.visibilityChanged();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    for (const poller of pollers) {
      poller.start();
    }
    return () => {
      for (const poller of pollers) {
        poller.stop();
      }
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [key, attempt, startRound]);

  const recheck = useCallback(() => setAttempt((value) => value + 1), []);
  return { states, recheck };
}

type ArchiveRouteBody =
  | { download: { id: string; attributes: DownloadAttributes } }
  | { problem: DownloadProblem };

async function readArchiveStatus(downloadId: string, signal: AbortSignal): Promise<ArchiveState> {
  let body: ArchiveRouteBody | null;
  try {
    const response = await fetch(`/download/archives/${encodeURIComponent(downloadId)}`, {
      cache: "no-store",
      signal,
    });
    body = (await response.json().catch(() => null)) as ArchiveRouteBody | null;
  } catch {
    return { kind: "problem", problem: { code: "unreachable", detail: null, retryAfterSeconds: null } };
  }
  if (body === null) {
    return { kind: "problem", problem: { code: "failed", detail: null, retryAfterSeconds: null } };
  }
  if ("problem" in body) {
    return { kind: "problem", problem: body.problem };
  }
  try {
    return { kind: "view", view: downloadView(body.download.attributes) };
  } catch (error) {
    return { kind: "unknown", message: error instanceof Error ? error.message : String(error) };
  }
}
