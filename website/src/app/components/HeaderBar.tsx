"use client";

import Link from "next/link";

import styles from "./AppHeader.module.css";
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
        <Link href="/" className={styles.brand} aria-label="dynamic-pdb home">
          <img
            className={styles.brandLogo}
            src="/dynamic-pdb-mark.svg"
            alt=""
            width={32}
            height={32}
          />
          <span className={styles.brandText}>dynamic-pdb</span>
        </Link>

        <div className={styles.actions}>
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
