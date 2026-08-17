import { notFound } from "next/navigation";

import { ApiRequestError, getModelRevision } from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
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
  if (!isConfiguredAdmin(await getCurrentUserId())) {
    notFound();
  }

  let revision;
  try {
    revision = await getModelRevision(session.token, entryId, modelId, revisionId);
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
