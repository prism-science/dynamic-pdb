import styles from "./AppFooter.module.css";

/**
 * Site footer.
 *
 * The design's attribution sentence, kept word for word, but broken across two
 * lines at its own hinge: what the registry is and who backs it, then the chain
 * of organisations behind that backer. In one flat line the reader met three
 * organisations at once and could not tell which of them runs the thing; split
 * this way the sentence keeps its meaning and gains a shape.
 *
 * Radial and the Astera Institute are linked. Prism is not: prismscience.org
 * does not resolve yet — the design notes as much — and a footer link that dies
 * is worse than a name in plain text. One href away when the site goes up.
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
        {/* One sentence, one paragraph: the break is presentational, so the
            first half is a block-level span rather than a second <p>. */}
        <p className={styles.copy}>
          <span className={styles.lead}>
            The Dynamic PDB is a community resource that is supported by{" "}
            <span className={styles.org}>Prism</span>,
          </span>
          <span className={styles.tail}>
            a program of{" "}
            <a className={styles.link} href="https://radial.org">
              Radial
            </a>
            , a division of the{" "}
            <a className={styles.link} href="https://astera.org">
              Astera Institute
            </a>
            .
          </span>
        </p>
      </div>
    </footer>
  );
}
