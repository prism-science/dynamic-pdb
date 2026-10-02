import Link from "next/link";

import ChevronIcon from "./ChevronIcon";
import { docsHref, neighbors } from "./pages";

import styles from "./docs.module.css";

export default function PrevNext({ slug }: { slug: string }) {
  const { previous, next } = neighbors(slug);

  return (
    <nav className={styles.prevNext} aria-label="Previous and next pages">
      {previous ? (
        <Link href={docsHref(previous)} className={styles.prevNextCard} rel="prev">
          <span className={styles.prevNextLabel}>
            <ChevronIcon direction="left" size={10} /> Previous
          </span>
          <span className={styles.prevNextTitle}>{previous.title}</span>
        </Link>
      ) : (
        <span />
      )}
      {next ? (
        <Link
          href={docsHref(next)}
          className={`${styles.prevNextCard} ${styles.prevNextCardNext}`}
          rel="next"
        >
          <span className={styles.prevNextLabel}>
            Next <ChevronIcon direction="right" size={10} />
          </span>
          <span className={styles.prevNextTitle}>{next.title}</span>
        </Link>
      ) : null}
    </nav>
  );
}
