import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type StructurePageData,
  type FastaMetadata,
  getStructurePageData,
} from "@/lib/api/structures";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import FileList, { type FileItem } from "@/app/components/FileList";
import SequenceView from "@/app/components/SequenceView";
import SortableModelList from "@/app/components/SortableModelList";
import { ImagePlaceholderIcon } from "./structure-view";

import styles from "./structure-page.module.css";

export const dynamic = "force-dynamic";

type StructureRouteProps = {
  params: Promise<{ structureId: string }>;
};

export default async function StructurePage({ params }: StructureRouteProps) {
  const { structureId } = await params;
  const session = await getAuthSession();

  const data = await loadStructurePage(session?.token, structureId);
  const currentUserId = session ? userIdFromToken(session.token) : null;
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
  const hasModels = data.models.length > 0;

  return (
    <main className={styles.page} aria-label={`${data.structure.name} structure`}>
      <div className={styles.record}>
        <Breadcrumbs
          items={[{ label: "Structures", href: "/" }, { label: data.structure.name }]}
        />

        <div className={styles.layout}>
          <aside className={styles.sidebar}>
            <div
              className={styles.sideThumb}
              data-empty={data.structure.thumbnail_image_url ? undefined : "true"}
            >
              {data.structure.thumbnail_image_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={data.structure.thumbnail_image_url} alt="" />
              ) : (
                <ImagePlaceholderIcon size={34} />
              )}
            </div>

            <h1 className={styles.sideName}>{data.structure.name}</h1>

            {vitals.length > 0 ? (
              <dl className={styles.sideVitals}>
                {vitals.map((item) => (
                  <div key={item}>{item}</div>
                ))}
              </dl>
            ) : null}
          </aside>

          <div className={styles.content}>
            {data.structure.description?.trim() ? (
              <p className={styles.lead}>{data.structure.description}</p>
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

            {/* Always rendered: a structure with no models still needs somewhere
                to add the first one. */}
            <section id="models" className={styles.contentSection}>
              <div className={styles.contentSectionHead}>
                <h2 className={styles.contentHeading}>Models</h2>
                <a
                  className={styles.addModelLink}
                  href={`/structures/${encodeURIComponent(data.structure.id)}/models/new`}
                >
                  Add model
                </a>
              </div>
              {hasModels ? (
                <SortableModelList
                  structureId={data.structure.id}
                  models={data.models}
                  entities={data.entities}
                  relations={data.relations}
                  currentUserId={currentUserId}
                />
              ) : (
                <p className={styles.modelsEmpty}>
                  No models yet. Add the first refinement or prediction built
                  from this structure&apos;s data.
                </p>
              )}
            </section>
          </div>
        </div>
      </div>
    </main>
  );
}

async function loadStructurePage(
  token: string | undefined,
  structureId: string,
): Promise<StructurePageData> {
  try {
    return await getStructurePageData(token, structureId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}

function getFastaMetadata(data: StructurePageData): FastaMetadata | null {
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
function getLevelZeroFiles(data: StructurePageData): FileItem[] {
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
