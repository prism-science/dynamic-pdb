import type { RelatedDataset } from "@/lib/api/entries";
import {
  landingUrl,
  relatedDatasetKey,
  relatedDatasetName,
  relatedDatasetSummary,
} from "@/lib/related-datasets";

import styles from "./RelatedDatasets.module.css";

/**
 * Datasets kept in other repositories, named by DOI or by the repository's
 * accession. Dynamic PDB holds no copy of them, so each is a link out, not a
 * file in the table beside it.
 */
export default function RelatedDatasets({ datasets }: { datasets: RelatedDataset[] }) {
  return (
    <ul className={styles.list}>
      {datasets.map((dataset) => {
        const name = relatedDatasetName(dataset);
        // Without a DOI the name already links to the landing page.
        const landing = dataset.doi ? landingUrl(dataset) : null;
        return (
          <li key={relatedDatasetKey(dataset)} className={styles.item}>
            {name.href ? (
              <a className={styles.doi} href={name.href} target="_blank" rel="noreferrer">
                {name.label}
              </a>
            ) : (
              <span className={styles.doi}>{name.label}</span>
            )}
            <span className={styles.summary}>{relatedDatasetSummary(dataset)}</span>
            {landing ? (
              <a className={styles.summary} href={landing} target="_blank" rel="noreferrer">
                Landing page
              </a>
            ) : null}
            {dataset.details ? <p className={styles.details}>{dataset.details}</p> : null}
          </li>
        );
      })}
    </ul>
  );
}
