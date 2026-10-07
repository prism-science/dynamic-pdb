"use client";

import Link from "next/link";
import { useRef } from "react";

import ChevronIcon from "./ChevronIcon";
import DocsNav from "./DocsNav";
import type { DocsPage } from "./pages";

import styles from "./docs.module.css";

/**
 * Sticky strip under the site header. It names the current page and, where
 * the left rail is not shown, holds the page list as a dropdown.
 */
export default function DocsBar({
  page,
  menuAlways = false,
}: {
  page: DocsPage | null;
  menuAlways?: boolean;
}) {
  const menu = useRef<HTMLDetailsElement>(null);

  return (
    <div className={styles.barOuter} data-docs-bar>
      <div className={styles.bar}>
        <details
          key={page?.slug ?? "home"}
          ref={menu}
          className={`${styles.pagesMenu} ${menuAlways ? styles.pagesMenuAlways : ""}`}
          onClick={(event) => {
            if ((event.target as HTMLElement).closest("a")) menu.current?.removeAttribute("open");
          }}
        >
          <summary className={styles.pagesMenuSummary}>
            Pages <ChevronIcon size={10} />
          </summary>
          <nav className={styles.pagesMenuPanel} aria-label="Docs pages">
            <DocsNav current={page?.slug ?? null} />
          </nav>
        </details>

        <nav className={styles.crumbs} aria-label="Breadcrumb">
          <Link href="/docs" className={styles.crumbLink}>
            About
          </Link>
          {page ? (
            <>
              <span className={styles.crumbSep} aria-hidden="true">
                /
              </span>
              <span className={styles.crumbCurrent} aria-current="page">
                {page.title}
              </span>
            </>
          ) : null}
        </nav>
      </div>
    </div>
  );
}
