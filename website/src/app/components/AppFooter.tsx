import styles from "./AppFooter.module.css";

/**
 * Site footer, on every page.
 *
 * The attribution names three organisations. None of them is linked yet: the
 * Prism site is not live, and the other two are left as plain text until we
 * agree on the URLs.
 */
export default function AppFooter() {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          className={styles.logo}
          src="/prism-mark-ink.png"
          alt="Prism"
          width={28}
          height={28}
        />
        <p className={styles.text}>
          The Dynamic PDB is a community resource that is supported by Prism, a
          program of Radial, a division of the Astera Institute.
        </p>
      </div>
    </footer>
  );
}
