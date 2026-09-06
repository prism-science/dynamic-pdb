import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type Entity,
  type EntryPageData,
  type FastaMetadata,
  getEntryPageData,
  listSimilarEntries,
  type SimilarEntry,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import { fastaTotalLength } from "@/lib/fasta";
import { type FileItem, fileItemFromEntity } from "@/lib/entities";
import { formatEntryLabel } from "@/lib/entry-label";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import FileTable from "@/app/components/FileTable";
import SimilarProteins from "@/app/components/SimilarProteins";
import SortableModelList from "@/app/components/SortableModelList";
import { SIMILAR_ENTRIES_FETCH_LIMIT } from "@/lib/similarity";
import {
  entryMetadataFacts,
  ImagePlaceholderIcon,
  InfoGrid,
} from "./entry-view";

import styles from "./entry-page.module.css";

export const dynamic = "force-dynamic";

type EntryRouteProps = {
  params: Promise<{ entryId: string }>;
};

export default async function EntryPage({
  params,
}: EntryRouteProps) {
  const { entryId } = await params;
  const session = await getAuthSession();

  const [data, similarEntries] = await Promise.all([
    loadEntryPage(session?.token, entryId),
    loadSimilarEntries(session?.token, entryId),
  ]);
  const sequence = getFastaMetadata(data);

  const vitals: string[] = [];
  if (sequence) {
    const length = fastaTotalLength(sequence);
    if (length > 0) {
      vitals.push(`${length.toLocaleString()} residues`);
    }
  }

  const files = getLevelZeroFiles(data);

  const hasFiles = files.length > 0;
  const hasModels = data.models.length > 0;
  const canAddModel = session !== null;
  const entryFacts = entryMetadataFacts(data.entry.metadata);
  const entryLabel = formatEntryLabel(data.entry);

  return (
    <main className={styles.page} aria-label={`${entryLabel} entry`}>
      <div className={styles.record}>
        <Breadcrumbs
          items={[{ label: "Entries", href: "/browse" }, { label: entryLabel }]}
        />

        <div className={styles.layout}>
          <aside className={styles.sidebar}>
            <div
              className={styles.sideThumb}
              data-empty={data.entry.thumbnail_image_url ? undefined : "true"}
            >
              {data.entry.thumbnail_image_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={data.entry.thumbnail_image_url} alt="" />
              ) : (
                <ImagePlaceholderIcon size={34} />
              )}
            </div>

            <h1 className={styles.sideName}>{entryLabel}</h1>

            {vitals.length > 0 ? (
              <dl className={styles.sideVitals}>
                {vitals.map((item) => (
                  <div key={item}>{item}</div>
                ))}
              </dl>
            ) : null}

            {/* Discovery lives in the rail: entries whose sequences a
                similarity run matched to this one, with the full list and
                alignments behind "All similar". */}
            <SimilarProteins
              entryId={data.entry.id}
              entryLabel={entryLabel}
              sequences={data.entry.protein_sequences ?? []}
              items={similarEntries}
            />
          </aside>

          <div className={styles.content}>
            {entryFacts.length > 0 ? (
              <section id="info" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Info</h2>
                <InfoGrid facts={entryFacts} />
              </section>
            ) : null}

            {/* The sequence is not printed inline any more: the FASTA shows up
                as a regular file below and expands into a preview on click. */}
            {hasFiles ? (
              <section id="files" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Files</h2>
                <FileTable items={files} />
              </section>
            ) : null}

            {/* Always rendered: an entry with no models still needs somewhere
                to add the first one. */}
            <section id="models" className={styles.contentSection}>
              <div className={styles.contentSectionHead}>
                <h2 className={styles.contentHeading}>Models</h2>
                {/* The page behind this link redirects anonymous visitors to
                    the login, so offering it signed-out was a dead end. Same
                    rule as the entries list, which hides its create button
                    unless the session can actually use it. */}
                {canAddModel ? (
                  <a
                    className={styles.addModelLink}
                    href={`/entries/${encodeURIComponent(data.entry.id)}/models/new`}
                  >
                    Add model
                  </a>
                ) : null}
              </div>
              {hasModels ? (
                <SortableModelList
                  entryId={data.entry.id}
                  entryThumbnailImageURL={data.entry.thumbnail_image_url}
                  models={data.models}
                  entities={data.entities}
                  relations={data.relations}
                />
              ) : (
                <p className={styles.modelsEmpty}>
                  {canAddModel
                    ? "No models yet. Add the first refinement or prediction built from this entry's data."
                    : "No models yet."}
                </p>
              )}
            </section>
          </div>
        </div>
      </div>
    </main>
  );
}

async function loadEntryPage(
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

// Similarity is a bonus block on the page: if listing it fails the entry
// still renders, just without the rail. This first page feeds the rail and
// the dialog; the dialog loads further pages itself while scrolling.
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

// The structure owns whatever is not attached to one of its models. Model-owned
// entities are shown on that model's page, the same way this page's files are
// not repeated there.
function structureEntities(data: EntryPageData): Entity[] {
  return data.entities.filter((entity) => entity.model_id === null);
}

function getFastaMetadata(data: EntryPageData): FastaMetadata | null {
  for (const entity of structureEntities(data)) {
    if (entity.type !== "data" && entity.type !== "model") {
      continue;
    }
    const payload = entity.payload as DataPayload;
    if (payload?.type === "fasta" && payload.metadata) {
      return payload.metadata as FastaMetadata;
    }
  }
  return null;
}

// L0 raw files, FASTA included: the sequence belongs to the structure, so it is
// listed here as a file and previewed in place. FASTA is pinned to the top
// because it is what people look for first.
function getLevelZeroFiles(data: EntryPageData): FileItem[] {
  return structureEntities(data)
    .filter((entity) => entity.level === "L0" && entity.type === "data")
    .map(fileItemFromEntity)
    .sort((a, b) => Number(b.type === "fasta") - Number(a.type === "fasta"));
}
