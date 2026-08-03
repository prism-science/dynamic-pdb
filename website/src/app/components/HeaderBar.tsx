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

export default function HeaderBar({ user }: { user: HeaderUser | null }) {
  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <Link href="/" className={styles.brand} aria-label="Dynamic PDB home">
          <img
            className={styles.brandLogo}
            src="/dynamic-pdb-mark.svg"
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
            />
          ) : (
            <LoginButton />
          )}
        </div>
      </div>
    </header>
  );
}
