import AppFooter from "../components/AppFooter";

import styles from "./download.module.css";

/**
 * Where bulk download will be explained, and what to do until it is.
 *
 * Two sentences, because the page is asked two things: is there a bulk
 * download — no — and how do I get data then. It promises nothing about a
 * mechanism that does not exist, and offers no service over the address: that
 * line is for questions, not for requesting a copy by mail.
 */
export default function Download() {
  return (
    <>
      <main className={styles.page}>
        <h1 className={styles.title}>Download</h1>

        <div className={styles.prose}>
          <p className={styles.lead}>Bulk download is not available yet.</p>
          {/* No full stop after the address: a period against a mailto link
              reads as part of it, and the sentence is the last one anyway. */}
          <p className={styles.note}>
            Individual files can be downloaded from any entry page. For
            questions, contact{" "}
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
