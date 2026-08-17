import { notFound } from "next/navigation";

import {
  ApiRequestError,
  getModelRevision,
  getUserModelRevision,
} from "@/lib/api/entries";
import {
  getAuthSession,
  getCurrentUserId,
  userIdFromToken,
} from "@/lib/auth/session";
import { isConfiguredAdmin } from "@/app/reviews/admin";

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
  const isAdmin = isConfiguredAdmin(await getCurrentUserId());
  const userId = userIdFromToken(session.token);
  if (!isAdmin && !userId) {
    notFound();
  }

  let revision;
  try {
    revision = isAdmin
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
    <RevisionPreviewFrame kind="model" name={revision.name}>
      <ModelRevisionPreview revision={revision} />
    </RevisionPreviewFrame>
  );
}
