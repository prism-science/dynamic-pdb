"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";

import styles from "./AppHeader.module.css";

type Props = {
  displayName: string;
  subLabel: string | null;
  initial: string;
  isReviewer?: boolean;
  toReviewCount?: number;
  toReviewCountCapped?: boolean;
};

export default function UserMenu({
  displayName,
  subLabel,
  initial,
  isReviewer = false,
  toReviewCount = 0,
  toReviewCountCapped = false,
}: Props) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    function handlePointer(event: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setOpen(false);
      }
    }

    function handleKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }

    document.addEventListener("mousedown", handlePointer);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handlePointer);
      document.removeEventListener("keydown", handleKey);
    };
  }, [open]);

  return (
    <div className={styles.menu} ref={containerRef}>
      <button
        type="button"
        className={styles.menuTrigger}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span className={styles.avatar} aria-hidden="true">
          {initial}
        </span>
        <span className={styles.userName}>{displayName}</span>
      </button>

      {open ? (
        <div className={styles.menuPanel} role="menu">
          <div className={styles.menuHeader}>
            <span className={styles.menuName}>{displayName}</span>
            {subLabel ? (
              <span className={styles.menuSub}>{subLabel}</span>
            ) : null}
          </div>

          <Link
            href="/entries"
            role="menuitem"
            className={styles.menuLink}
            onClick={() => setOpen(false)}
          >
            My entries
          </Link>

          {isReviewer ? (
            <Link
              href="/review"
              role="menuitem"
              className={styles.menuLink}
              onClick={() => setOpen(false)}
            >
              <span>To review</span>
              {toReviewCount > 0 ? (
                <span className={styles.menuCount}>
                  {toReviewCount}
                  {toReviewCountCapped ? "+" : ""}
                </span>
              ) : null}
            </Link>
          ) : null}

          <div className={styles.menuDivider} />

          <form action="/auth/logout" method="post" className={styles.menuForm}>
            <button type="submit" role="menuitem" className={styles.menuItem}>
              <svg
                width="15"
                height="15"
                viewBox="0 0 16 16"
                fill="none"
                aria-hidden="true"
              >
                <path
                  d="M6 14H3.5A1.5 1.5 0 0 1 2 12.5v-9A1.5 1.5 0 0 1 3.5 2H6M10.5 11 14 7.5 10.5 4M14 7.5H6"
                  stroke="currentColor"
                  strokeWidth="1.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
              Log out
            </button>
          </form>
        </div>
      ) : null}
    </div>
  );
}
