import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type ModelPageData,
  getEntryGraph,
  getModelPageData,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  dataTableEntities,
  getEntityFileURL,
  structureMaps,
} from "@/lib/entities";
import { buildLineage } from "@/lib/lineage";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import DataTable from "@/app/components/DataTable";
import PipelineModal from "@/app/components/PipelineModal";
import ResolvedFileLink from "@/app/components/ResolvedFileLink";
import {
  buildProvenance,
  hasModelEvaluations,
  ImagePlaceholderIcon,
  InfoGrid,
  modelInfoFacts,
  modelMetadata,
  modelPreviewURL,
  ModelEvaluations,
  ModelViewerButton,
  modelVitals,
} from "../../entry-view";

import styles from "../../entry-page.module.css";

export const dynamic = "force-dynamic";

type ModelRouteProps = {
  params: Promise<{ entryId: string; modelId: string }>;
};

// The model mirrors the entry: a narrow identity rail on the left, sections on
// the right. Facts belong in the rail — a label/value list only reads well in a
// column that narrow.
export default async function ModelPage({
  params,
}: ModelRouteProps) {
  const { entryId, modelId } = await params;
  const session = await getAuthSession();

  const data = await loadModelPage(session?.token, entryId, modelId);
  const provenance = buildProvenance(data.entities, data.relations);

  const model = data.entities.find((entity) => entity.type === "model") ?? null;
  const modelFileURL = model ? getEntityFileURL(model) : null;
  const maps = structureMaps(data.entities);

  const metadata = modelMetadata(model, data.model.metadata);
  const previewURL = modelPreviewURL(
    model,
    data.model.thumbnail_image_url,
    data.entry.thumbnail_image_url,
  );
  const vitals = modelVitals(metadata);
  const infoFacts = modelInfoFacts(
    metadata,
    model ? provenance.programOf(model.id) : null,
  );

  const hasData = dataTableEntities(data.entities).length > 0;
  const hasEvaluations = model
    ? hasModelEvaluations(model, provenance)
    : false;

  // The chain crosses model boundaries: this model's input was some other
  // model's output, and /models/{id}/artifacts only carries relations for this
  // model's own runs. Until the backend grows a lineage endpoint, the whole
  // entry graph is what has the edges to walk.
  const entryGraph = await getEntryGraph(session?.token, entryId);
  const lineage = buildLineage(
    entryGraph.entities,
    entryGraph.relations,
    model?.id ?? null,
  );

  return (
    <main
      className={styles.page}
      aria-label={`${data.entry.name} model ${data.model.name}`}
    >
      <div className={styles.record}>
        <Breadcrumbs
          items={[
            { label: "Entries", href: "/browse" },
            { label: data.entry.name, href: `/entries/${data.entry.id}` },
            { label: data.model.name },
          ]}
        />

        <div className={styles.layout}>
          <aside className={styles.sidebar}>
            <div
              className={styles.sideThumb}
              data-empty={previewURL ? undefined : "true"}
            >
              {previewURL ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={previewURL} alt="" />
              ) : (
                <ImagePlaceholderIcon size={34} />
              )}
            </div>

            <h1 className={styles.sideName}>{data.model.name}</h1>

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
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
                    </svg>
                    Download
                  </ResolvedFileLink>
                ) : null}
              </div>
            ) : null}

            <PipelineModal lineage={lineage} title={data.model.name} />
          </aside>

          <div className={styles.content}>
            {!model ? (
              <p className={styles.emptyState}>No model produced yet.</p>
            ) : null}

            {infoFacts.length > 0 ? (
              <section id="info" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Info</h2>
                <InfoGrid facts={infoFacts} />
              </section>
            ) : null}

            {model && hasEvaluations ? (
              <section id="evaluations" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Evaluations</h2>
                <ModelEvaluations entity={model} provenance={provenance} />
              </section>
            ) : null}

            {/* No sequence block here: the sequence is a property of the
                structure, identical across every model, and lives on the entry
                page. */}
            {hasData ? (
              <section id="data" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Data</h2>
                <DataTable entities={data.entities} />
              </section>
            ) : null}
          </div>
        </div>
      </div>
    </main>
  );
}

async function loadModelPage(
  token: string | undefined,
  entryId: string,
  modelId: string,
): Promise<ModelPageData> {
  try {
    return await getModelPageData(token, entryId, modelId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
