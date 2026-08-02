import { notFound } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type ModelPageData,
  type FastaMetadata,
  getModelPageData,
} from "@/lib/api/structures";
import { getAuthSession } from "@/lib/auth/session";
import type { StructureMap } from "@/lib/structureKind";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import DataTable from "@/app/components/DataTable";
import ResolvedFileLink from "@/app/components/ResolvedFileLink";
import SequenceView from "@/app/components/SequenceView";
import {
  buildProvenance,
  StructureHero,
  getEntityFileURL,
  ModelCard,
  SectionHeader,
} from "../../structure-view";

import styles from "../../structure-page.module.css";

export const dynamic = "force-dynamic";

type ModelRouteProps = {
  params: Promise<{ structureId: string; modelId: string }>;
};

export default async function ModelPage({
  params,
}: ModelRouteProps) {
  const { structureId, modelId } = await params;
  const session = await getAuthSession();

  const data = await loadModelPage(session?.token, structureId, modelId);
  const provenance = buildProvenance(data.entities, data.relations);

  const model = data.entities.find((entity) => entity.type === "model") ?? null;
  const modelFileURL = model ? getEntityFileURL(model) : null;

  // For now, treat every nearby MTZ file as a density-map layer, regardless of
  // how it is related to the model.
  const maps: StructureMap[] = data.entities
    .filter(
      (entity) =>
        entity.type === "data" &&
        (entity.payload as DataPayload)?.type === "mtz",
    )
    .map((entity) => ({ url: getEntityFileURL(entity), name: entity.name }))
    .filter((map): map is StructureMap => typeof map.url === "string");

  let sequence: FastaMetadata | null = null;
  for (const entity of data.entities) {
    const payload = entity.payload as DataPayload;
    if (payload?.type === "fasta" && payload.metadata) {
      sequence = payload.metadata as FastaMetadata;
      break;
    }
  }

  return (
    <main
      className={styles.page}
      aria-label={`${data.structure.name} model ${data.model.name}`}
    >
      <section className={styles.shell}>
        <Breadcrumbs
          items={[
            { label: "Structures", href: "/" },
            { label: data.structure.name, href: `/structures/${data.structure.id}` },
            { label: data.model.name },
          ]}
        />

        <StructureHero
          title={data.model.name}
          action={
            modelFileURL ? (
              <ResolvedFileLink
                className={styles.heroDownload}
                href={modelFileURL}
                download
                rel="noreferrer"
                target="_blank"
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
                </svg>
                Download
              </ResolvedFileLink>
            ) : null
          }
        />

        {model ? (
          <ModelCard
            entity={model}
            provenance={provenance}
            thumbnailImageURL={data.model.thumbnail_image_url}
            maps={maps}
          />
        ) : (
          <p className={styles.emptyState}>No model produced yet.</p>
        )}

        {sequence ? (
          <section className={styles.modelsBlock}>
            <SectionHeader title="Sequence" />
            <SequenceView metadata={sequence} />
          </section>
        ) : null}

        <section className={styles.modelsBlock}>
          <SectionHeader title="Data" />
          <DataTable entities={data.entities} />
        </section>
      </section>
    </main>
  );
}

async function loadModelPage(
  token: string | undefined,
  structureId: string,
  modelId: string,
): Promise<ModelPageData> {
  try {
    return await getModelPageData(token, structureId, modelId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
