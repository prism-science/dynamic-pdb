import { notFound, redirect } from "next/navigation";

import { ApiRequestError, getModelPageData } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import EditModelForm from "@/app/components/EditModelForm";

import styles from "@/app/entries/new/new-entry.module.css";

export const dynamic = "force-dynamic";

type EditModelPageProps = {
  params: Promise<{ entryId: string; modelId: string }>;
};

// The form starts from the model's active revision every time: an edit keeps
// no saved draft.
export default async function EditModelPage({ params }: EditModelPageProps) {
  const { entryId, modelId } = await params;
  const returnTo = `/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
    modelId,
  )}/edit`;

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  let data;
  try {
    data = await getModelPageData(session.token, entryId, modelId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }

  const modelEntities = data.entities.filter(
    (entity) => entity.model_id === data.model.id,
  );
  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <h1 className={styles.title}>Edit model</h1>
        <EditModelForm
          entryId={data.entry.id}
          model={data.model}
          files={modelEntities.filter(
            (entity) => entity.type === "data" || entity.type === "model",
          )}
          runs={modelEntities.filter((entity) => entity.type === "program")}
        />
      </div>
    </main>
  );
}
