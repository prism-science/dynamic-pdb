import { notFound, redirect } from "next/navigation";

import {
  ApiRequestError,
  getEntryPageData,
  type Entity,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import NewModelForm from "@/app/components/NewModelForm";

import styles from "@/app/entries/new/new-entry.module.css";

export const dynamic = "force-dynamic";

type NewModelPageProps = {
  params: Promise<{ entryId: string }>;
};

export default async function NewModelPage({ params }: NewModelPageProps) {
  const { entryId } = await params;
  const returnTo = `/entries/${encodeURIComponent(entryId)}/models/new`;

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  let data;
  try {
    data = await getEntryPageData(session.token, entryId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <h1 className={styles.title}>New model</h1>
        <NewModelForm
          entry={data.entry}
          entryDataEntities={entryDataEntities(data.entities)}
          modelCount={data.models.length}
        />
      </div>
    </main>
  );
}

// Files already deposited on the entry are the existing entities a new model may
// point at: they can be inputs to the program that produced it. Structure files
// count too — a .pdb/.cif is stored as a "model" entity, and an entry-level one
// is often the starting geometry.
function entryDataEntities(entities: Entity[]): Entity[] {
  return entities.filter(
    (entity) =>
      entity.model_id === null &&
      (entity.type === "data" || entity.type === "model"),
  );
}
