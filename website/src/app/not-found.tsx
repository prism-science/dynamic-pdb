import Link from "next/link";

import AppFooter from "./components/AppFooter";

import styles from "./not-found.module.css";

/**
 * 404, in the site's own frame.
 *
 * Next ships a default for this, but it renders in its own styles — centred
 * black-on-white, or white-on-black wherever the reader's system asks for a
 * dark theme — under our light header, which reads as the site having broken
 * rather than the address being wrong. This is the same page frame as About,
 * so a mistyped entry ID lands somewhere that still looks like the registry.
 *
 * Two ways out, both of them somewhere with content: the catalog, which is
 * where a wrong entry ID was trying to go, and the landing page.
 */
export default function NotFound() {
  return (
    <>
      <main className={styles.page}>
        {/* Mono and muted: the status code is a detail for whoever is
            debugging the link, not the message. */}
        <p className={styles.code}>404</p>

        <h1 className={styles.title}>This page does not exist</h1>

        <p className={styles.note}>
          The address may be mistyped, or the entry it pointed at may have been
          withdrawn.
        </p>

        <p className={styles.links}>
          <Link className={styles.link} href="/browse">
            Browse entries
          </Link>
          <span className={styles.sep} aria-hidden="true">
            ·
          </span>
          <Link className={styles.link} href="/">
            Go home
          </Link>
        </p>
      </main>

      <AppFooter />
    </>
  );
}
