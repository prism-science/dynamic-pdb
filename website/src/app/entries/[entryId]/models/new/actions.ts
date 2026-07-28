"use server";

import { redirect } from "next/navigation";

import { createModel, type CreateModelInput } from "@/lib/api/entries";
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
    await createModel(session.token, entryId, input);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to add the model.",
    };
  }

  redirect(`/entries/${encodeURIComponent(entryId)}#models`);
}
