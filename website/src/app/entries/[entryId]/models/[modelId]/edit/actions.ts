"use server";

import { redirect } from "next/navigation";

import {
  ApiRequestError,
  createModelRevision,
  createModelRevisionRequestBody,
  type ModelRevisionChange,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  type ApiFailure,
} from "@/app/components/entry-form/apiErrors";

import { CHANGE_SUBMITTED_PARAM } from "../../../changeNotice";

/** Sends a model edit as a revision for review, then returns to the model. */
export async function updateModelAction(
  entryId: string,
  modelId: string,
  change: ModelRevisionChange,
): Promise<ApiFailure | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to edit a model.", fieldErrors: [] };
  }

  try {
    await createModelRevision(session.token, entryId, modelId, change);
  } catch (error) {
    return apiFailure(
      error instanceof Error ? error.message : "Failed to save the change.",
      error instanceof ApiRequestError ? error.errors : [],
      () => createModelRevisionRequestBody(change),
    );
  }

  redirect(
    `/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
      modelId,
    )}?${CHANGE_SUBMITTED_PARAM}=1`,
  );
}
