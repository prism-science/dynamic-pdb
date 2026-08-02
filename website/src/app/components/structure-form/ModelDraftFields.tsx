"use client";

import { useRef } from "react";

import { extFileKey, type ExtExperiment } from "@/lib/api/ext";
import { uploadFileToObjectStorage } from "@/lib/api/uploads";

import FilesEditor, { ProgressBar } from "./FilesEditor";
import MetricsEditor from "./MetricsEditor";
import ProgramEditor from "./ProgramEditor";
import { UploadIcon } from "./icons";
import type { ModelDraft, ParsedFile } from "./types";
import {
  ONE_MODEL_FILE_ERROR,
  canAddModelFiles,
  extFileKeys,
  extFileToParsed,
  normalizeModelLevel,
  parseModelFile,
  parseModelUrlFile,
  setFileLevel,
  uploadErrorMessage,
  uploadParsedFileNow,
  uploadStatusText,
} from "./helpers";
import styles from "./form.module.css";

export type ModelDraftUpdater = (
  update: (draft: ModelDraft) => ModelDraft,
) => void;

/**
 * The editable body of a single model: name, description, preview image, data
 * files, metrics, and program. Shared by the new-structure form (one block per
 * model) and the add-model form (a single block).
 *
 * State stays with the parent, but every write goes through `onUpdate` as a
 * functional update, so concurrent upload callbacks patch the latest draft
 * instead of a snapshot captured at render time.
 */
export default function ModelDraftFields({
  draft,
  structureId,
  extExperiment = null,
  onUpdate,
  onError,
}: {
  draft: ModelDraft;
  structureId: string;
  extExperiment?: ExtExperiment | null;
  onUpdate: ModelDraftUpdater;
  onError: (message: string | null) => void;
}) {
  // Tracks the draft that is currently on screen, for the guards below.
  const draftRef = useRef(draft);
  draftRef.current = draft;

  const patchFile = (fileId: string, patch: Partial<ParsedFile>) =>
    onUpdate((current) => ({
      ...current,
      files: current.files.map((file) =>
        file.id === fileId ? { ...file, ...patch } : file,
      ),
    }));

  // The one-model-file rule is checked against the ref rather than inside the
  // state updater: React may defer the updater, so a value it assigns is not
  // readable right after the call.
  const appendFiles = (incoming: ParsedFile[]) => {
    if (incoming.length === 0) {
      return false;
    }
    if (!canAddModelFiles(draftRef.current.files, incoming)) {
      onError(ONE_MODEL_FILE_ERROR);
      return false;
    }
    onError(null);
    // Advance the ref before React commits, so two adds resolving in the same
    // tick (two dropped files, one still parsing) see each other.
    draftRef.current = {
      ...draftRef.current,
      files: [...draftRef.current.files, ...incoming],
    };
    onUpdate((current) => ({
      ...current,
      files: [...current.files, ...incoming],
    }));
    return true;
  };

  const uploadThumbnail = async (file: File, fileId: string) => {
    const patchThumb = (patch: Partial<ModelDraft>) =>
      onUpdate((current) =>
        current.thumbFileId === fileId ? { ...current, ...patch } : current,
      );
    try {
      const url = await uploadFileToObjectStorage(
        file,
        { structureId, modelId: draft.id, entityId: fileId },
        (fraction) => patchThumb({ thumbProgress: fraction }),
      );
      patchThumb({
        thumbUrl: url,
        thumbProgress: 1,
        thumbUploadStatus: "uploaded",
        thumbUploadError: null,
      });
    } catch (error) {
      patchThumb({
        thumbUploadStatus: "failed",
        thumbUploadError: uploadErrorMessage(error),
      });
    }
  };

  return (
    <>
      <input
        className={styles.input}
        value={draft.name}
        onChange={(event) =>
          onUpdate((current) => ({ ...current, name: event.target.value }))
        }
        placeholder="e.g. Refined structure (REFMAC)"
        autoComplete="off"
        data-1p-ignore
        data-lpignore="true"
        data-form-type="other"
      />
      <input
        className={styles.input}
        value={draft.description}
        onChange={(event) =>
          onUpdate((current) => ({
            ...current,
            description: event.target.value,
          }))
        }
        placeholder="Optional — e.g. molecular replacement, then restrained refinement"
        autoComplete="off"
      />

      <label className={styles.thumbDrop}>
        {draft.thumbPreview ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img className={styles.thumbImg} src={draft.thumbPreview} alt="" />
        ) : (
          <span className={styles.thumbHint}>
            <UploadIcon />
            Preview image
          </span>
        )}
        {draft.thumbUploadStatus === "uploading" ? (
          <span className={styles.thumbOverlay}>
            <ProgressBar value={draft.thumbProgress} />
          </span>
        ) : null}
        <input
          type="file"
          accept="image/*"
          className={styles.hiddenInput}
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (!file) {
              return;
            }
            const nextThumbFileId = crypto.randomUUID();
            onUpdate((current) => ({
              ...current,
              thumbFileId: nextThumbFileId,
              thumbFile: file,
              thumbPreview: URL.createObjectURL(file),
              thumbUrl: null,
              thumbProgress: 0,
              thumbUploadStatus: "uploading",
              thumbUploadError: null,
            }));
            void uploadThumbnail(file, nextThumbFileId);
          }}
        />
      </label>
      {draft.thumbUploadStatus === "failed" ? (
        <span className={styles.uploadStatus} data-state="failed">
          {uploadStatusText(draft.thumbUploadStatus, draft.thumbUploadError)}
        </span>
      ) : null}

      <span className={styles.subLabel}>Data</span>
      <FilesEditor
        files={draft.files}
        lockModelLevel
        extExperiment={extExperiment}
        onAdd={async (list) => {
          const parsed = await Promise.all(
            list.map((file) => parseModelFile(file)),
          );
          if (!appendFiles(parsed)) {
            return;
          }
          parsed.forEach((file) => {
            void uploadParsedFileNow(
              file,
              { structureId, modelId: draft.id },
              patchFile,
            );
          });
        }}
        onAddUrl={async (rawUrl) => {
          const parsed = await parseModelUrlFile(rawUrl);
          if (parsed) {
            appendFiles([parsed]);
          }
        }}
        onAddExt={(selected) => {
          if (!extExperiment) {
            return;
          }
          const existing = extFileKeys(draft.files);
          const parsed = selected
            .filter((file) => !existing.has(extFileKey(file)))
            .map((file) =>
              normalizeModelLevel(
                extFileToParsed(file, extExperiment.id, "L2"),
              ),
            );
          appendFiles(parsed);
        }}
        onRemove={(id) =>
          onUpdate((current) => ({
            ...current,
            files: current.files.filter((file) => file.id !== id),
          }))
        }
        onLevel={(id, level) =>
          onUpdate((current) => ({
            ...current,
            files: current.files.map((file) =>
              file.id === id ? setFileLevel(file, level) : file,
            ),
          }))
        }
        onPatch={patchFile}
      />

      <span className={styles.subLabel}>Metrics</span>
      <MetricsEditor
        metrics={draft.metrics}
        setMetrics={(next) =>
          onUpdate((current) => ({ ...current, metrics: next }))
        }
      />

      <span className={styles.subLabel}>Program</span>
      <ProgramEditor
        program={draft.program}
        setProgram={(next) =>
          onUpdate((current) => ({ ...current, program: next }))
        }
      />
    </>
  );
}

export function emptyModelDraft(): ModelDraft {
  return {
    id: crypto.randomUUID(),
    name: "",
    description: "",
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [],
    metrics: [],
    program: null,
  };
}
