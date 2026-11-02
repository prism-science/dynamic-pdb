import type { RelatedDataset } from "@/lib/api/entries";
import { doiUrl, relatedDatasetSummary } from "@/lib/related-datasets";

import styles from "./RelatedDatasets.module.css";

/**
 * Datasets kept in other repositories, linked by DOI. Dynamic PDB holds no
 * copy of them, so each is a link out, not a file in the table beside it.
 */
export default function RelatedDatasets({ datasets }: { datasets: RelatedDataset[] }) {
  return (
    <ul className={styles.list}>
      {datasets.map((dataset) => (
        <li key={dataset.doi} className={styles.item}>
          <a
            className={styles.doi}
            href={doiUrl(dataset.doi)}
            target="_blank"
            rel="noreferrer"
          >
            {dataset.doi}
          </a>
          <span className={styles.summary}>{relatedDatasetSummary(dataset)}</span>
          {dataset.details ? <p className={styles.details}>{dataset.details}</p> : null}
        </li>
      ))}
    </ul>
  );
}
