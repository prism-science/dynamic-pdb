"use client";

import { useState, useTransition, type MouseEvent } from "react";
import { useRouter } from "next/navigation";

import styles from "./DeleteButton.module.css";

export default function DeleteButton({
  action,
  itemName,
  itemKind,
  className,
}: {
  action: () => Promise<{ error: string } | void>;
  itemName: string;
  itemKind: string;
  className?: string;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    // The button lives inside a clickable card/row; keep the click local.
    event.preventDefault();
    event.stopPropagation();
    if (pending) {
      return;
    }
    const confirmed = window.confirm(
      `Delete ${itemKind} “${itemName}”? This can’t be undone.`,
    );
    if (!confirmed) {
      return;
    }
    setError(null);
    startTransition(async () => {
      const result = await action();
      if (result?.error) {
        setError(result.error);
        return;
      }
      router.refresh();
    });
  };

  return (
    <button
      type="button"
      className={`${styles.button} ${className ?? ""}`}
      onClick={handleClick}
      disabled={pending}
      aria-label={`Delete ${itemKind} ${itemName}`}
      aria-busy={pending}
      title={error ?? `Delete ${itemKind}`}
      data-error={error ? "true" : undefined}
    >
      {pending ? (
        <svg
          className={styles.spinner}
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
        >
          <circle
            cx="12"
            cy="12"
            r="9"
            stroke="currentColor"
            strokeWidth="2.4"
            strokeLinecap="round"
            strokeDasharray="42"
            strokeDashoffset="14"
          />
        </svg>
      ) : (
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2m2 0-.7 12a2 2 0 0 1-2 1.9H8.7a2 2 0 0 1-2-1.9L6 7" />
          <path d="M10 11v6M14 11v6" />
        </svg>
      )}
    </button>
  );
}
