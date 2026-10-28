"use client";

import { useState, type FormEvent } from "react";

import type { Entity, MetricKey, ModelDetail } from "@/lib/api/entries";
import { updateModelAction } from "@/app/entries/[entryId]/models/[modelId]/edit/actions";

import ChangeSummaryField from "./entry-form/ChangeSummaryField";
import { Field, FormSection, SubmitBlockers } from "./entry-form/FormFields";
import MetricsEditor from "./entry-form/MetricsEditor";
import ModelOverviewFields from "./entry-form/ModelOverviewFields";
import { FilesReadOnly } from "./entry-form/ReadOnlyFields";
import { FIELDS, type SectionId } from "./entry-form/fields";
import { blockerLines, type FieldMessage } from "./entry-form/messages";
import {
  archiveLockLabel,
  buildModelRevisionChange,
  hasChanges,
  modelDraftFromModel,
  modelLocks,
  type ArchiveLock,
  type ModelEditDraft,
} from "./entry-form/revisionChanges";
import { METRIC_FIELDS } from "./entry-form/types";
import { useThumbnailUpload } from "./entry-form/useThumbnailUpload";
import { modelEditMessages } from "./entry-form/validation";
import styles from "./entry-form/form.module.css";

/**
 * Edits a published model. Saving sends only what the depositor changed, as
 * a revision a reviewer approves. Files and runs are read-only: re-sending
 * their stored ids fails until T2-G.
 */
export default function EditModelForm({
  entryId,
  model,
  files,
  runs,
}: {
  entryId: string;
  model: ModelDetail;
  files: Entity[];
  runs: Entity[];
}) {
  const [initial] = useState(() => modelDraftFromModel(model));
  const [draft, setDraft] = useState<ModelEditDraft>(initial);
  const thumbnail = useThumbnailUpload(
    { entryId, modelId: model.id },
    initial.thumbUrl,
  );
  const [changeSummary, setChangeSummary] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [apiMessages, setApiMessages] = useState<FieldMessage[]>([]);

  const locks = modelLocks(model);
  const edit = (section: SectionId, update: (current: ModelEditDraft) => ModelEditDraft) => {
    setDraft(update);
    setApiMessages((current) =>
      current.filter((message) => FIELDS[message.field].section !== section),
    );
  };

  const change = buildModelRevisionChange(model, initial, {
    ...draft,
    thumbUrl: thumbnail.url,
  });
  const changed = hasChanges(change);

  const validationMessages = modelEditMessages(draft, locks);
  const messages = [...validationMessages, ...apiMessages];
  const blockers = [
    ...blockerLines(validationMessages),
    ...(thumbnail.status === "uploading"
      ? ["Preview image: wait until it has finished uploading"]
      : []),
    ...(thumbnail.status === "failed"
      ? ["Preview image: choose it again, or keep the current one"]
      : []),
  ];
  const canSubmit = changed && blockers.length === 0 && !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    setApiMessages([]);
    const summary = changeSummary.trim();
    const result = await updateModelAction(entryId, model.id, {
      ...change,
      ...(summary ? { change_summary: summary } : {}),
    });
    // On success the action redirects to the model page.
    if (result) {
      setError(result.error);
      setApiMessages(result.fieldErrors);
      setSubmitting(false);
    }
  };

  const lockedMetrics = Object.fromEntries(
    (Object.entries(locks.metrics) as [MetricKey, ArchiveLock][]).map(([key, lock]) => [
      key,
      archiveLockLabel(lock),
    ]),
  );
  // A stored metric the form has no field for is sent back unchanged; an
  // error the API returns about it is shown under the whole section.
  const formMetricKeys = new Set<string>(METRIC_FIELDS.map((field) => field.key));
  const sectionMetricMessages = messages.filter(
    (message) =>
      message.field === "model.metrics" &&
      !message.missing &&
      (message.item === null || !formMetricKeys.has(message.item)),
  );
  const modelHref = `/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(model.id)}`;

  return (
    <form className={styles.form} onSubmit={submit} noValidate>
      <ModelOverviewFields
        values={draft}
        onPatch={(patch) => edit("overview", (current) => ({ ...current, ...patch }))}
        thumbnail={thumbnail}
        onThumbnail={thumbnail.choose}
        locks={locks}
        messages={messages}
      />

      <FormSection id="data">
        <FilesReadOnly files={files} note="Adding or replacing files comes later." />
      </FormSection>

      <FormSection id="evaluations">
        <Field id="model.metrics" messages={sectionMetricMessages}>
          <MetricsEditor
            metrics={draft.metrics}
            messages={messages}
            setMetrics={(metrics) =>
              edit("evaluations", (current) => ({ ...current, metrics }))
            }
            locked={lockedMetrics}
          />
        </Field>
      </FormSection>

      <FormSection id="experiment">
        <span className={styles.subLabel}>Software</span>
        <FilesReadOnly
          files={runs}
          note="Changing the recorded runs comes later."
          empty="No runs recorded"
        />
      </FormSection>

      <ChangeSummaryField value={changeSummary} onChange={setChangeSummary} />

      {error ? <p className={styles.error}>{error}</p> : null}
      {submitting ? null : <SubmitBlockers action="save" lines={blockers} />}

      <div className={styles.actions}>
        <div className={styles.actionsRight}>
          <a className={styles.secondary} href={modelHref}>
            Cancel
          </a>
          <button type="submit" className={styles.primary} disabled={!canSubmit}>
            {submitting ? "Saving…" : changed ? "Save change" : "Change at least one field"}
          </button>
        </div>
      </div>
    </form>
  );
}
