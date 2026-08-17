"use server";

import { redirect } from "next/navigation";

import {
  createEntry,
  submitEntryRevision,
  submitModelRevision,
  type CreateEntryInput,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";

export async function createEntryAction(
  input: CreateEntryInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to create an entry." };
  }

  try {
    const userId = userIdFromToken(session.token);
    if (!userId) {
      return { error: "Your session does not contain a user ID." };
    }
    const result = await createEntry(session.token, input);
    await Promise.all([
      submitEntryRevision(
        session.token,
        userId,
        result.entry_id,
        result.revision_id,
      ),
      ...result.model_results.map((model) =>
        submitModelRevision(
          session.token,
          userId,
          result.entry_id,
          model.model_id,
          model.model_revision_id,
        ),
      ),
    ]);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to create the entry.",
    };
  }

  redirect("/entries?tab=under-review");
}
