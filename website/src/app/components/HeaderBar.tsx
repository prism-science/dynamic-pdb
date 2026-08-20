"use client";

import { Suspense } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";

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

/**
 * Main navigation, left aligned on every page.
 *
 * All four have a page behind them now, so the branch that rendered a pending
 * item as plain text is gone along with its style. Git has it if a fifth item
 * ever arrives ahead of its page.
 */
const NAV = [
  { label: "Browse", href: "/browse" },
  { label: "Download", href: "/download" },
  { label: "Docs", href: "/docs" },
  { label: "About", href: "/about" },
] as const;

export default function HeaderBar({ user, reviews }: Props) {
  const pathname = usePathname();
  // The landing page carries its own centred search, so the header does not
  // repeat it there.
  const isLanding = pathname === "/";

  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <div className={styles.left}>
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

          <nav className={styles.nav} aria-label="Main">
            {NAV.map((item) => (
              <Link
                key={item.label}
                className={styles.navLink}
                href={item.href}
                aria-current={
                  pathname.startsWith(item.href) ? "page" : undefined
                }
              >
                {item.label}
              </Link>
            ))}
          </nav>
        </div>

        <div className={styles.actions}>
          {/* useSearchParams needs a boundary so the rest of the header is not
              pulled out of static rendering with it. */}
          {isLanding ? null : (
            <Suspense fallback={<div className={styles.search} />}>
              <HeaderSearch />
            </Suspense>
          )}

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
