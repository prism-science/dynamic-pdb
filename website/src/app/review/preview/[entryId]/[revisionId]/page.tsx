import { notFound } from "next/navigation";

import {
  ApiRequestError,
  findReviewItem,
  findUserSubmissionItem,
  getEntryReview,
  getEntryRevision,
  getUserEntryRevision,
  getUserSubmission,
  type ModelRevision,
} from "@/lib/api/entries";
import {
  getAuthSession,
  userIdFromToken,
} from "@/lib/auth/session";
import { hasReviewAccess } from "@/lib/auth/permissions";
import { formatEntryLabel } from "@/lib/entry-label";

import {
  EntryRevisionPreview,
  RevisionPreviewFrame,
} from "../../revision-view";

export const dynamic = "force-dynamic";

type Props = {
  params: Promise<{ entryId: string; revisionId: string }>;
};

// Reachable by the reviewer of the submission and by the author who made it.
// Which one decides where the revision is read from: the reviewer routes
// refuse anyone else, the user-scoped ones refuse anyone but the owner.
export default async function EntryRevisionPreviewPage({ params }: Props) {
  const { entryId, revisionId } = await params;
  const session = await getAuthSession();
  if (!session) {
    notFound();
  }
  const isReviewer = hasReviewAccess(session.permissions);
  const userId = userIdFromToken(session.token);
  if (!isReviewer && !userId) {
    notFound();
  }

  const revision = await load(() =>
    isReviewer
      ? getEntryRevision(session.token, entryId, revisionId)
      : getUserEntryRevision(session.token, userId as string, entryId, revisionId),
  );
  if (!revision) {
    notFound();
  }

  // The models that would go live alongside this revision: whatever is waiting
  // under the same entry. Assembled here, not asked of the backend.
  const models = await modelsInReview(
    session.token,
    entryId,
    isReviewer ? null : (userId as string),
  );

  return (
    <RevisionPreviewFrame
      kind="entry"
      title={formatEntryLabel({
        id: revision.entry_id,
        title: revision.title,
        external_refs: revision.external_refs,
      })}
    >
      <EntryRevisionPreview revision={revision} models={models} />
    </RevisionPreviewFrame>
  );
}

async function modelsInReview(
  token: string,
  entryId: string,
  authorId: string | null,
): Promise<ModelRevision[]> {
  if (authorId) {
    const item = await load(() =>
      findUserSubmissionItem(token, authorId, "in_review", entryId),
    );
    if (!item) return [];
    const submission = await load(() => getUserSubmission(token, authorId, item));
    return submission ? submission.models.map((model) => model.proposed) : [];
  }

  const item = await load(() => findReviewItem(token, entryId));
  if (!item) return [];
  const review = await load(() => getEntryReview(token, item));
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
