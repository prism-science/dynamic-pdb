import styles from "./docs.module.css";

export default function TourButton() {
  return (
    <p className={styles.tourRow}>
      <a
        className={styles.tourButton}
        href="/docs/tour"
        target="_blank"
        rel="noreferrer"
      >
        Take the tour
      </a>
      <span className={styles.tourHint}>
        Opens a sample entry in a new tab and walks through each part of the page.
      </span>
    </p>
  );
}
