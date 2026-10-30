"use client";

import { useRef } from "react";

import { extFileKey, type ExtExperiment } from "@/lib/api/ext";
import { uploadFileToObjectStorage } from "@/lib/api/uploads";

import FilesEditor from "./FilesEditor";
import { Field, FormSection } from "./FormFields";
import MetricsEditor from "./MetricsEditor";
import ModelOverviewFields from "./ModelOverviewFields";
import PipelineEditor from "./PipelineEditor";
import PipelinePreview from "./PipelinePreview";
import { detectFromFile, resolveExpectedInputs } from "./autoDetect";
import { type FieldId, type SectionId } from "./fields";
import { inlineMessages, type FieldMessage } from "./messages";
import { emptyCompounds } from "./types";
import type { ModelDraft, ParsedFile } from "./types";
import {
  ONE_MODEL_FILE_ERROR,
  assignCanonicalArtifactTypes,
  canAddModelFiles,
  isModelFile,
  modelFileIsStored,
  extFileKeys,
  extFileToParsed,
  parseModelFile,
  parseModelUrlFile,
  setFileLevel,
  uploadErrorMessage,
  uploadParsedFileNow,
} from "./helpers";
import styles from "./form.module.css";

export type ModelDraftUpdater = (
  update: (draft: ModelDraft) => ModelDraft,
) => void;

/**
 * The editable body of a single model, grouped like the model page:
 * Overview (title, purpose, type, description, attribution, composition),
 * Data (files), Evaluations (metrics) and Experiment (the software that ran).
 *
 * State stays with the parent, but every write goes through `onUpdate` as a
 * functional update, so concurrent upload callbacks patch the latest draft
 * instead of a snapshot captured at render time. Edits the depositor makes
 * also call `onEdit` with their section, so the parent can drop API errors
 * the edit may have fixed.
 */
