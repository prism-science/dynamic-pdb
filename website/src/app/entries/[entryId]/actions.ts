"use server";

import { redirect } from "next/navigation";

import { deleteEntry } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import { ENTRY_DELETED_PARAM } from "@/app/browse/deletedNotice";

export type DeleteEntryFailure = { error: string };

/** Deletes the entry, then lands on the entries list, which says it was
 *  deleted. Returns the backend's message when it refuses. */
export async function deleteEntryAction(
  entryId: string,
): Promise<DeleteEntryFailure | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to delete an entry." };
  }

  try {
    await deleteEntry(session.token, entryId);
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : "Failed to delete the entry.",
    };
  }

  redirect(`/browse?${ENTRY_DELETED_PARAM}=${encodeURIComponent(entryId)}`);
}
