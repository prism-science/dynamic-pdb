"use client";

import { useId, useRef } from "react";

import { extFileKey, type ExtExperiment } from "@/lib/api/ext";
import { uploadFileToObjectStorage } from "@/lib/api/uploads";

import ChipListInput from "./ChipListInput";
import CompoundListsEditor from "./CompoundListsEditor";
import FilesEditor, { ProgressBar } from "./FilesEditor";
import { Field, FormSection } from "./FormFields";
import MetricsEditor from "./MetricsEditor";
import PipelineEditor from "./PipelineEditor";
import PipelinePreview from "./PipelinePreview";
import { detectFromFile, resolveExpectedInputs } from "./autoDetect";
import {
  MODEL_COMPOUND_FIELDS,
  type FieldId,
  type SectionId,
} from "./fields";
import { UploadIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import { MODEL_PURPOSES, MODEL_TYPES, emptyCompounds } from "./types";
import type { ModelDraft, ParsedFile } from "./types";
import {
  ONE_MODEL_FILE_ERROR,
  assignCanonicalArtifactTypes,
  canAddModelFiles,
  isModelFile,
  extFileKeys,
  extFileToParsed,
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

  // Field ids come from useId, not from draft.id: the draft id is a
  // crypto.randomUUID() minted where the draft is created, and the add-model
  // form seeds one in its initial state — so it is generated once on the server
  // and again on the client, and the two never match. React does not patch
  // mismatched attributes, which left the rendered `for` pointing at an id no
  // input carried: clicking a label focused nothing. useId is stable across the
  // two renders and unique per instance, which is what these ids need to be.
  // draft.id stays the model's identity for uploads and keys.
  const fieldId = useId();

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
        <FormSection id="overview">
          {/* Same identity block as the entry form. The thumbnail is last in
              the DOM and moved by CSS so tabbing still starts at the title. */}
          <div className={styles.identityRow}>
            <div className={styles.identityMain}>
              <Field
                id="model.title"
                htmlFor={`${fieldId}-title`}
                messages={inline("model.title")}
              >
                <input
                  id={`${fieldId}-title`}
                  className={styles.input}
                  value={draft.title}
                  onChange={(event) =>
                    edit("overview", (current) => ({
                      ...current,
                      title: event.target.value,
                    }))
                  }
                  placeholder="e.g. Refined structure (REFMAC)"
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-form-type="other"
                />
              </Field>

              {/* What the run was for and what came out are properties of the
                  model, like its title — not a separate stage of the form. */}
              <div className={styles.identityChoices}>
                <Field
                  id="model.purpose"
                  htmlFor={`${fieldId}-purpose`}
                  messages={inline("model.purpose")}
                >
                  <select
                    id={`${fieldId}-purpose`}
                    className={styles.select}
                    value={draft.purpose}
                    onChange={(event) =>
                      edit("overview", (current) => ({
                        ...current,
                        purpose: event.target.value,
                      }))
                    }
                  >
                    <option value="">—</option>
                    {MODEL_PURPOSES.map((value) => (
                      <option key={value} value={value}>
                        {value}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field
                  id="model.type"
                  htmlFor={`${fieldId}-type`}
                  messages={inline("model.type")}
                >
                  <select
                    id={`${fieldId}-type`}
                    className={styles.select}
                    value={draft.modelType}
                    onChange={(event) =>
                      edit("overview", (current) => ({
                        ...current,
                        modelType: event.target.value,
                      }))
                    }
                  >
                    <option value="">—</option>
                    {MODEL_TYPES.map((value) => (
                      <option key={value} value={value}>
                        {value}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
            </div>

            <div className={styles.identityThumb}>
              <Field id="model.thumbnail" messages={inline("model.thumbnail")}>
                <label className={styles.thumbDrop}>
                  {draft.thumbPreview ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      className={styles.thumbImg}
                      src={draft.thumbPreview}
                      alt=""
                    />
                  ) : (
                    <span className={styles.thumbHint}>
                      <UploadIcon />
                      Choose an image
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
                  />
                </label>
                {draft.thumbUploadStatus === "failed" ? (
                  <span className={styles.uploadStatus} data-state="failed">
                    {uploadStatusText(
                      draft.thumbUploadStatus,
                      draft.thumbUploadError,
                    )}
                  </span>
                ) : null}
              </Field>
            </div>
          </div>

          <Field
            id="model.details"
            htmlFor={`${fieldId}-details`}
            messages={inline("model.details")}
          >
            <textarea
              id={`${fieldId}-details`}
              className={styles.textarea}
              rows={3}
              value={draft.details}
              onChange={(event) =>
                edit("overview", (current) => ({
                  ...current,
                  details: event.target.value,
                }))
              }
              placeholder="e.g. Multiconformer model built with qFit against the deposited data, then refined in phenix."
            />
          </Field>

          <div className={styles.fieldGrid}>
            <Field
              id="model.authors"
              htmlFor={`${fieldId}-authors`}
              messages={inline("model.authors")}
            >
              <ChipListInput
                id={`${fieldId}-authors`}
                values={draft.authors}
                onChange={(authors) =>
                  edit("overview", (current) => ({ ...current, authors }))
                }
                placeholder="e.g. Hendrickson, W.A. — press Enter"
                separators={/[;\n]/}
              />
            </Field>
            <Field
              id="model.affiliation"
              htmlFor={`${fieldId}-affiliation`}
              messages={inline("model.affiliation")}
            >
              <input
                id={`${fieldId}-affiliation`}
                className={styles.input}
                value={draft.affiliation}
                onChange={(event) =>
                  edit("overview", (current) => ({
                    ...current,
                    affiliation: event.target.value,
                  }))
                }
                placeholder="e.g. University of California, San Francisco"
                autoComplete="off"
              />
            </Field>
          </div>

          <CompoundListsEditor
            compounds={draft.compounds}
            onChange={(compounds) =>
              edit("overview", (current) => ({ ...current, compounds }))
            }
            fields={MODEL_COMPOUND_FIELDS}
            messages={messages}
            source={draft.compoundsSource ? `from ${draft.compoundsSource}` : null}
          />
        </FormSection>

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
