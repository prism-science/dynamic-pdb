"use client";

import { useState, useTransition } from "react";

import { deleteEntryAction } from "@/app/entries/[entryId]/actions";

import styles from "./DeleteEntryButton.module.css";

/** "Delete entry" in the entry rail. The first click asks to confirm in place
 *  (the site has no dialog component); the second deletes and leaves the page.
 *  Shown only to sessions holding entries.delete, which the backend grants on
 *  dev to reviewers. */
export default function DeleteEntryButton({ entryId }: { entryId: string }) {
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  if (!confirming) {
    return (
      <button
        type="button"
        className={styles.delete}
        onClick={() => {
          setError(null);
          setConfirming(true);
        }}
      >
        Delete entry
      </button>
    );
  }

  return (
    <div className={styles.confirm} role="group" aria-label="Confirm delete">
      <p className={styles.question}>
        Delete this entry? It disappears from the site, search and the API. Only
        an admin can bring it back.
      </p>
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.confirmDelete}
          disabled={pending}
          onClick={() =>
            startTransition(async () => {
              const failure = await deleteEntryAction(entryId);
              if (failure) {
                setError(failure.error);
              }
            })
          }
        >
          {pending ? "Deleting…" : "Delete"}
        </button>
        <button
          type="button"
          className={styles.cancel}
          disabled={pending}
          onClick={() => setConfirming(false)}
        >
          Cancel
        </button>
      </div>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
