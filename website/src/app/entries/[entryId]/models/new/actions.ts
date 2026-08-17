"use server";

import { redirect } from "next/navigation";

import {
  createModel,
  submitModel,
  type CreateModelInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

export async function createModelAction(
  entryId: string,
  input: CreateModelInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to add a model." };
  }

  try {
    // A new model is a draft that goes through review before it appears on the
    // entry: create it, then submit it for review.
    const modelId = await createModel(session.token, entryId, input);
    await submitModel(session.token, entryId, modelId);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to add the model.",
    };
  }

  redirect(`/entries/${encodeURIComponent(entryId)}`);
}
