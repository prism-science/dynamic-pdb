import { ApiRequestError } from "@/lib/api/entries";
import { ownRevisionMessage } from "@/lib/auth/permissions";

/**
 * What a reviewer reads when Approve or Reject fails: the API's refusal in
 * plain words. Kept apart from the server actions so it can be tested.
 */
export function decisionErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) {
      if (error.errors.some((item) => item.code === "SELF_REVIEW")) {
        return ownRevisionMessage;
      }
      return "You don't have permission to make this decision.";
    }
    if (error.status === 404) {
      return "This submission is no longer in the queue.";
    }
    if (error.status === 409) {
      // A PDB deposition filed under another entry waits for the grouping
      // rules (researcher answer of 17 Nov, Q11); the API says which PDB IDs.
      const grouping = error.errors.find((item) => item.code === "GROUPING_NOT_APPROVED");
      if (grouping?.detail) {
        return grouping.detail;
      }
      return "This submission changed while you were looking at it.";
    }
    if (error.status === 401) {
      return "You must be signed in to review.";
    }
  }
  return error instanceof Error ? error.message : fallback;
}
