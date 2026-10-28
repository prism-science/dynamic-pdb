"use client";

import { useRef, useState } from "react";

import { uploadFileToObjectStorage } from "@/lib/api/uploads";

import { uploadErrorMessage } from "./helpers";
import type { UploadStatus } from "./types";

/** A preview image the form uploads as soon as it is chosen. */
export type ThumbnailUpload = {
  file: File | null;
  preview: string | null;
  url: string | null;
  progress: number;
  status: UploadStatus;
  error: string | null;
  choose: (file: File) => void;
  /** Shows an image that is already uploaded, such as a saved draft's. */
  restore: (url: string | null, preview: string | null) => void;
  reset: () => void;
};

/**
 * A form's preview image, stored under the entry, or under the model when
 * modelId is set. Only the last chosen file counts: an upload that finishes
 * after another file was chosen is ignored.
 */
export function useThumbnailUpload(
  target: { entryId: string; modelId: string | null },
  initialUrl: string | null = null,
): ThumbnailUpload {
  const activeFileId = useRef<string | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | null>(initialUrl);
  const [url, setUrl] = useState<string | null>(initialUrl);
  const [progress, setProgress] = useState(initialUrl ? 1 : 0);
  const [status, setStatus] = useState<UploadStatus>(
    initialUrl ? "uploaded" : "idle",
  );
  const [error, setError] = useState<string | null>(null);

  const upload = async (chosen: File, fileId: string) => {
    try {
      const uploadedUrl = await uploadFileToObjectStorage(
        chosen,
        { entryId: target.entryId, modelId: target.modelId, entityId: fileId },
        (fraction) => {
          if (activeFileId.current === fileId) {
            setProgress(fraction);
          }
        },
      );
      if (activeFileId.current !== fileId) {
        return;
      }
      setUrl(uploadedUrl);
      setProgress(1);
      setStatus("uploaded");
      setError(null);
    } catch (uploadError) {
      if (activeFileId.current !== fileId) {
        return;
      }
      setStatus("failed");
      setError(uploadErrorMessage(uploadError));
    }
  };

  const choose = (chosen: File) => {
    const fileId = crypto.randomUUID();
    activeFileId.current = fileId;
    setFile(chosen);
    setPreview(URL.createObjectURL(chosen));
    setUrl(null);
    setProgress(0);
    setStatus("uploading");
    setError(null);
    void upload(chosen, fileId);
  };

  const restore = (restoredUrl: string | null, restoredPreview: string | null) => {
    activeFileId.current = null;
    setFile(null);
    setUrl(restoredUrl);
    setPreview(restoredPreview);
    setStatus(restoredUrl ? "uploaded" : "idle");
    setProgress(restoredUrl ? 1 : 0);
    setError(null);
  };

  const reset = () => restore(null, null);

  return { file, preview, url, progress, status, error, choose, restore, reset };
}
