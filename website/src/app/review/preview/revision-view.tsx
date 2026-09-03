import {
  modelRevisionGraph,
  type Artifact,
  type EntryRevision,
  type ModelRevision,
} from "@/lib/api/entries";
import {
  dataTableEntities,
  type FileItem,
  getEntityFileURL,
  structureMaps,
} from "@/lib/entities";
import { formatEntryLabel } from "@/lib/entry-label";
import DataTable from "@/app/components/DataTable";
import FileTable from "@/app/components/FileTable";
import ResolvedFileLink from "@/app/components/ResolvedFileLink";
import {
  buildProvenance,
  entryMetadataFacts,
  hasModelEvaluations,
  ImagePlaceholderIcon,
  InfoGrid,
  modelInfoFacts,
  modelMetadata,
  modelPreviewURL,
  ModelEvaluations,
  ModelViewerButton,
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
  const entryLabel = formatEntryLabel({
    id: revision.entry_id,
    name: revision.name,
    metadata: revision.metadata,
  });

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <Thumb url={thumbnail} />
        <h1 className={styles.sideName}>{entryLabel}</h1>
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
            <FileTable items={files} />
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

export function ModelRevisionPreview({
  revision,
}: {
  revision: ModelRevision;
}) {
  const { entities, relations } = modelRevisionGraph(revision);
  const provenance = buildProvenance(entities, relations);
  const model = entities.find((entity) => entity.type === "model") ?? null;
  const maps = structureMaps(entities);
  const modelFileURL = model ? getEntityFileURL(model) : null;

  const metadata = modelMetadata(model, revision.metadata);
  const previewURL = modelPreviewURL(model, revision.thumbnail_image_url);
  const vitals = modelVitals(metadata);
  const infoFacts = modelInfoFacts(
    metadata,
    model ? provenance.programOf(model.id) : null,
  );
  const hasEvaluations = model
    ? hasModelEvaluations(model, provenance)
    : false;
  const hasData = dataTableEntities(entities).length > 0;

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <Thumb url={previewURL} />
        <h1 className={styles.sideName}>{revision.name}</h1>

        {vitals.length > 0 ? (
          <dl className={styles.sideVitals}>
            {vitals.map((item) => (
              <div key={item}>{item}</div>
            ))}
          </dl>
        ) : null}

        {model || modelFileURL ? (
          <div className={styles.sideActions}>
            {model ? <ModelViewerButton entity={model} maps={maps} /> : null}
            {modelFileURL ? (
              <ResolvedFileLink
                className={styles.sideDownload}
                href={modelFileURL}
                download
                rel="noreferrer"
                target="_blank"
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.9"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
                </svg>
                Download
              </ResolvedFileLink>
            ) : null}
          </div>
        ) : null}
      </aside>

      <div className={styles.content}>
        {revision.description?.trim() || infoFacts.length > 0 ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Info</h2>
            {revision.description?.trim() ? (
              <p className={styles.lead}>{revision.description}</p>
            ) : null}
            <InfoGrid facts={infoFacts} />
          </section>
        ) : null}

        {model && hasEvaluations ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Evaluations</h2>
            <ModelEvaluations entity={model} provenance={provenance} />
          </section>
        ) : null}

        {hasData ? (
          <section className={styles.contentSection}>
            <h2 className={styles.contentHeading}>Data</h2>
            <DataTable entities={entities} />
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
      level: artifact.level,
      sha256: artifact.sha256,
      url: artifact.uri,
    }));
}
