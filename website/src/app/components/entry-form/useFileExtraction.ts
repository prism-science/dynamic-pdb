"use client";

import { useCallback, useRef, useState } from "react";

import { extractUploadedFileAction, type FileExtractionResult } from "./fileExtractionAction";
import { readExtraction, type ExtractedFile } from "./fileExtraction";

/** An uploaded mmCIF the server can read: its artifact id, URL and checksum. */
export type UploadedStructure = { id: string; name: string; url: string; sha256: string };

/** Where reading one file on the server stands. No entry: not asked, or the
 *  server does not read it (turned off, not an mmCIF), so the preview stays. */
export type FileReadState =
  | { status: "reading"; file: UploadedStructure }
  | { status: "read"; file: UploadedStructure }
  | { status: "failed"; file: UploadedStructure; message: string };

/** How long to wait before the one automatic retry when the server is busy. */
const BUSY_RETRY_MS = 5000;

/**
 * Reads uploaded mmCIF files on the server and hands each result to onRead.
 * A busy server is asked once more after a pause; a failure stays listed with
 * its message until read(file) is called again (the Retry button). The draft
 * is never touched here: onRead decides what to fill.
 */
export function useFileExtraction(
  onRead: (file: UploadedStructure, extracted: ExtractedFile) => void,
  waitMs: number = BUSY_RETRY_MS,
) {
  const [states, setStates] = useState<Record<string, FileReadState>>({});
  const onReadRef = useRef(onRead);
  onReadRef.current = onRead;

  const setState = (id: string, state: FileReadState | null) =>
    setStates((current) => {
      const next = { ...current };
      if (state) {
        next[id] = state;
      } else {
        delete next[id];
      }
      return next;
    });

  const read = useCallback(
    async (file: UploadedStructure) => {
      setState(file.id, { status: "reading", file });
      const result = await askWithRetry(
        () => extractUploadedFileAction({ artifactId: file.id, objectUrl: file.url, sha256: file.sha256 }),
        waitMs,
      );
      switch (result.status) {
        case "read":
          try {
            onReadRef.current(file, readExtraction(result.extraction));
            setState(file.id, { status: "read", file });
          } catch (error) {
            setState(file.id, {
              status: "failed",
              file,
              message: error instanceof Error ? error.message : "its values could not be used",
            });
          }
          return;
        case "off":
        case "not_read":
          setState(file.id, null);
          return;
        case "busy":
          setState(file.id, { status: "failed", file, message: "the server is busy reading other files" });
          return;
        case "failed":
          setState(file.id, { status: "failed", file, message: result.message });
          return;
        default:
          throw new Error(`unknown file extraction status: ${String((result as { status: string }).status)}`);
      }
    },
    [waitMs],
  );

  /** Forgets a file, for one the depositor removed. */
  const forget = useCallback((id: string) => setState(id, null), []);

  return { states, read, forget };
}

/**
 * Asks once, and once more after waitMs when the server is busy. A thrown
 * error (the action could not be reached) is a failure with its message.
 */
export async function askWithRetry(
  ask: () => Promise<FileExtractionResult>,
  waitMs: number,
): Promise<FileExtractionResult> {
  const attempt = () =>
    ask().catch(
      (error: unknown): FileExtractionResult => ({
        status: "failed",
        message: error instanceof Error ? error.message : "the server could not be reached",
      }),
    );
  const first = await attempt();
  if (first.status !== "busy") {
    return first;
  }
  await new Promise((resolve) => setTimeout(resolve, waitMs));
  return attempt();
}

/** The upload a file read on the server needs: an mmCIF that has finished
 *  uploading and has a checksum. Null for any other file. */
export function uploadedStructure(file: {
  id: string;
  name: string;
  type: string;
  url: string;
  sha256?: string;
  uploadStatus: string;
}): UploadedStructure | null {
  if (file.type !== "mmcif" || file.uploadStatus !== "uploaded" || !file.url || !file.sha256) {
    return null;
  }
  return { id: file.id, name: file.name, url: file.url, sha256: file.sha256 };
}
