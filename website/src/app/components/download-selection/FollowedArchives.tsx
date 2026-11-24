"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import {
  countLines,
  fileChoiceText,
  formatDecimalBytes,
  problemMessage,
  reasonCodeText,
  REUSE_LICENSE_URL,
  REUSE_TERMS,
  withDownload,
  type DownloadProblem,
  type DownloadView,
  type PreviewSummary,
} from "@/lib/download-selection";
import type { DownloadFailure } from "@/lib/api/downloads";
import { scriptPageHref } from "@/lib/download-instructions";
import type { OperationProblem } from "@/lib/download-selection-store";

import { useDownloadSelection, type FollowedArchive } from "./DownloadSelectionProvider";
import styles from "./download-selection.module.css";

/** Where "Make the selection smaller" takes the reader: the top of the review. */
export const SELECTION_REVIEW_ID = "download-selection-review";

/**
 * The archive the panel follows, and the retries of its failed files under it
 * (L2 UI decision 4A), each with what can be done next: build it after its
 * preview, download it, see why files were left out, fetch failed files again.
 */
export default function FollowedArchives() {
  const context = useDownloadSelection();
  if (context === null || context.selection.download === null) {
    return null;
  }
  const download = context.selection.download;
  return (
    <section className={styles.section} aria-live="polite" data-archive-section="">
      <h3 className={styles.sectionTitle}>Archive: {fileChoiceText(download.fileChoice).label}</h3>
      {context.archives.map((archive) => (
        <ArchiveCard key={archive.id} archive={archive} />
      ))}
      <p className={styles.actions}>
        <button
          type="button"
          className={styles.linkButton}
          onClick={() => context.update((current) => withDownload(current, null))}
        >
          Forget this archive
        </button>
      </p>
    </section>
  );
}

function ArchiveCard({ archive }: { archive: FollowedArchive }) {
  const context = useDownloadSelection();
  if (context === null) {
    return null;
  }
  const state = archive.state;
  return (
    <div className={archive.isRetry ? styles.retryCard : styles.archiveCard} data-archive-id={archive.id}>
      {archive.isRetry ? <h4 className={styles.cardTitle}>Retry of the files that could not be read</h4> : null}
      {(() => {
        switch (state.kind) {
          case "none":
          case "loading":
            return <p className={styles.muted}>Checking the archive…</p>;
          case "problem":
            return (
              <>
                <ProblemNotice problem={state.problem} />
                <p className={styles.actions}>
                  <button type="button" className={styles.secondaryButton} onClick={context.recheckArchives}>
                    Check again
                  </button>
                </p>
              </>
            );
          case "unknown":
            return (
              <p className={styles.problem} role="alert">
                This page does not understand the server&rsquo;s answer about the archive ({state.message}).
              </p>
            );
          case "view":
            return <ArchiveView view={state.view} archiveId={archive.id} />;
          default:
            throw new Error(`unknown archive state: ${String((state as { kind: never }).kind)}`);
        }
      })()}
      <OperationProblemNotice problem={context.problem} archiveId={archive.id} />
    </div>
  );
}

function ArchiveView({ view, archiveId }: { view: DownloadView; archiveId: string }) {
  const context = useDownloadSelection();
  if (context === null) {
    return null;
  }
  switch (view.kind) {
    case "in_progress":
      return (
        <>
          <p className={styles.progress} role="status">
            {view.step}… You can keep browsing; this panel follows the archive.
          </p>
          {view.preview !== null ? <PreviewDetails preview={view.preview} /> : null}
        </>
      );
    case "preview":
      return (
        <>
          <PreviewDetails preview={view.preview} />
          {view.preview.overLimit ? (
            <LimitNotice preview={view.preview} message={null} maxArchiveBytes={view.preview.maxArchiveBytes} />
          ) : (
            <p className={styles.actions}>
              <button
                type="button"
                className={styles.primaryButton}
                disabled={context.pending !== null}
                onClick={() => context.prepareDownload(archiveId)}
              >
                {context.pending === "prepare" ? "Asking for the archive…" : "Build the archive"}
              </button>
              <span className={styles.muted}>Nothing is read until you build it.</span>
            </p>
          )}
        </>
      );
    case "ready":
      return <ReadyArchive view={view} archiveId={archiveId} />;
    case "blocked":
      return (
        <>
          {view.preview !== null ? <PreviewDetails preview={view.preview} /> : null}
          <LimitNotice preview={view.preview} message={view.message} maxArchiveBytes={view.maxArchiveBytes} />
        </>
      );
    case "failed":
    case "expired":
      return (
        <>
          <p className={styles.problem} role="alert">
            {view.message}
          </p>
          <p className={styles.actions}>
            {view.retryOf !== null ? (
              // A retry is asked for again through the archive it retries, never as a new archive.
              <button
                type="button"
                className={styles.secondaryButton}
                disabled={context.pending !== null}
                onClick={() => context.retryFailedFiles(view.retryOf as string)}
              >
                Retry the failed files again
              </button>
            ) : (
              <button
                type="button"
                className={styles.secondaryButton}
                disabled={context.pending !== null}
                onClick={() => context.renewDownload(archiveId)}
              >
                {context.pending === "renew" ? "Asking for the archive…" : "Check the archive again"}
              </button>
            )}
          </p>
        </>
      );
    default:
      throw new Error(`unknown archive view: ${String((view as { kind: never }).kind)}`);
  }
}

