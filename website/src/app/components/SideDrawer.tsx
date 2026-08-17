"use client";

import { useEffect } from "react";
import Link from "next/link";

import type { HeaderReviews } from "./HeaderBar";
import styles from "./SideDrawer.module.css";

type Props = {
  open: boolean;
  onClose: () => void;
  reviews: HeaderReviews | null;
};

export default function SideDrawer({ open, onClose, reviews }: Props) {
  useEffect(() => {
    if (!open) {
      return;
    }
    function handleKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open, onClose]);

  const isReviewer = reviews?.isReviewer ?? false;
  const toReviewCount = reviews?.toReviewCount ?? 0;

  return (
    <>
      <div
        className={`${styles.overlay} ${open ? styles.overlayOpen : ""}`}
        onClick={onClose}
        aria-hidden={!open}
      />
      <nav
        className={`${styles.drawer} ${open ? styles.drawerOpen : ""}`}
        aria-label="Main menu"
        aria-hidden={!open}
      >
        <div className={styles.group}>Browse</div>
        <Link href="/entries" className={styles.item} onClick={onClose}>
          Entries
        </Link>

        <div className={styles.spacer} />

        {isReviewer ? (
          <div className={styles.adminWrap}>
            <div className={styles.group}>Admin</div>
            <Link href="/review" className={styles.item} onClick={onClose}>
              To review
              {toReviewCount > 0 ? (
                <span className={styles.count}>{toReviewCount}</span>
              ) : null}
            </Link>
            <div className={styles.role}>
              <span className={styles.dot} /> Reviewer
            </div>
          </div>
        ) : null}
      </nav>
    </>
  );
}
