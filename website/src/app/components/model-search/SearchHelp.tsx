import styles from "./model-search.module.css";

/**
 * How the model search reads its filters, in the words GET /v1/models
 * documents them. Missing values get the most room: a range never matches a
 * record without the value, which is the result people find surprising.
 */
export default function SearchHelp({ open = false }: { open?: boolean }) {
  return (
    <details className={styles.help} open={open}>
      <summary className={styles.helpSummary}>How search works</summary>
      <ul className={styles.helpList}>
        <li>Each result is one model, shown with its entry.</li>
        <li>
          Different filters must all match. Several values of one filter mean any of them.
        </li>
        <li>
          Entry filters look at the model&apos;s entry. Model and score filters look at that same
          model, never at another model of the entry.
        </li>
        <li>
          <strong>Missing values never match.</strong> An entry without a resolution never matches
          a resolution range, an entry whose Ligands and Cofactors are unknown never matches that
          filter, and a model without an R-free never matches an R-free range. Use &ldquo;No value
          for&rdquo; under Scores to find models missing a score.
        </li>
        <li>
          Compounds the file reader marked &ldquo;needs review&rdquo; still match, and the result
          names them.
        </li>
        <li>
          Records flagged for review stay in the results, with a &ldquo;Flagged for review&rdquo;
          badge. Reviewers and admins also see which issues, and can filter by flags.
        </li>
        <li>Counts are exact up to 10,000; above that they show as 10,000+.</li>
        <li>
          The API link under the count runs the same search, for scripts. Its address uses the same
          filter names as this page&apos;s.
        </li>
      </ul>
    </details>
  );
}
