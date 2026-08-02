import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type EntryPageData,
  type FastaMetadata,
  getEntryPageData,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";
import { fastaTotalLength } from "@/lib/fasta";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import FileList, { type FileItem } from "@/app/components/FileList";
import SortableModelList from "@/app/components/SortableModelList";
import { ImagePlaceholderIcon } from "./entry-view";

import styles from "./entry-page.module.css";

export const dynamic = "force-dynamic";

type EntryRouteProps = {
  params: Promise<{ entryId: string }>;
};

export default async function EntryPage({ params }: EntryRouteProps) {
  const { entryId } = await params;
  const session = await getAuthSession();

  const data = await loadEntryPage(session?.token, entryId);
  const currentUserId = session ? userIdFromToken(session.token) : null;
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

  return (
    <main className={styles.page} aria-label={`${data.entry.name} entry`}>
      <div className={styles.record}>
        <Breadcrumbs
          items={[{ label: "Entries", href: "/" }, { label: data.entry.name }]}
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

            <h1 className={styles.sideName}>{data.entry.name}</h1>

            {vitals.length > 0 ? (
              <dl className={styles.sideVitals}>
                {vitals.map((item) => (
                  <div key={item}>{item}</div>
                ))}
              </dl>
            ) : null}
          </aside>

          <div className={styles.content}>
            {data.entry.description?.trim() ? (
              <p className={styles.lead}>{data.entry.description}</p>
            ) : null}

            {/* The sequence is not printed inline any more: the FASTA shows up
                as a regular file below and expands into a preview on click. */}
            {hasFiles ? (
              <section id="files" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Files</h2>
                <FileList items={files} />
              </section>
            ) : null}

            {/* Always rendered: an entry with no models still needs somewhere
                to add the first one. */}
            <section id="models" className={styles.contentSection}>
              <div className={styles.contentSectionHead}>
                <h2 className={styles.contentHeading}>Models</h2>
                <a
                  className={styles.addModelLink}
                  href={`/entries/${encodeURIComponent(data.entry.id)}/models/new`}
                >
                  Add model
                </a>
              </div>
              {hasModels ? (
                <SortableModelList
                  entryId={data.entry.id}
                  models={data.models}
                  entities={data.entities}
                  relations={data.relations}
                  currentUserId={currentUserId}
                />
              ) : (
                <p className={styles.modelsEmpty}>
                  No models yet. Add the first refinement or prediction built
                  from this entry&apos;s data.
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

function getFastaMetadata(data: EntryPageData): FastaMetadata | null {
  for (const entity of data.entities) {
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
  return data.entities
    .filter((entity) => entity.level === "L0" && entity.type === "data")
    .map((entity) => {
      const payload = entity.payload as DataPayload;
      return {
        id: entity.id,
        name: entity.name,
        type: payload?.type,
        size: payload?.size,
        url: typeof payload?.file_url === "string" ? payload.file_url : null,
        entity,
      };
    })
    .sort((a, b) => Number(b.type === "fasta") - Number(a.type === "fasta"));
}
