import Link from "next/link";

import { docsGroups, docsHref } from "./pages";

import styles from "./docs.module.css";

export default function DocsNav({ current }: { current: string | null }) {
  return (
    <>
      {docsGroups().map(({ group, pages }) => (
        <div key={group} className={styles.navGroup}>
          <p className={styles.railLabel}>{group}</p>
          {pages.map((page) => (
            <Link
              key={page.slug}
              href={docsHref(page)}
              aria-current={page.slug === current ? "page" : undefined}
              className={`${styles.navLink} ${
                page.slug === current ? styles.navLinkOn : ""
              }`}
            >
              {page.title}
            </Link>
          ))}
        </div>
      ))}
    </>
  );
}
