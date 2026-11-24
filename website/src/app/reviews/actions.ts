"use server";

import { revalidatePath, revalidateTag } from "next/cache";

import { decideEntryReview } from "@/lib/api/entries";
import type { RevisionTarget } from "@/lib/api/entries";
import { reviewPermissionsFor } from "@/lib/auth/permissions";
import { getAuthSession } from "@/lib/auth/session";
import { REVIEW_COUNT_TAG } from "./count";
import { decisionErrorMessage } from "./decisionMessage";

type ActionResult = { error: string } | void;

export async function approveSubmissionAction(
  target: RevisionTarget,
): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to review." };
  }
  if (!reviewPermissionsFor(session.permissions).canApprove) {
    return { error: "You don't have permission to approve." };
  }

  try {
    await decideEntryReview(session.token, target, "active");
  } catch (error) {
    return { error: decisionErrorMessage(error, "Failed to approve.") };
  }

  // Revalidate the whole tree so the public entries list refreshes, and drop the
  // cached badge count: revalidatePath does not reach into unstable_cache.
  revalidatePath("/", "layout");
  revalidateTag(REVIEW_COUNT_TAG, "max");
}

export async function rejectSubmissionAction(
  target: RevisionTarget,
): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to review." };
  }
  if (!reviewPermissionsFor(session.permissions).canReject) {
    return { error: "You don't have permission to reject." };
  }

  try {
    await decideEntryReview(session.token, target, "rejected");
  } catch (error) {
    return { error: decisionErrorMessage(error, "Failed to reject.") };
  }

  revalidatePath("/", "layout");
  revalidateTag(REVIEW_COUNT_TAG, "max");
}
