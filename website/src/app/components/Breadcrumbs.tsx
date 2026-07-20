import Link from "next/link";
import { Fragment } from "react";

import styles from "./Breadcrumbs.module.css";

export type Crumb = {
  label: string;
  href?: string;
};

export default function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className={styles.breadcrumbs} aria-label="Breadcrumb">
      {items.map((item, index) => {
        const isLast = index === items.length - 1;
        return (
          <Fragment key={`${item.label}-${index}`}>
            {item.href && !isLast ? (
              <Link className={styles.link} href={item.href}>
                {item.label}
              </Link>
            ) : (
              <span
                className={isLast ? styles.current : styles.link}
                aria-current={isLast ? "page" : undefined}
              >
                {item.label}
              </span>
            )}
            {!isLast ? (
              <span className={styles.sep} aria-hidden="true">
                /
              </span>
            ) : null}
          </Fragment>
        );
      })}
    </nav>
  );
}
