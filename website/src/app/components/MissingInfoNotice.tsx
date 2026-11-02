import styles from "./MissingInfoNotice.module.css";

export const MISSING_INFO_TOOLTIP =
  "Some details now required for computed models are not on file for this record.";

/**
 * The "information missing" label of an old record (researcher answer Q5). The
 * record stays public; the label says which details a write would now ask for.
 * Closed by default, so it costs one line on the many pages that carry it.
 * Renders nothing when nothing is missing.
 */
export default function MissingInfoNotice({ lines }: { lines: readonly string[] }) {
  if (lines.length === 0) {
    return null;
  }
  return (
    <details className={styles.notice}>
      <summary className={styles.summary}>
        <span className={styles.badge} title={MISSING_INFO_TOOLTIP}>
          Information missing
        </span>
        <span className={styles.toggle}>What is missing</span>
      </summary>
      <p className={styles.lead}>Added before these details were required:</p>
      <ul className={styles.lines}>
        {lines.map((line, index) => (
          <li key={`${index}-${line}`}>{line}</li>
        ))}
      </ul>
      <p className={styles.note}>The record stays public. An editor can add the missing details.</p>
    </details>
  );
}
