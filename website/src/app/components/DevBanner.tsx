import styles from "./DevBanner.module.css";

export default function DevBanner() {
  return (
    <aside className={styles.banner} aria-label="Site notice">
      The Dynamic PDB is under active development and{" "}
      <strong>is not accepting external submissions at this time</strong>.
    </aside>
  );
}
