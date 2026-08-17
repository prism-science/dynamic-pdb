"use server";

import { redirect } from "next/navigation";

import {
  createModel,
  submitModelRevision,
  type CreateModelInput,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";

export async function createModelAction(
  entryId: string,
  input: CreateModelInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to add a model." };
  }

  try {
    const userId = userIdFromToken(session.token);
    if (!userId) {
      return { error: "Your session does not contain a user ID." };
    }
    const result = await createModel(session.token, entryId, input);
    await submitModelRevision(
      session.token,
      userId,
      result.entry_id,
      result.model_id,
      result.revision_id,
    );
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to add the model.",
    };
  }

  redirect(`/entries/${encodeURIComponent(entryId)}`);
}