/** L1's preview: what the archive will hold, before anything is read. */
function PreviewDetails({ preview }: { preview: PreviewSummary }) {
  return (
    <div className={styles.preview} data-preview="">
      <p className={styles.previewHeadline}>{preview.headline}</p>
      <ul className={styles.previewList}>
        {preview.byType.map((item) => (
          <li key={item.type}>
            {item.label}: {item.files.toLocaleString("en-US")} ({item.size})
          </li>
        ))}
        <li data-preview-size="included">To download: {preview.included}</li>
        <li data-preview-size="linked">Linked in manifest.tsv, not in the zip: {preview.linked}</li>
        <li data-preview-terms="">Reuse terms: {preview.reuseTerms}</li>
        {preview.notVerifiedAtSave !== null ? <li data-preview-at-save="">{preview.notVerifiedAtSave}</li> : null}
        {preview.modelsWithoutFiles > 0 ? (
          <li>
            {preview.modelsWithoutFiles.toLocaleString("en-US")} selected{" "}
            {preview.modelsWithoutFiles === 1 ? "model has" : "models have"} no file of this choice; manifest.tsv
            says why.
          </li>
        ) : null}
        <li>
          Checksums: {preview.verification.verified.toLocaleString("en-US")} verified,{" "}
          {preview.verification.mismatch.toLocaleString("en-US")} not matching,{" "}
          {preview.verification.notVerified.toLocaleString("en-US")} not checked
        </li>
        {preview.relatedDatasets !== null ? <li>{preview.relatedDatasets}</li> : null}
      </ul>
      {preview.leftOut.length > 0 ? (
        <>
          <p className={styles.help}>Left out of the zip:</p>
          <ul className={styles.previewList} aria-label="Files left out of the zip">
            {preview.leftOut.map((item) => (
              <li key={item.code} data-left-out={item.code}>
                {item.files.toLocaleString("en-US")} {item.files === 1 ? "file" : "files"}: {item.text}
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </div>
  );
}

/** Over 10 GB: never built; a smaller selection or the script gets the files (researcher item 9). */
function LimitNotice({
  preview,
  message,
  maxArchiveBytes,
}: {
  preview: PreviewSummary | null;
  message: string | null;
  maxArchiveBytes: number;
}) {
  const context = useDownloadSelection();
  const selectionId = context?.selection.download?.selectionId ?? null;
  const limit = `${formatDecimalBytes(maxArchiveBytes)} (${maxArchiveBytes.toLocaleString("en-US")} bytes, before compression)`;
  const known = preview !== null && preview.includedBytes !== null;
  const size = known
    ? `This archive would hold ${formatDecimalBytes(preview.includedBytes as number)}; one archive holds at most ${limit}.`
    : `One archive holds at most ${limit}.`;
  return (
    <div className={styles.problem} role="alert" data-limit-notice="">
      <p className={styles.problemTitle}>Too large for one archive</p>
      <p>{size}</p>
      {/* The API's message says the same in its own words; it adds the size only without a preview. */}
      {message !== null && !known ? <p>{message}</p> : null}
      <p className={styles.actions}>
        <button
          type="button"
          className={styles.secondaryButton}
          onClick={() => {
            const review = document.getElementById(SELECTION_REVIEW_ID);
            review?.scrollIntoView({ block: "start" });
            review?.focus();
          }}
        >
          Make the selection smaller
        </button>
        <Link className={styles.linkButton} href={scriptPageHref(selectionId)}>
          Use the script
        </Link>
      </p>
    </div>
  );
}

function ReadyArchive({ view, archiveId }: { view: Extract<DownloadView, { kind: "ready" }>; archiveId: string }) {
  const context = useDownloadSelection();
  if (context === null) {
    return null;
  }
  const failed = view.counts === null ? 0 : view.counts.unavailable;
  return (
    <div className={styles.ready}>
      {view.retryOf !== null ? (
        <p className={styles.help}>This archive holds only the files the first one could not read.</p>
      ) : null}
      <p>
        <a className={styles.downloadLink} href={view.archiveUrl} download>
          Download zip{view.archiveBytes !== null ? ` (${formatDecimalBytes(view.archiveBytes)})` : ""}
        </a>
      </p>
      <div className={styles.transferHelp} data-transfer-help="">
        <p>
          Download stopped? Download again: the link{view.expiresAt !== null ? ` works until ${formatDate(view.expiresAt)}` : " keeps working"},
          and your browser resumes where it can.{" "}
          <button type="button" className={styles.linkButton} onClick={context.recheckArchives}>
            Check the link
          </button>
        </p>
        {view.archiveSha256 !== null ? (
          <p>
            To check the file, its SHA-256 is <code className={styles.mono}>{view.archiveSha256}</code>; compare it
            with <code className={styles.mono}>shasum -a 256 &lt;file&gt;.zip</code>.
          </p>
        ) : null}
      </div>
      {view.manifestUrl !== null ? (
        <p>
          <a href={view.manifestUrl} target="_blank" rel="noreferrer">
            manifest.tsv
          </a>{" "}
          <span className={styles.muted}>(one row per file, also inside the zip)</span>
        </p>
      ) : null}
      {view.counts !== null ? (
        <ul className={styles.counts} aria-label="Files">
          {countLines(view.counts).map((line) => (
            <li key={line.key} data-count={line.key}>
              <strong>{line.count.toLocaleString("en-US")}</strong> {line.text}
            </li>
          ))}
        </ul>
      ) : null}
      {failed > 0 ? <FailuresList archiveId={archiveId} /> : null}
      {view.retryableFiles > 0 ? (
        <p className={styles.actions}>
          <button
            type="button"
            className={styles.secondaryButton}
            disabled={context.pending !== null}
            onClick={() => context.retryFailedFiles(archiveId)}
          >
            {context.pending === "retry"
              ? "Asking for them…"
              : `Retry the ${view.retryableFiles.toLocaleString("en-US")} ${view.retryableFiles === 1 ? "file" : "files"} that could not be read`}
          </button>
        </p>
      ) : null}
      <p className={styles.terms} data-reuse-terms="">
        {REUSE_TERMS}{" "}
        <a href={REUSE_LICENSE_URL} target="_blank" rel="noreferrer">
          About CC0 1.0
        </a>
      </p>
    </div>
  );
}

type FailuresPage = { items: DownloadFailure[]; nextCursor: string | null };

/** The files the archive could not vouch for, 20 at a time (L2 UI decision 3A). */
function FailuresList({ archiveId }: { archiveId: string }) {
  const [items, setItems] = useState<DownloadFailure[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [loading, setLoading] = useState(false);
  const [problem, setProblem] = useState<DownloadProblem | null>(null);

  const load = async (after: string | null, signal?: AbortSignal) => {
    setLoading(true);
    setProblem(null);
    try {
      const params = after ? `?${new URLSearchParams({ cursor: after }).toString()}` : "";
      const response = await fetch(`/download/archives/${encodeURIComponent(archiveId)}/failures${params}`, {
        cache: "no-store",
        signal,
      });
      const body = (await response.json().catch(() => null)) as FailuresPage | { problem: DownloadProblem } | null;
      if (signal?.aborted) {
        return;
      }
      if (body === null || !response.ok || "problem" in body) {
        setProblem(
          body !== null && "problem" in body
            ? body.problem
            : { code: "failed", detail: `The website answered ${response.status}`, retryAfterSeconds: null },
        );
        return;
      }
      setItems((current) => (after === null ? body.items : [...current, ...body.items]));
      setCursor(body.nextCursor);
      setDone(body.nextCursor === null);
    } catch {
      if (!signal?.aborted) {
        setProblem({ code: "unreachable", detail: null, retryAfterSeconds: null });
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    const request = new AbortController();
    void load(null, request.signal);
    return () => request.abort();
  }, [archiveId]);

  return (
    <div className={styles.failures}>
      <p className={styles.sectionTitle}>Files left out, and why</p>
      {items.length > 0 ? (
        <ul className={styles.reviewList} aria-label="Files the archive left out">
          {items.map((item) => (
            <li key={`${item.file_id}-${item.model_id}`} className={styles.reviewItem} data-failure={item.file_id}>
              <span className={styles.reviewText}>
                <span>
                  <span className={styles.mono}>{item.file_name}</span>
                  {item.retryable ? <span className={styles.retryableBadge}>a retry may fetch it</span> : null}
                </span>
                <span className={styles.muted} title={item.reason}>
                  {reasonCodeText(item.reason_code)}
                </span>
                <span className={styles.muted}>
                  Model{" "}
                  <Link href={`/entries/${encodeURIComponent(item.entry_id)}/models/${encodeURIComponent(item.model_id)}`}>
                    {item.model_id}
                  </Link>
                </span>
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {loading ? <p className={styles.muted}>Loading the list…</p> : null}
      {problem !== null ? <ProblemNotice problem={problem} /> : null}
      {!loading && (problem !== null || (!done && cursor !== null)) ? (
        <button type="button" className={styles.linkButton} onClick={() => void load(problem !== null && items.length === 0 ? null : cursor)}>
          {problem !== null ? "Try again" : "Show more"}
        </button>
      ) : null}
    </div>
  );
}

/** The last operation's problem, under the archive it was about. */
function OperationProblemNotice({ problem, archiveId }: { problem: OperationProblem | null; archiveId: string }) {
  if (problem === null || problem.archiveId !== archiveId) {
    return null;
  }
  return <ProblemNotice problem={problem.problem} />;
}

export function ProblemNotice({ problem }: { problem: DownloadProblem }) {
  const message = problemMessage(problem);
  return (
    <div className={styles.problem} role="alert">
      <p className={styles.problemTitle}>{message.title}</p>
      <p>{message.body}</p>
    </div>
  );
}

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString("en-US", { dateStyle: "medium", timeStyle: "short" });
}
