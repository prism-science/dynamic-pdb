import styles from "./EntryVersions.module.css";

/** One line of the entry's version history. */
export type EntryVersion = {
  key: string;
  number: string;
  /** As deposited: an ISO date, shown as written so it sorts and reads the
   *  same way it does in the archive. */
  date: string;
  /** Why this version exists -- "Initial release", "Structure summary". */
  reason: string;
  /** What actually changed in it. Empty until the record can say. */
  change: string | null;
};

/**
 * A placeholder history: one version, the same one for every entry.
 *
 * The record keeps revisions -- there is a `model_revisions` table and a review
 * queue built on it -- but nothing on the API answers "what changed in this
 * entry and when", so there is nothing to read yet. The table is here in its
 * final shape so the tab can be looked at and argued about; when the API can
 * answer, this constant goes and the rows arrive as a prop.
 */
const PLACEHOLDER: EntryVersion[] = [
  { key: "1.0", number: "1.0", date: "2026-08-11", reason: "Initial sync", change: null },
];

/**
 * Version history, laid out as RCSB's entry page lays it out: one row per
 * version, and the four columns they carry -- what the version is, when it
 * appeared, why, and what it changed.
 */
export default function EntryVersions() {
  return (
    <section className={styles.box} aria-label="Version history">
      <table className={styles.table}>
        <thead>
          <tr>
            <th scope="col">Version Number</th>
            <th scope="col">Version Date</th>
            <th scope="col">Version Type/Reason</th>
            <th scope="col">Version Change</th>
          </tr>
        </thead>
        <tbody>
          {PLACEHOLDER.map((version) => (
            <tr key={version.key}>
              <th scope="row" className={styles.number}>
                {version.number}
              </th>
              <td className={styles.date}>{version.date}</td>
              <td>{version.reason}</td>
              <td>{version.change}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
