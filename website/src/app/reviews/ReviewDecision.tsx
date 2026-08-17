"use client";

import { useState, useTransition } from "react";

import { approveSubmissionAction, rejectSubmissionAction } from "./actions";
import styles from "./reviews.module.css";

type Props = {
  entryId: string;
};

/** One decision per submission: the entry revision and every model revision in
 *  review under it are approved or rejected together. */
export default function ReviewDecision({ entryId }: Props) {
  const [pending, startTransition] = useTransition();
  const [rejecting, setRejecting] = useState(false);
  const [comment, setComment] = useState("");
  const [error, setError] = useState<string | null>(null);

  function approve() {
    setError(null);
    startTransition(async () => {
      const result = await approveSubmissionAction(entryId);
      if (result && "error" in result) setError(result.error);
    });
  }

  function reject() {
    if (!rejecting) {
      setRejecting(true);
      return;
    }
    setError(null);
    startTransition(async () => {
      const result = await rejectSubmissionAction(entryId, comment);
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
          {rejecting ? "Confirm reject" : "Reject submission"}
        </button>
        {rejecting ? (
          <button
            type="button"
            className={styles.buttonGhost}
            onClick={() => {
              setRejecting(false);
              setComment("");
              setError(null);
            }}
            disabled={pending}
          >
            Cancel
          </button>
        ) : null}
      </div>

      {rejecting ? (
        <textarea
          className={styles.commentBox}
          placeholder="Why is this being rejected? (required)"
          value={comment}
          onChange={(event) => setComment(event.target.value)}
          rows={2}
          disabled={pending}
        />
      ) : null}

      {error ? <p className={styles.decisionError}>{error}</p> : null}
    </div>
  );
}
