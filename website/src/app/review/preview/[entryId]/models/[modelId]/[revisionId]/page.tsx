import { notFound } from "next/navigation";

import {
  ApiRequestError,
  getModelRevision,
  getUserModelRevision,
} from "@/lib/api/entries";
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

  return (
    <RevisionPreviewFrame kind="model" title={revision.title?.trim() || revision.model_id}>
      <ModelRevisionPreview revision={revision} />
    </RevisionPreviewFrame>
  );
}
