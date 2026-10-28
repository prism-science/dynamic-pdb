import { notFound, redirect } from "next/navigation";

import { ApiRequestError, getEntryPageData } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import EditEntryForm from "@/app/components/EditEntryForm";

import styles from "@/app/entries/new/new-entry.module.css";

export const dynamic = "force-dynamic";

type EditEntryPageProps = {
  params: Promise<{ entryId: string }>;
};

// The form starts from the entry's active revision every time: an edit keeps
// no saved draft.
export default async function EditEntryPage({ params }: EditEntryPageProps) {
  const { entryId } = await params;
  const returnTo = `/entries/${encodeURIComponent(entryId)}/edit`;

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
        <h1 className={styles.title}>Edit entry</h1>
        <EditEntryForm
          entry={data.entry}
          files={data.entities.filter((entity) => entity.model_id === null)}
        />
      </div>
    </main>
  );
}
