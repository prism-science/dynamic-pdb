"use server";

import { redirect } from "next/navigation";

import { createModel, type CreateModelInput } from "@/lib/api/structures";
import { getAuthSession } from "@/lib/auth/session";

export async function createModelAction(
  structureId: string,
  input: CreateModelInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to add a model." };
  }

  try {
    await createModel(session.token, structureId, input);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to add the model.",
    };
  }

  redirect(`/structures/${encodeURIComponent(structureId)}#models`);
}
