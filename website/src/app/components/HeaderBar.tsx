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
 * All three have a page behind them now, so the branch that rendered a pending
 * item as plain text is gone along with its style. Git has it if a fifth item
 * ever arrives ahead of its page.
 */
const NAV = [
  { label: "Browse", href: "/browse" },
  { label: "Download", href: "/download" },
  // The docs live under /docs but are presented as About.
  { label: "About", href: "/docs" },
] as const;

function isCurrent(pathname: string, href: string): boolean {
  return pathname.startsWith(href);
}

export default function HeaderBar({ user, reviews }: Props) {
  const pathname = usePathname();
  // The landing page carries its own centred search, so the header does not
  // repeat it there.
  const isLanding = pathname === "/";
  // Signed out there is nothing to deposit into, and on the form itself the
  // button would only reload the page the author is already filling in — and
  // take their typing with it.
  const canDeposit = user != null && pathname !== "/entries/new";

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
                  isCurrent(pathname, item.href) ? "page" : undefined
                }
              >
                {item.label}
              </Link>
            ))}
          </nav>
        </div>

        <div className={styles.actions}>
          {canDeposit ? (
            <Link
              className={styles.newEntry}
              href="/entries/new"
              aria-label="New entry"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
                aria-hidden="true"
              >
                <path d="M12 5v14M5 12h14" />
              </svg>
              {/* Dropped on a narrow bar; the plus carries it from there. */}
              <span className={styles.newEntryLabel}>New entry</span>
            </Link>
          ) : null}

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
