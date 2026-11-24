"use client";

import Link from "next/link";
import { useState } from "react";

import {
  NOT_VERIFIED_AT_SAVE,
  type DownloadProblem,
  type SelectedModel,
  type SelectionModelFileChecks,
} from "@/lib/download-selection";

import { ProblemNotice } from "./FollowedArchives";
import styles from "./download-selection.module.css";

type FileChecksPage = { models: SelectionModelFileChecks[]; nextCursor: string | null };

/**
 * A saved selection's files with each one's last check (researchers' Q10): the
 * checksum job's last result and time, the reuse terms, and "not verified
 * against the bytes at save time" where the SHA-256 was recorded after the
 * save or is not recorded. Loaded only when asked for, a page of models at a
 * time, through the website server.
 */
export default function SelectionFileChecks({
  selectionId,
  picked,
}: {
  selectionId: string;
  /** The working selection's picked models, for their titles. */
  picked: SelectedModel[];
}) {
  const [models, setModels] = useState<SelectionModelFileChecks[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [started, setStarted] = useState(false);
  const [done, setDone] = useState(false);
  const [loading, setLoading] = useState(false);
  const [problem, setProblem] = useState<DownloadProblem | null>(null);
  const titles = new Map(picked.map((model) => [model.id, model.title]));

  const load = async (after: string | null) => {
    setStarted(true);
    setLoading(true);
    setProblem(null);
    try {
      const params = after ? `?${new URLSearchParams({ cursor: after }).toString()}` : "";
      const response = await fetch(`/download/selections/${encodeURIComponent(selectionId)}/files${params}`, {
        cache: "no-store",
      });
      const body = (await response.json().catch(() => null)) as FileChecksPage | { problem: DownloadProblem } | null;
      if (body === null || !response.ok || "problem" in body) {
        setProblem(
          body !== null && "problem" in body
            ? body.problem
            : { code: "failed", detail: `The website answered ${response.status}`, retryAfterSeconds: null },
        );
        return;
      }
      setModels((current) => (after === null ? body.models : [...current, ...body.models]));
      setCursor(body.nextCursor);
      setDone(body.nextCursor === null);
    } catch {
      setProblem({ code: "unreachable", detail: null, retryAfterSeconds: null });
    } finally {
      setLoading(false);
    }
  };

  if (!started) {
    return (
      <p className={styles.help}>
        <button type="button" className={styles.linkButton} onClick={() => void load(null)}>
          Show each file&rsquo;s last check
        </button>
      </p>
    );
  }
  return (
    <section className={styles.section} aria-label="Files and their last check" data-file-checks="">
      <h3 className={styles.sectionTitle}>Files and their last check</h3>
      {models.map((model) => (
        <div key={model.modelId} data-file-checks-model={model.modelId}>
          <p className={styles.help}>
            <Link href={`/entries/${encodeURIComponent(model.entryId)}/models/${encodeURIComponent(model.modelId)}`}>
              {titles.get(model.modelId) ?? model.modelId}
            </Link>
          </p>
          {model.files.length === 0 ? (
            <p className={styles.muted}>No files.</p>
          ) : (
            <ul className={styles.reviewList}>
              {model.files.map((file) => (
                <li key={file.fileId} className={styles.reviewItem} data-file-check={file.fileId}>
                  <span className={styles.reviewText}>
                    <span className={styles.mono}>{file.name}</span>
                    <span className={styles.muted}>
                      {file.check} · {file.terms}
                    </span>
                    {file.notVerifiedAtSave ? (
                      <span className={styles.atSaveMark} data-not-verified-at-save="">
                        {NOT_VERIFIED_AT_SAVE}
                      </span>
                    ) : null}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
      {loading ? <p className={styles.muted}>Loading the files…</p> : null}
      {problem !== null ? <ProblemNotice problem={problem} /> : null}
      {!done && !loading ? (
        <button
          type="button"
          className={styles.linkButton}
          onClick={() => void load(cursor)}
        >
          {problem !== null ? "Try again" : "Show more"}
        </button>
      ) : null}
    </section>
  );
}
