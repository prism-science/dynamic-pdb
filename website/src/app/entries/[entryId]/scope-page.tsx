import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type Entity,
  type EntryPageData,
  getEntryPageData,
  listSimilarEntries,
  type SimilarEntry,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  dataTableEntities,
  getEntityFileURL,
  getFilePayload,
  modelScopedEntities,
} from "@/lib/entities";
import { entryIdentity, formatEntryLabel } from "@/lib/entry-label";
import { crystallographyView } from "@/lib/crystallography";
import { downloadGroups } from "@/lib/download-files";
import { defaultModel, modelTitle } from "@/lib/model-metrics";
import { polymerEntityViews } from "@/lib/polymer-entities";
import { sequenceChains } from "@/lib/sequence-tracks";
import { SIMILAR_ENTRIES_FETCH_LIMIT } from "@/lib/similarity";
import { readModelStructure } from "@/lib/api/coordinates";
import { scopeRailModels } from "@/lib/scope-rail";
import { detectStructureKind, type StructureKind } from "@/lib/structureKind";
import Crystallography from "@/app/components/Crystallography";
import DataTable from "@/app/components/DataTable";
import DownloadFiles from "@/app/components/DownloadFiles";
import EntryOverview, {
  type SummaryModel,
} from "@/app/components/EntryOverview";
import EntryTabs, {
  type EntryTabDescriptor,
  OVERVIEW_TAB,
  resolveTab,
} from "@/app/components/EntryTabs";
import ScopeRail from "@/app/components/ScopeRail";
import SequencePanel from "@/app/components/SequencePanel";
import StructurePanel from "@/app/components/StructurePanel";
import SimilarProteins from "@/app/components/SimilarProteins";
import {
  buildProvenance,
  ImagePlaceholderIcon,
  mergedModelMetrics,
  metricColumns,
  modelMetadata,
  modelPreviewURL,
  summaryModelFacts,
} from "./entry-view";

import styles from "./entry-page.module.css";

/**
 * The entry, seen through one of its models.
 *
 * There is no model-less view of an entry. Coordinates, metrics and files all
 * belong to some model, so a page with none selected could only show the half
 * of the record that never changes. Both routes render this: `/entries/{id}`
 * picks the model to start on, `/entries/{id}/models/{modelId}` names it, and
 * from then on the rail moves between them while the tab stays put.
 */
