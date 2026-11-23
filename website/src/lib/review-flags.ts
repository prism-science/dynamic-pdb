import type { components } from "@/lib/api/schema.gen";

type ReviewFlag = components["schemas"]["ReviewFlag"];

/**
 * Review flags (researcher answer 7). Everyone sees whether a record is
 * flagged for review. Only reviewers and admins see which issues: the API
 * sends `review_flags` to them alone, so the website shows the issues exactly
 * when the response carries them, and never decides that itself.
 */

/** The never-stored permission key the sign-in response adds for reviewers
 *  and admins. It only says what to offer (the flagged filter); the API
 *  checks the role on every request. */
const reviewFlagsRead = "review_flags.read";

/** Whether to offer the flagged filter to the signed-in user. */
export function canSeeReviewFlags(permissions: readonly string[]): boolean {
  return permissions.includes(reviewFlagsRead);
}

/** What a page shows about a record's review flags. */
export type ReviewStatus = {
  flagged: boolean;
  /** The named issues, one line each; null when the API does not show them
   *  to this caller. */
  issueLines: string[] | null;
};

export const NOT_FLAGGED: ReviewStatus = { flagged: false, issueLines: null };

/** The review status of a record as the API returned it to this caller. A
 *  response without flagged_for_review (an older server) reads as unflagged. */
export function reviewStatus(record: {
  flagged_for_review?: boolean;
  review_flags?: readonly ReviewFlag[];
}): ReviewStatus {
  return {
    flagged: record.flagged_for_review === true,
    issueLines: record.review_flags ? reviewFlagLines(record.review_flags) : null,
  };
}

// Open vocabularies on the server: a flag or object type added there must not
// break the page, so these are lookups with the raw code as the fallback
// rather than switches that fail. Only reviewers and admins ever see them.
const REVIEW_ISSUE_LABELS: Readonly<Record<string, string>> = {
  mutations_need_review: "Mutations need review",
  ligands_cofactors_rederive_from_archive: "Ligands and Cofactors need review",
  sync_validation_failed: "The latest PDB update failed validation",
};

const REVIEW_OBJECT_LABELS: Readonly<Record<string, [singular: string, plural: string]>> = {
  entry: ["entry", "entries"],
  model: ["model", "models"],
  model_revision: ["model version", "model versions"],
  polymer_entity: ["polymer entity", "polymer entities"],
};

/**
 * Open review flags as lines, one per issue and kind of record:
 * "Mutations need review (2 polymer entities)".
 */
export function reviewFlagLines(flags: readonly ReviewFlag[]): string[] {
  const counts = new Map<string, { issue: string; objectType: string; count: number }>();
  for (const flag of flags) {
    const key = `${flag.issue}\u0000${flag.object_type}`;
    const current = counts.get(key);
    if (current) {
      current.count += 1;
    } else {
      counts.set(key, { issue: flag.issue, objectType: flag.object_type, count: 1 });
    }
  }
  return [...counts.values()].map(({ issue, objectType, count }) => {
    const issueLabel = REVIEW_ISSUE_LABELS[issue] ?? issue;
    const [singular, plural] = REVIEW_OBJECT_LABELS[objectType] ?? [objectType, objectType];
    return `${issueLabel} (${count === 1 ? singular : `${count} ${plural}`})`;
  });
}
