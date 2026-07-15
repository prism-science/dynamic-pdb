import styles from "./page.module.css";

export default function Home() {
  return (
    <main className={styles.page}>
      <section className={styles.hero}>
        <img
          className={styles.mark}
          src="/dynamic-pdb-mark.svg"
          alt=""
          aria-hidden="true"
        />
        <p className={styles.kicker}>dynamic-pdb</p>
        <h1>Protein structures, ready to move.</h1>
        <p className={styles.copy}>
          A minimal home for the project while the backend and product surface
          come together.
        </p>
      </section>
    </main>
  );
}
