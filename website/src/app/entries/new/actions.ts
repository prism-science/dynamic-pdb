"use server";

import { redirect } from "next/navigation";

import {
  createEntry,
  submitEntry,
  type CreateEntryInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

export async function createEntryAction(
  input: CreateEntryInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to create an entry." };
  }

  try {
    // New entries are not published directly: create the draft, then submit it
    // for review. It becomes public only after a reviewer approves it.
    const entryId = await createEntry(session.token, input);
    await submitEntry(session.token, entryId);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to create the entry.",
    };
  }

  redirect("/entries?tab=under-review");
}
