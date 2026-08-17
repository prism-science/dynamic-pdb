"use client";

import { useState, useTransition } from "react";

import type { RevisionTarget } from "@/lib/api/entries";

import { approveSubmissionAction, rejectSubmissionAction } from "./actions";
import styles from "./reviews.module.css";

type Props = {
  target: RevisionTarget;
};

export default function ReviewDecision({ target }: Props) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

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
      <button
        type="button"
        className={`${styles.buttonSm} ${styles.approve}`}
        onClick={approve}
        disabled={pending}
      >
        Approve
      </button>
      <button
        type="button"
        className={`${styles.buttonSm} ${styles.reject}`}
        onClick={reject}
        disabled={pending}
      >
        Reject
      </button>
      {error ? <span className={styles.decisionErrorInline}>{error}</span> : null}
    </div>
  );
}
