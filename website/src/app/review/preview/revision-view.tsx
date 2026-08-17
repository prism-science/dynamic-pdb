import type { Artifact, EntryRevision, ModelRevision } from "@/lib/api/entries";
import FileList, { type FileItem } from "@/app/components/FileList";
import {
  entryMetadataFacts,
  ImagePlaceholderIcon,
  InfoGrid,
  modelInfoFacts,
  modelVitals,
} from "@/app/entries/[entryId]/entry-view";

import styles from "@/app/entries/[entryId]/entry-page.module.css";
import previewStyles from "./preview.module.css";

/** A revision waiting for review, drawn as the page it will become. Built here
 *  from the revision the API already returns — the published pages resolve the
 *  active revision and have no business knowing about this. */
export function RevisionPreviewFrame({
  kind,
  name,
  children,
}: {
  kind: "entry" | "model";
  name: string;
  children: React.ReactNode;
}) {
  return (
    <main className={styles.page} aria-label={`${name} preview`}>
      <div className={styles.record}>
        <div className={previewStyles.banner}>
          <span className={previewStyles.dot} />
          Preview of a {kind} revision waiting for review. Not published yet.
        </div>
        {children}
      </div>
    </main>
  );
}

export function EntryRevisionPreview({
  revision,
  models,
}: {
  revision: EntryRevision;
  models: ModelRevision[];
}) {
  const residues = revision.protein_sequences.reduce(
    (total, sequence) => total + sequence.sequence.length,
    0,
  );
  const facts = entryMetadataFacts(revision.metadata);
  const files = fileItems(revision.artifacts);
  const thumbnail = revision.thumbnail_image_url?.trim() || null;

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <Thumb url={thumbnail} />
        <h1 className={styles.sideName}>{revision.name}</h1>
        {residues > 0 ? (
          <dl className={styles.sideVitals}>
            <div>{residues.toLocaleString()} residues</div>
          </dl>
        ) : null}
      </aside>

      <div className={styles.content}>
        {revision.description?.trim() || facts.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Info</h2>
            {revision.description?.trim() ? (
              <p className={styles.lead}>{revision.description}</p>
            ) : null}
            <InfoGrid facts={facts} />
          </section>
        ) : null}

        {files.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Files</h2>
            <FileList items={files} />
          </section>
        ) : null}

        <section className={styles.contentSection}>
          <h2 className={styles.contentHeading}>Models</h2>
          {models.length > 0 ? (
            <ul className={previewStyles.modelList}>
              {models.map((model) => (
                <li key={model.id} className={previewStyles.modelRow}>
                  <span className={previewStyles.modelName}>{model.name}</span>
                  <span className={previewStyles.modelMetrics}>
                    {model.metrics.length > 0
                      ? model.metrics
                          .map((metric) => `${metric.key} ${metric.value}`)
                          .join(" · ")
                      : "no metrics"}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className={styles.modelsEmpty}>
              No models are part of this revision.
            </p>
          )}
        </section>
      </div>
    </div>
  );
}

export function ModelRevisionPreview({ revision }: { revision: ModelRevision }) {
  const metadata = revision.metadata ?? {};
  const facts = modelInfoFacts(metadata, null);
  const vitals = modelVitals(metadata);
  const files = fileItems(revision.artifacts);
  const thumbnail = revision.thumbnail_image_url?.trim() || null;

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <Thumb url={thumbnail} />
        <h1 className={styles.sideName}>{revision.name}</h1>
        {vitals.length > 0 ? (
          <dl className={styles.sideVitals}>
            {vitals.map((item) => (
              <div key={item}>{item}</div>
            ))}
          </dl>
        ) : null}
      </aside>

      <div className={styles.content}>
        {revision.description?.trim() || facts.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Info</h2>
            {revision.description?.trim() ? (
              <p className={styles.lead}>{revision.description}</p>
            ) : null}
            <InfoGrid facts={facts} />
          </section>
        ) : null}

        {revision.metrics.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Validation</h2>
            <div className={styles.metricGrid}>
              {revision.metrics.map((metric) => (
                <div key={metric.id} className={styles.metricTile}>
                  <div className={styles.metricLabel}>{metric.key}</div>
                  <div className={styles.metricValue}>{metric.value}</div>
                </div>
              ))}
            </div>
          </section>
        ) : null}

        {files.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Files</h2>
            <FileList items={files} />
          </section>
        ) : null}
      </div>
    </div>
  );
}

function Thumb({ url }: { url: string | null }) {
  return (
    <div className={styles.sideThumb} data-empty={url ? undefined : "true"}>
      {url ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={url} alt="" />
      ) : (
        <ImagePlaceholderIcon size={34} />
      )}
    </div>
  );
}

function fileItems(artifacts: Artifact[]): FileItem[] {
  return [...artifacts]
    .sort((left, right) => left.name.localeCompare(right.name))
    .map((artifact) => ({
      id: artifact.id,
      name: artifact.name,
      type: artifact.format ?? undefined,
      size: artifact.size_bytes ?? undefined,
      url: artifact.uri,
    }));
}
