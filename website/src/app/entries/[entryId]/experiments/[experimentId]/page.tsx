import { notFound, redirect } from "next/navigation";

import {
  ApiRequestError,
  type DataPayload,
  type ExperimentPageData,
  type FastaMetadata,
  getExperimentPageData,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import DataTable from "@/app/components/DataTable";
import SequenceView from "@/app/components/SequenceView";
import {
  buildProvenance,
  EntryHero,
  getEntityFileURL,
  ModelCard,
  SectionHeader,
} from "../../entry-view";

import styles from "../../entry-page.module.css";

export const dynamic = "force-dynamic";

type ExperimentRouteProps = {
  params: Promise<{ entryId: string; experimentId: string }>;
};

export default async function ExperimentPage({
  params,
}: ExperimentRouteProps) {
  const { entryId, experimentId } = await params;
  const session = await getAuthSession();
  const returnTo = `/entries/${entryId}/experiments/${experimentId}`;

  if (!session) {
    redirect(loginRedirectPath(returnTo));
  }

  const data = await loadExperimentPage(
    session.token,
    entryId,
    experimentId,
    returnTo,
  );
  const provenance = buildProvenance(data.entities, data.relations);

  const model = data.entities.find((entity) => entity.type === "model") ?? null;
  const modelFileURL = model ? getEntityFileURL(model) : null;

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
      aria-label={`${data.entry.name} experiment ${data.experiment.name}`}
    >
      <section className={styles.shell}>
        <Breadcrumbs
          items={[
            { label: "Entries", href: "/" },
            { label: data.entry.name, href: `/entries/${data.entry.id}` },
            { label: data.experiment.name },
          ]}
        />

        <EntryHero
          title={data.experiment.name}
          action={
            modelFileURL ? (
              <a
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
              </a>
            ) : null
          }
        />

        {model ? (
          <ModelCard entity={model} provenance={provenance} />
        ) : (
          <p className={styles.emptyState}>No model produced yet.</p>
        )}

        {sequence ? (
          <section className={styles.experimentsBlock}>
            <SectionHeader title="Sequence" />
            <SequenceView metadata={sequence} />
          </section>
        ) : null}

        <section className={styles.experimentsBlock}>
          <SectionHeader title="Data" />
          <DataTable entities={data.entities} />
        </section>
      </section>
    </main>
  );
}

async function loadExperimentPage(
  token: string,
  entryId: string,
  experimentId: string,
  returnTo: string,
): Promise<ExperimentPageData> {
  try {
    return await getExperimentPageData(token, entryId, experimentId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 401) {
      redirect(loginRedirectPath(returnTo));
    }
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}

function loginRedirectPath(returnTo: string): string {
  const encodedReturnTo = encodeURIComponent(returnTo);
  if (process.env.NODE_ENV !== "production") {
    return `/auth/local-demo?return_to=${encodedReturnTo}`;
  }
  return `/auth/github/login?return_to=${encodedReturnTo}`;
}
