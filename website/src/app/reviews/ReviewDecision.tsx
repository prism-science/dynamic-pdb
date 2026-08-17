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
    <div className={styles.decision}>
      <div className={styles.decisionButtons}>
        <button
          type="button"
          className={`${styles.button} ${styles.approve}`}
          onClick={approve}
          disabled={pending}
        >
          Approve submission
        </button>
        <button
          type="button"
          className={`${styles.button} ${styles.reject}`}
          onClick={reject}
          disabled={pending}
        >
          Reject submission
        </button>
      </div>

      {error ? <p className={styles.decisionError}>{error}</p> : null}
    </div>
  );
}
