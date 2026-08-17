import type { RevisionState } from "@/lib/api/entries";

/** The only two states this screen shows: what is waiting on the reviewer, and
 *  what came back. Published entries live in the public catalog, so listing them
 *  here again said nothing. */
export const SUBMISSION_TABS = [
  {
    key: "under-review",
    label: "Under review",
    state: "in_review" as RevisionState,
    empty: "Nothing of yours is waiting for review.",
  },
  {
    key: "rejected",
    label: "Rejected",
    state: "rejected" as RevisionState,
    empty: "Nothing of yours was rejected.",
  },
] as const;

export type SubmissionTab = (typeof SUBMISSION_TABS)[number];

export const SUBMISSION_STATES: RevisionState[] = SUBMISSION_TABS.map(
  (tab) => tab.state,
);

export function tabFor(key: string | undefined): SubmissionTab {
  return SUBMISSION_TABS.find((tab) => tab.key === key) ?? SUBMISSION_TABS[0];
}
