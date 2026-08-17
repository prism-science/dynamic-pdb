"use server";

import { revalidatePath } from "next/cache";

import { ApiRequestError, decideEntryReview } from "@/lib/api/entries";
import type { RevisionTarget } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

type ActionResult = { error: string } | void;

function messageForStatus(error: unknown, fallback: string): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) {
      return "Only the administrator can approve or reject.";
    }
    if (error.status === 404) {
      return "This submission is no longer in the queue.";
    }
    if (error.status === 409) {
      return "This submission changed while you were looking at it.";
    }
    if (error.status === 401) {
      return "You must be signed in to review.";
    }
  }
  return error instanceof Error ? error.message : fallback;
}

export async function approveSubmissionAction(
  target: RevisionTarget,
): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to review." };
  }

  try {
    await decideEntryReview(session.token, target, "active");
  } catch (error) {
    return { error: messageForStatus(error, "Failed to approve.") };
  }

  // Revalidate the whole tree so the header count (in the root layout) and the
  // public entries list both refresh.
  revalidatePath("/", "layout");
}

export async function rejectSubmissionAction(
  target: RevisionTarget,
): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to review." };
  }

  try {
    await decideEntryReview(session.token, target, "rejected");
  } catch (error) {
    return { error: messageForStatus(error, "Failed to reject.") };
  }

  revalidatePath("/", "layout");
}
