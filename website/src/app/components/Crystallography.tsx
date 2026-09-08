import {
  type CrystallographyView,
  formatKelvin,
  formatPH,
} from "@/lib/crystallography";

import styles from "./Crystallography.module.css";

/**
 * How the crystals were grown and what was collected from them.
 *
 * A row is a diffraction dataset rather than a crystal: datasets are what
 * differ from one another -- a structure is routinely built from two of them
 * off the same crystal -- and the crystal's own conditions repeat down its
 * rows, the same way the organism repeats in the entity table.
 */
export default function Crystallography({
  view,
}: {
  view: CrystallographyView;
}) {
  return (
    <div className={styles.wrap}>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">Dataset</th>
              <th scope="col">Crystal</th>
              <th scope="col" className={styles.numHead}>
                pH
              </th>
              <th scope="col" className={styles.numHead}>
                Grown at
              </th>
              <th scope="col" className={styles.numHead}>
                Collected at
              </th>
            </tr>
          </thead>
          <tbody>
            {view.datasets.map((dataset) => (
              <tr key={dataset.key}>
                <td className={styles.idCell}>
                  {dataset.datasetId !== null ? (
                    <span className={styles.datasetId}>{dataset.datasetId}</span>
                  ) : (
                    <span className={styles.absent}>—</span>
                  )}
                </td>
                <td className={styles.crystalCell}>Crystal {dataset.crystalId}</td>
                <td className={styles.numCell}>
                  {dataset.ph !== null ? (
                    formatPH(dataset.ph)
                  ) : (
                    <span className={styles.absent}>—</span>
                  )}
                </td>
                <td className={styles.numCell}>
                  {dataset.growthKelvin !== null ? (
                    formatKelvin(dataset.growthKelvin)
                  ) : (
                    <span className={styles.absent}>—</span>
                  )}
                </td>
                <td className={styles.numCell}>
                  {dataset.collectionKelvin !== null ? (
                    formatKelvin(dataset.collectionKelvin)
                  ) : (
                    <span className={styles.absent}>—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
