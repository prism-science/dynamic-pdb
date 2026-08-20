import AppFooter from "../components/AppFooter";

import styles from "./about.module.css";

/**
 * What the registry is, who runs it, and where to write.
 *
 * A placeholder in the sense that the words are the design's own and nothing
 * else has been invented to sit around them: no history, no team, no roadmap.
 * The page exists because the header has always listed About, and a nav item
 * with no page behind it is the thing worth removing first.
 *
 * The design gives the copy as one block. It is set as two paragraphs, split
 * where the subject changes — what the registry is, then what state it is in
 * and how to reach the people running it. The words are unchanged; only the
 * line break is ours.
 */
export default function About() {
  return (
    <>
      <main className={styles.page}>
        {/* Named rather than "About": the nav item already said that word, and
            the heading is what a tab, a bookmark and a search result show. It
            repeats the opening of the paragraph below only because that
            paragraph is the design's placeholder — real copy will not. */}
        <h1 className={styles.title}>About the Dynamic PDB</h1>

        <div className={styles.prose}>
          <p className={styles.lead}>
            The Dynamic PDB is a living, open, registry for heterogeneity-aware
            structural biology: the experimental dataset is held fixed, and
            ensemble models are deposited, versioned, and validated against it.
          </p>
          {/* No full stop after the address: a period against a mailto link
              reads as part of it, and the sentence is the last one anyway. */}
          <p className={styles.note}>
            This initiative is under active development. For questions or
            comments, please contact{" "}
            <a className={styles.email} href="mailto:prism-core@astera.org">
              prism-core@astera.org
            </a>
          </p>
        </div>
      </main>

      <AppFooter />
    </>
  );
}
