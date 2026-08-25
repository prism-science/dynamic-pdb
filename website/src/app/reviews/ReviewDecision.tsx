"use client";

import { useState, useTransition } from "react";

import type { RevisionTarget } from "@/lib/api/entries";

import { approveSubmissionAction, rejectSubmissionAction } from "./actions";
import styles from "./reviews.module.css";

type Props = {
  target: RevisionTarget;
  canApprove: boolean;
  canReject: boolean;
  /** When set, the decision cannot be made yet and this says why. */
  blockedReason?: string | null;
};

export default function ReviewDecision({
  target,
  canApprove,
  canReject,
  blockedReason,
}: Props) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const blocked = Boolean(blockedReason);

  function approve() {
    setError(null);
    startTransition(async () => {
      const result = await approveSubmissionAction(target);
      if (result && "error" in result) setError(result.error);
    });
  }

  function reject() {
    setError(null);
    startTransition(async () => {
      const result = await rejectSubmissionAction(target);
      if (result && "error" in result) setError(result.error);
    });
  }

  return (
    <div className={styles.decisionInline}>
      {canApprove ? (
        <button
          type="button"
          className={`${styles.buttonSm} ${styles.approve}`}
          onClick={approve}
          disabled={pending || blocked}
          title={blockedReason ?? undefined}
        >
          Approve
        </button>
      ) : null}
      {canReject ? (
        <button
          type="button"
          className={`${styles.buttonSm} ${styles.reject}`}
          onClick={reject}
          disabled={pending || blocked}
          title={blockedReason ?? undefined}
        >
          Reject
        </button>
      ) : null}
      {error ? <span className={styles.decisionErrorInline}>{error}</span> : null}
    </div>
  );
}