export default function ModelDraftFields({
  draft,
  entryId,
  extExperiment = null,
  baselineFiles = [],
  messages,
  onUpdate,
  onEdit,
  onError,
}: {
  draft: ModelDraft;
  entryId: string;
  extExperiment?: ExtExperiment | null;
  /** Entry-level files this model's runs may consume. */
  baselineFiles?: ParsedFile[];
  messages: readonly FieldMessage[];
  onUpdate: ModelDraftUpdater;
  onEdit: (section: SectionId) => void;
  onError: (message: string | null) => void;
}) {
  // Tracks the draft that is currently on screen, for the guards below.
  const draftRef = useRef(draft);
  draftRef.current = draft;

  const hasModel = draft.files.some(isModelFile);
  const inline = (field: FieldId, item: string | null = null) =>
    inlineMessages(messages, field, item);

  const edit = (
    section: SectionId,
    update: (current: ModelDraft) => ModelDraft,
  ) => {
    onUpdate(update);
    onEdit(section);
  };

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
    edit("data", (current) => ({
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
        { entryId, modelId: draft.id, entityId: fileId },
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
    <div className={styles.modelLayout}>
      <div className={styles.modelMain}>
        <ModelOverviewFields
          values={draft}
          onPatch={(patch) =>
            edit("overview", (current) => ({ ...current, ...patch }))
          }
          thumbnail={{
            preview: draft.thumbPreview,
            status: draft.thumbUploadStatus,
            progress: draft.thumbProgress,
            error: draft.thumbUploadError,
          }}
          onThumbnail={(file) => {
            const nextThumbFileId = crypto.randomUUID();
            edit("overview", (current) => ({
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
          compoundsSource={
            draft.compoundsSource ? `from ${draft.compoundsSource}` : null
          }
          compoundsFromFile={modelFileIsStored(draft.files)}
          messages={messages}
        />

        <FormSection id="data">
          <Field id="model.files" messages={inline("model.files")}>
            <FilesEditor
              files={draft.files}
              lockModelLevel
              fileMessages={(fileId) => inline("model.files", fileId)}
              title={
                hasModel ? undefined : (
                  <>
                    Start with the model file —{" "}
                    <span className={styles.dropZoneBrowse}>browse</span>
                  </>
                )
              }
              hint={
                hasModel
                  ? "Reflections, maps and run logs — .mtz, .ccp4, .log, .eff"
                  : "A .pdb or .cif. Its header supplies the refinement program, authors, R-factors and composition."
              }
              extExperiment={extExperiment}
              onAdd={async (list) => {
                const parsed = assignCanonicalArtifactTypes(
                  draftRef.current.files,
                  await Promise.all(
                    list.map((file) => parseModelFile(file, false)),
                  ),
                  "model",
                );
                if (!appendFiles(parsed)) {
                  return;
                }
                parsed.forEach((file) => {
                  void uploadParsedFileNow(
                    file,
                    { entryId, modelId: draft.id },
                    patchFile,
                  );
                });
                // Any new file may be the one a log was waiting for, whether or
                // not it has a header of its own — a plain .mtz never parses
                // but is exactly what closes a placeholder.
                onUpdate((current) => ({
                  ...current,
                  programs: resolveExpectedInputs(current),
                }));

                // Read each dropped file for what it already declares. Done
                // after the files land in the draft so a log can match names
                // against them.
                for (let index = 0; index < parsed.length; index += 1) {
                  const source = list[index];
                  if (!source) {
                    continue;
                  }
                  void detectFromFile(source, parsed[index], draftRef.current)
                    .then((patch) => {
                      const { notes, ...rest } = patch;
                      if (Object.keys(rest).length === 0) {
                        return;
                      }
                      onUpdate((current) => {
                        const merged = { ...current, ...rest } as ModelDraft;
                        return {
                          ...merged,
                          programs: resolveExpectedInputs(merged),
                        };
                      });
                      if (notes.length > 0) {
                        onError(null);
                      }
                    })
                    .catch(() => {
                      // A file we cannot read is simply a file with no header.
                    });
                }
              }}
              onAddUrl={async (rawUrl) => {
                const parsed = await parseModelUrlFile(rawUrl, false);
                if (parsed) {
                  appendFiles(
                    assignCanonicalArtifactTypes(
                      draftRef.current.files,
                      [parsed],
                      "model",
                    ),
                  );
                }
              }}
              onAddExt={(selected) => {
                if (!extExperiment) {
                  return;
                }
                const existing = extFileKeys(draft.files);
                const parsed = assignCanonicalArtifactTypes(
                  draft.files,
                  selected
                    .filter((file) => !existing.has(extFileKey(file)))
                    .map((file) =>
                      extFileToParsed(file, extExperiment.id, "L2"),
                    ),
                  "model",
                );
                appendFiles(parsed);
              }}
              onRemove={(id) =>
                edit("data", (current) => ({
                  ...current,
                  files: current.files.filter((file) => file.id !== id),
                }))
              }
              onLevel={(id, level) =>
                edit("data", (current) => ({
                  ...current,
                  files: current.files.map((file) =>
                    file.id === id ? setFileLevel(file, level) : file,
                  ),
                }))
              }
              onPatch={(id, patch) => {
                patchFile(id, patch);
                onEdit("data");
              }}
              artifactTypeOptions={["structure_factors", "other"]}
            />
          </Field>
        </FormSection>

        <FormSection id="evaluations">
          <Field id="model.metrics" messages={inline("model.metrics")}>
            <MetricsEditor
              metrics={draft.metrics}
              messages={messages}
              setMetrics={(next) =>
                edit("evaluations", (current) => ({ ...current, metrics: next }))
              }
            />
          </Field>
        </FormSection>

        <FormSection id="experiment">
          <span className={styles.subLabel}>Software</span>
          <PipelineEditor
            programs={draft.programs}
            files={draft.files}
            baseline={baselineFiles}
            messages={messages}
            onChange={(next) =>
              edit("experiment", (current) => ({ ...current, programs: next }))
            }
          />
        </FormSection>
      </div>

      {/* The graph rides alongside the fields rather than under them: it is a
          mirror of what is being typed, and a mirror below the fold is one
          nobody looks in. */}
      <aside className={styles.modelSide}>
        <span className={styles.subLabel}>Pipeline preview</span>
        <PipelinePreview draft={draft} baseline={baselineFiles} />
      </aside>
    </div>
  );
}

export function emptyModelDraft(): ModelDraft {
  return {
    id: crypto.randomUUID(),
    title: "",
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [],
    metrics: [],
    purpose: "",
    modelType: "",
    details: "",
    authors: [],
    affiliation: "",
    compounds: emptyCompounds(),
    compoundsSource: null,
    programs: [],
  };
}
