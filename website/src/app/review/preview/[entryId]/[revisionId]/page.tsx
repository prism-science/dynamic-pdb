import { notFound } from "next/navigation";

import {
  ApiRequestError,
  getEntryReview,
  getEntryRevision,
  type ModelRevision,
} from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import { isConfiguredAdmin } from "@/app/reviews/admin";

import {
  EntryRevisionPreview,
  RevisionPreviewFrame,
} from "../../revision-view";

export const dynamic = "force-dynamic";

type Props = {
  params: Promise<{ entryId: string; revisionId: string }>;
};

export default async function EntryRevisionPreviewPage({ params }: Props) {
  const { entryId, revisionId } = await params;
  const session = await getAuthSession();
  if (!session) {
    notFound();
  }
  if (!isConfiguredAdmin(await getCurrentUserId())) {
    notFound();
  }

  const revision = await load(() =>
    getEntryRevision(session.token, entryId, revisionId),
  );
  if (!revision) {
    notFound();
  }

  // The models that would go live alongside this revision: whatever is waiting
  // under the same entry. Assembled here, not asked of the backend.
  const models = await modelsInReview(session.token, entryId);

  return (
    <RevisionPreviewFrame kind="entry" name={revision.name}>
      <EntryRevisionPreview revision={revision} models={models} />
    </RevisionPreviewFrame>
  );
}

async function modelsInReview(
  token: string,
  entryId: string,
): Promise<ModelRevision[]> {
  const review = await load(() => getEntryReview(token, entryId));
  return review ? review.models.map((model) => model.proposed) : [];
}

async function load<T>(read: () => Promise<T>): Promise<T | null> {
  try {
    return await read();
  } catch (error) {
    if (error instanceof ApiRequestError) return null;
    throw error;
  }
}
