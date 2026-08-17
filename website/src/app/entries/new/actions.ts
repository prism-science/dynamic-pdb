"use server";

import { redirect } from "next/navigation";

import { createEntry, type CreateEntryInput } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

export async function createEntryAction(
  input: CreateEntryInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to create an entry." };
  }

  try {
    await createEntry(session.token, input);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to create the entry.",
    };
  }

  redirect("/entries?tab=under-review");
}
