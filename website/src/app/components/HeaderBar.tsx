"use client";

import { Suspense } from "react";
import Link from "next/link";

import styles from "./AppHeader.module.css";
import HeaderSearch from "./HeaderSearch";
import LoginButton from "./LoginButton";
import UserMenu from "./UserMenu";

export type HeaderUser = {
  displayName: string;
  subLabel: string | null;
  initial: string;
};

export type HeaderReviews = {
  isReviewer: boolean;
  /** Something is waiting. Deliberately not a count: see reviews/count.ts. */
  hasWork: boolean;
};

type Props = {
  user: HeaderUser | null;
  reviews: HeaderReviews | null;
};

export default function HeaderBar({ user, reviews }: Props) {
  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <Link href="/" className={styles.brand} aria-label="Dynamic PDB home">
          <img
            className={styles.brandLogo}
            src="/prism-mark-white.png"
            alt=""
            width={32}
            height={32}
          />
          <span className={styles.brandText}>Dynamic PDB</span>
        </Link>

        <div className={styles.actions}>
          {/* useSearchParams needs a boundary so the rest of the header is not
              pulled out of static rendering with it. */}
          <Suspense fallback={<div className={styles.search} />}>
            <HeaderSearch />
          </Suspense>

          {user ? (
            <UserMenu
              displayName={user.displayName}
              subLabel={user.subLabel}
              initial={user.initial}
              isReviewer={reviews?.isReviewer ?? false}
              hasWork={reviews?.hasWork ?? false}
            />
          ) : (
            <LoginButton />
          )}
        </div>
      </div>
    </header>
  );
}
