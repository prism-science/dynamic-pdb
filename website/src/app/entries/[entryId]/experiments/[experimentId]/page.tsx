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
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  const data = await loadExperimentPage(session.token, entryId, experimentId);
  const provenance = buildProvenance(data.entities, data.relations);

  const model = data.entities.find((entity) => entity.type === "model") ?? null;

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

        <EntryHero title={data.experiment.name} />

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
): Promise<ExperimentPageData> {
  try {
    return await getExperimentPageData(token, entryId, experimentId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
