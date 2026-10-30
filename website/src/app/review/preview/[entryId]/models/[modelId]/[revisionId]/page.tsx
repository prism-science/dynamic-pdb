import { notFound } from "next/navigation";

import {
  ApiRequestError,
  getEntry,
  getModelRevision,
  getUserModelRevision,
} from "@/lib/api/entries";
import type { EntryCompoundLists } from "@/lib/modeled-compounds";
import {
  getAuthSession,
  userIdFromToken,
} from "@/lib/auth/session";
import { hasReviewAccess } from "@/lib/auth/permissions";

import {
  ModelRevisionPreview,
  RevisionPreviewFrame,
} from "../../../../revision-view";

export const dynamic = "force-dynamic";

type Props = {
  params: Promise<{ entryId: string; modelId: string; revisionId: string }>;
};

export default async function ModelRevisionPreviewPage({ params }: Props) {
  const { entryId, modelId, revisionId } = await params;
  const session = await getAuthSession();
  if (!session) {
    notFound();
  }
  const isReviewer = hasReviewAccess(session.permissions);
  const userId = userIdFromToken(session.token);
  if (!isReviewer && !userId) {
    notFound();
  }

  let revision;
  try {
    revision = isReviewer
      ? await getModelRevision(session.token, entryId, modelId, revisionId)
      : await getUserModelRevision(
          session.token,
          userId as string,
          entryId,
          modelId,
          revisionId,
        );
  } catch (error) {
    if (error instanceof ApiRequestError) notFound();
    throw error;
  }

  const entryLists = await loadEntryCompoundLists(session.token, entryId);

  return (
    <RevisionPreviewFrame kind="model" title={revision.title?.trim() || revision.model_id}>
      <ModelRevisionPreview revision={revision} entryLists={entryLists} />
    </RevisionPreviewFrame>
  );
}

// The published entry's Ligands and Cofactors lists, which the compound tags come
// from. An entry not yet published has none to read; its compounds then carry no tags.
async function loadEntryCompoundLists(
  token: string,
  entryId: string,
): Promise<EntryCompoundLists | null> {
  try {
    const entry = await getEntry(token, entryId);
    return { ligands: entry.ligands ?? [], cofactors: entry.cofactors ?? [] };
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return null;
    }
    throw error;
  }
}
