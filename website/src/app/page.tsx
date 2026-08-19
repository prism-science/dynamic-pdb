import Link from "next/link";

import styles from "./landing.module.css";

/**
 * Everything on this page that counts something is a placeholder.
 *
 * The real figures are backend queries we have not built yet — totals per data
 * level, a group-by over L1 experiment types, a group-by over the software that
 * produced each L2 model, and the most recently updated entries. They are kept
 * together here, shaped the way the API will return them, so wiring them up is
 * a matter of deleting this block and passing the fetched values in.
 */
const PLACEHOLDER = {
  counts: {
    // L0: raw source datasets (HDF5 diffraction, raw images, cryo-EM particles).
    // Expected to be 0 for a while — today we only hold MTZs.
    rawSource: 0,
    // L1: MTZ reflections, CCP4 / MRC / DSN6 maps, NXS diffuse-scattering maps.
    processed: 1284,
    // L2: computed structural models.
    computedModels: 3912,
  },
  // Top 5 by count; the bar track is the L1 total, so the bars are comparable
  // to each other and show what share of the registry each experiment is.
  byExperiment: [
    { label: "X-ray", count: 1102 },
    { label: "CryoEM", count: 96 },
    { label: "Diffuse scattering", count: 54 },
    { label: "NMR", count: 22 },
    { label: "Other", count: 10 },
  ],
  // Top 5 by count; same idea against the L2 total.
  byAnalysis: [
    { label: "qFit3", count: 1640 },
    { label: "PHENIX", count: 1205 },
    { label: "Sampleworks", count: 712 },
    { label: "NMR", count: 210 },
    { label: "Other", count: 145 },
  ],
  latest: [
    { entry: "7APT", modelType: "Multiconformer", experiment: "X-ray", time: "01:43 PM 2026-08-17" },
    { entry: "6XQ1", modelType: "Ensemble", experiment: "CryoEM", time: "11:02 AM 2026-08-17" },
    { entry: "4KTU", modelType: "Multiconformer", experiment: "NMR", time: "09:18 AM 2026-08-16" },
    { entry: "3LZT", modelType: "Ensemble", experiment: "Diffuse scattering", time: "04:55 PM 2026-08-15" },
    { entry: "1TQN", modelType: "Multiconformer", experiment: "X-ray", time: "10:31 AM 2026-08-15" },
    { entry: "5NW3", modelType: "Ensemble", experiment: "Neutron", time: "02:07 PM 2026-08-14" },
  ],
};

// Shown under the search field as the kinds of thing that can be typed into it.
// Inert for now; each becomes a link to /browse?query=<term> once we wire them.
const SAMPLE_SEARCHES = [
  "2X68",
  "Cyp3",
  "qFit",
  "room temperature",
  "diffuse scattering",
  "Sampleworks",
];

const numberFormatter = new Intl.NumberFormat("en-US");

export default function Home() {
  const experimentTotal = sum(PLACEHOLDER.byExperiment);
  const analysisTotal = sum(PLACEHOLDER.byAnalysis);

  return (
    <main className={styles.page}>
      <section className={styles.hero}>
        <h1 className={styles.title}>The Dynamic PDB</h1>
        <p className={styles.tagline}>
          The Dynamic PDB is a living, open, registry for heterogeneity-aware
          structural biology: one experimental dataset is held fixed, and
          ensemble models are deposited, versioned, and validated against it.
        </p>

        {/* A plain GET form, so search works before any JavaScript arrives and
            lands on the same list the header search does. */}
        <form className={styles.search} action="/browse" method="get" role="search">
          <input
            className={styles.searchInput}
            type="search"
            name="query"
            placeholder="Entry ID, PDB ID, protein, ligand, software, or modeler…"
            aria-label="Search the registry"
          />
          <button type="submit" className={styles.searchButton}>
            Search
          </button>
        </form>

        <p className={styles.samples}>
          <span className={styles.samplesLead}>Try</span>
          {SAMPLE_SEARCHES.map((term) => (
            <span key={term} className={styles.sample}>
              {term}
            </span>
          ))}
        </p>
      </section>

      <section className={styles.counts} aria-label="Registry totals">
        <CountCard
          value={PLACEHOLDER.counts.rawSource}
          label="Raw Source Experimental Datasets"
        />
        <CountCard
          value={PLACEHOLDER.counts.processed}
          label="Processed Experimental Datasets"
        />
        <CountCard
          value={PLACEHOLDER.counts.computedModels}
          label="Computed Structural Models"
        />
      </section>

      <section className={styles.panels}>
        <Breakdown
          heading="Datasets By Experiment (L1)"
          rows={PLACEHOLDER.byExperiment}
          total={experimentTotal}
        />
        <Breakdown
          heading="Datasets By Analysis (L2)"
          rows={PLACEHOLDER.byAnalysis}
          total={analysisTotal}
        />
      </section>

      <section className={styles.panel}>
        <h2 className={styles.panelHeading}>Latest depositions</h2>
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Entry</th>
                <th scope="col">Model Type</th>
                <th scope="col">Experiment Type</th>
                <th scope="col" className={styles.tableTime}>
                  Time
                </th>
              </tr>
            </thead>
            <tbody>
              {PLACEHOLDER.latest.map((row) => (
                <tr key={row.entry}>
                  <td className={styles.tableEntry}>{row.entry}</td>
                  <td>{row.modelType}</td>
                  <td>{row.experiment}</td>
                  <td className={styles.tableTime}>{row.time}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <Link className={styles.browseAll} href="/browse">
          Browse all entries →
        </Link>
      </section>
    </main>
  );
}

function CountCard({ value, label }: { value: number; label: string }) {
  return (
    <div className={styles.countCard}>
      <div className={styles.countValue}>{numberFormatter.format(value)}</div>
      <div className={styles.countLabel}>{label}</div>
    </div>
  );
}

function Breakdown({
  heading,
  rows,
  total,
}: {
  heading: string;
  rows: { label: string; count: number }[];
  total: number;
}) {
  return (
    <div className={styles.panel}>
      <h2 className={styles.panelHeading}>{heading}</h2>
      <ul className={styles.breakdown}>
        {rows.map((row) => (
          <li key={row.label} className={styles.breakdownRow}>
            <div className={styles.breakdownHead}>
              <span className={styles.breakdownLabel}>{row.label}</span>
              <span className={styles.breakdownCount}>
                {numberFormatter.format(row.count)}
              </span>
            </div>
            {/* The track is the whole level, so a row reads as this
                experiment's share of it rather than as a bar against the
                largest row. */}
            <div className={styles.bar}>
              <span
                className={styles.barFill}
                style={{ width: `${total > 0 ? (row.count / total) * 100 : 0}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function sum(rows: { count: number }[]): number {
  return rows.reduce((total, row) => total + row.count, 0);
}
