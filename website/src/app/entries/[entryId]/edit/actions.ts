"use server";

import { redirect } from "next/navigation";

import {
  ApiRequestError,
  createEntryRevision,
  createEntryRevisionRequestBody,
  type EntryRevisionChange,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  type ApiFailure,
} from "@/app/components/entry-form/apiErrors";

import { CHANGE_SUBMITTED_PARAM } from "../changeNotice";

/** Sends an entry edit as a revision for review, then returns to the entry. */
export async function updateEntryAction(
  entryId: string,
  change: EntryRevisionChange,
): Promise<ApiFailure | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to edit an entry.", fieldErrors: [] };
  }

  try {
    await createEntryRevision(session.token, entryId, change);
  } catch (error) {
    return apiFailure(
      error instanceof Error ? error.message : "Failed to save the change.",
      error instanceof ApiRequestError ? error.errors : [],
      () => createEntryRevisionRequestBody(change),
    );
  }

  redirect(`/entries/${encodeURIComponent(entryId)}?${CHANGE_SUBMITTED_PARAM}=1`);
}
