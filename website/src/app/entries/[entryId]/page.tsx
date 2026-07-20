import { notFound, redirect } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type EntryPageData,
  type FastaMetadata,
  getEntryPageData,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import FileList, { type FileItem } from "@/app/components/FileList";
import SequenceView from "@/app/components/SequenceView";
import { ExperimentList, ImagePlaceholderIcon } from "./entry-view";

import styles from "./entry-page.module.css";

export const dynamic = "force-dynamic";

type EntryRouteProps = {
  params: Promise<{ entryId: string }>;
};

export default async function EntryPage({ params }: EntryRouteProps) {
  const { entryId } = await params;
  const session = await getAuthSession();

  if (!session) {
    redirect(
      `/auth/github/login?return_to=/entries/${encodeURIComponent(entryId)}`,
    );
  }

  const data = await loadEntryPage(session.token, entryId);
  const sequence = getFastaMetadata(data);

  const vitals: string[] = [];
  if (sequence) {
    const length =
      typeof sequence.length === "number"
        ? sequence.length
        : typeof sequence.sequence === "string"
          ? sequence.sequence.replace(/\s+/g, "").length
          : 0;
    if (length > 0) {
      vitals.push(`${length.toLocaleString()} residues`);
    }
    if (typeof sequence.chains === "number") {
      vitals.push(`${sequence.chains} ${sequence.chains === 1 ? "chain" : "chains"}`);
    }
  }

  const files = getLevelZeroFiles(data);

  const hasSequence = sequence !== null;
  const hasFiles = files.length > 0;
  const hasExperiments = data.experiments.length > 0;

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

            {hasSequence ? (
              <section id="sequence" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Sequence</h2>
                <SequenceView metadata={sequence} />
              </section>
            ) : null}

            {hasFiles ? (
              <section id="files" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Files</h2>
                <FileList items={files} />
              </section>
            ) : null}

            {hasExperiments ? (
              <section id="experiments" className={styles.contentSection}>
                <h2 className={styles.contentHeading}>Models</h2>
                <ExperimentList
                  entryId={data.entry.id}
                  entities={data.entities}
                  experiments={data.experiments}
                />
              </section>
            ) : null}
          </div>
        </div>
      </div>
    </main>
  );
}

async function loadEntryPage(
  token: string,
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

// L0 raw files shown as a generic list (the sequence is rendered separately, so
// the FASTA entity is excluded here).
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
      };
    })
    .filter((item) => item.type !== "fasta");
}