export default async function ScopePage({
  entryId,
  modelId,
  requestedTab,
}: {
  entryId: string;
  /** null on the entry route, which then starts on the deposited model. */
  modelId: string | null;
  requestedTab: string | undefined;
}) {
  const session = await getAuthSession();
  const [data, similarEntries] = await Promise.all([
    loadEntry(session?.token, entryId),
    loadSimilarEntries(session?.token, entryId),
  ]);

  const model =
    modelId !== null
      ? (data.models.find((item) => item.id === modelId) ?? null)
      : defaultModel(data.models);
  if (modelId !== null && model === null) {
    notFound();
  }

  const entryLabel = formatEntryLabel(data.entry);
  const identity = entryIdentity(data.entry);
  // The page is the entry seen through one model, so the model is what names
  // it; the entry's own identity goes underneath, where a reader looks for a
  // code to quote rather than for the name of the thing on screen. An entry
  // with no models at all has nothing else to be headed by.
  const heading =
    model !== null ? modelTitle(model) : (identity.name ?? identity.id);
  const canAddModel = session !== null;
  const provenance = buildProvenance(data.entities, data.relations);

  const modelEntity =
    model !== null
      ? (data.entities.find(
          (entity) => entity.type === "model" && entity.model_id === model.id,
        ) ?? null)
      : null;
  const metadata = modelMetadata(modelEntity, model?.metadata);
  const previewURL = modelPreviewURL(
    modelEntity,
    model?.thumbnail_image_url,
    data.entry.thumbnail_image_url,
  );

  const entities = polymerEntityViews(
    data.entry.polymer_entities ?? [],
    data.entry.protein_sequences ?? [],
  );
  const crystallography = crystallographyView(data.entry.crystallography);
  const artifacts = new Map(data.entities.map((entity) => [entity.id, entity]));
  // Scoped to the selected model, like the download menu: the artifacts this
  // model was made from and produced, plus the entry's own. The whole graph is
  // still what provenance and the metrics are read from -- a model's input is
  // often another model's output -- but the Data tab is a list of files, and a
  // reader on one model has no use for another model's.
  const modelEntities = modelScopedEntities(data.entities, model?.id ?? null);
  const dataEntities = dataTableEntities(modelEntities);
  const structure = viewableStructure(modelEntity);

  // The chains of the selected model, as its coordinates hold them. Only read
  // when the Sequence tab is open: it is a network fetch and a parse, and no
  // other tab uses it.
  const modelChains =
    requestedTab === "sequence"
      ? await readModelStructure(structure?.url ?? null)
      : [];

  // Everything the Summary tab needs about the selected model, flattened here
  // so the component stays a renderer and the page keeps the joining.
  const summaryModel: SummaryModel | null =
    model === null
      ? null
      : {
          title: modelTitle(model),
          previewURL,
          facts: summaryModelFacts(
            metadata,
            modelEntity ? provenance.programOf(modelEntity.id) : null,
          ),
          metrics: modelEntity
            ? metricColumns(mergedModelMetrics(modelEntity, provenance))
            : [],
        };

  // Both routes render the same page, so the tab links have to hang off the
  // URL the reader is actually on -- otherwise switching a tab would silently
  // move them onto a different model.
  const base =
    modelId !== null
      ? `/entries/${encodeURIComponent(data.entry.id)}/models/${encodeURIComponent(modelId)}`
      : `/entries/${encodeURIComponent(data.entry.id)}`;

  const tabs: EntryTabDescriptor[] = [{ id: OVERVIEW_TAB, label: "Summary" }];
  if (structure) {
    tabs.push({ id: "structure", label: "Structure" });
  }
  if (crystallography) {
    tabs.push({ id: "experiment", label: "Experiment" });
  }
  if (dataEntities.length > 0) {
    tabs.push({ id: "data", label: "Data" });
  }
  // One tab for everything about the sequence: the residues themselves, and
  // the entries that share them. Present as soon as either exists.
  const hasSequences = entities.some((entity) => entity.sequence);
  if (hasSequences || similarEntries.length > 0) {
    tabs.push({ id: "sequence", label: "Sequence" });
  }
  const active = resolveTab(tabs, requestedTab);

  const hrefFor = (id: string) =>
    tabs.some((item) => item.id === id) ? `${base}?tab=${id}` : null;

  return (
    <main
      className={`${styles.page} ${styles.scopeWide}`}
      aria-label={`${entryLabel} entry`}
    >
      <div className={styles.scope}>
        {/* The name spans both columns; the tabs belong to the right one,
            because that is the only column they change. Both stay put while
            the record scrolls under them. */}
        <div className={styles.scopeHead}>
          <div className={styles.identityBar}>
            <div
              className={styles.identityThumb}
              data-empty={data.entry.thumbnail_image_url ? undefined : "true"}
            >
              {data.entry.thumbnail_image_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={data.entry.thumbnail_image_url} alt="" />
              ) : (
                <ImagePlaceholderIcon size={26} />
              )}
            </div>
            <div className={styles.identityNames}>
              {/* Kept to one line, with the whole of it in the tooltip: the
                  head is sticky and a fixed height, and the rail truncates
                  the same titles the same way. */}
              <h1 className={styles.identityName} title={heading}>
                {heading}
              </h1>
              <p className={styles.identityRef}>
                {model !== null && identity.name !== null ? (
                  <>
                    <span className={styles.identityRefName}>
                      {identity.name}
                    </span>
                    <span className={styles.identityRefSep}>|</span>
                  </>
                ) : null}
                {identity.id}
              </p>
            </div>
            <DownloadFiles
              groups={downloadGroups(data.entities, model?.id ?? null)}
            />
          </div>
        </div>

        <div className={styles.scopeGrid}>
          <ScopeRail
            entryId={data.entry.id}
            models={scopeRailModels(data)}
            activeModelId={model?.id ?? null}
            tab={active === OVERVIEW_TAB ? null : active}
            addModelHref={
              canAddModel
                ? `/entries/${encodeURIComponent(data.entry.id)}/models/new`
                : null
            }
          />

          <div className={styles.scopeBody}>
            <div className={styles.scopeTabs}>
              <EntryTabs base={base} tabs={tabs} active={active} />
            </div>

            {active === OVERVIEW_TAB ? (
              <EntryOverview
                entry={data.entry}
                entities={entities}
                artifacts={artifacts}
                model={summaryModel}
              />
            ) : null}

          {active === "structure" && structure ? (
            <section aria-label="Structure">
              <StructurePanel url={structure.url} kind={structure.kind} square />
            </section>
          ) : null}

          {active === "experiment" && crystallography ? (
            <section aria-label="Crystallography">
              <Crystallography view={crystallography} />
            </section>
          ) : null}

          {active === "data" ? (
            <section aria-label="Data">
              <DataTable entities={modelEntities} />
            </section>
          ) : null}

          {active === "sequence" ? (
            <section aria-label="Sequence">
              <SequencePanel chains={sequenceChains(entities, modelChains)} />
              {/* The table is the width of the tab and spaces itself off the
                  viewer, so it needs no wrapper to place it. */}
              <SimilarProteins
                entryId={data.entry.id}
                sequences={data.entry.protein_sequences ?? []}
                items={similarEntries}
              />
            </section>
          ) : null}
          </div>
        </div>
      </div>
    </main>
  );
}

/**
 * The coordinates the Structure tab would draw.
 *
 * Null when this model has no coordinate file the viewer recognises -- and then
 * the tab is not offered at all, rather than opening on an empty canvas.
 */
function viewableStructure(
  modelEntity: Entity | null,
): { url: string; kind: StructureKind } | null {
  if (!modelEntity) {
    return null;
  }
  const url = getEntityFileURL(modelEntity);
  if (!url) {
    return null;
  }
  const kind = detectStructureKind(getFilePayload(modelEntity)?.type ?? url);
  return kind ? { url, kind } : null;
}

async function loadEntry(
  token: string | undefined,
  entryId: string,
): Promise<EntryPageData> {
  try {
    return await getEntryPageData(token, entryId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}

// Similarity is a bonus block on the page: if listing it fails the record still
// renders, just without the tab.
async function loadSimilarEntries(
  token: string | undefined,
  entryId: string,
): Promise<SimilarEntry[]> {
  try {
    return await listSimilarEntries(token, entryId, {
      limit: SIMILAR_ENTRIES_FETCH_LIMIT,
    });
  } catch (error) {
    console.error("list similar entries failed", error);
    return [];
  }
}
