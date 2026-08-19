import Link from "next/link";

import AppFooter from "./components/AppFooter";
import LandingSearch from "./components/LandingSearch";

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
    // Genuinely zero, not a placeholder — today we only hold MTZs — so this one
    // is printed exactly.
    rawSource: 0,
    // L1: MTZ reflections, CCP4 / MRC / DSN6 maps, NXS diffuse-scattering maps.
    processed: 225505,
    // L2: computed structural models.
    computedModels: 225505,
  },
  // Top 5 by count. `total` is the whole level, not the sum of the rows: the
  // bar track is the level, so each bar reads as that slice's share of the
  // registry rather than as a bar against the biggest row. The top 5 need not
  // add up to it — whatever is left is simply not shown.
  //
  // Still invented, though the shares are plausible and do add up to the L1
  // total.
  byExperiment: {
    total: 225505,
    rows: [
      { label: "X-ray", count: 208940 },
      { label: "CryoEM", count: 12180 },
      { label: "NMR", count: 3410 },
      { label: "Diffuse scattering", count: 610 },
      { label: "Other", count: 365 },
    ],
  },
  // These five are real, exported from the registry. They cover 219,785 of the
  // 225,505 L2 models; the remaining 5,720 sit outside the top 5 and are not
  // drawn, which is why the bars do not fill the row.
  byAnalysis: {
    total: 225505,
    rows: [
      { label: "PHENIX", count: 120125 },
      { label: "qFit", count: 59577 },
      { label: "REFMAC", count: 31616 },
      { label: "CNS", count: 5511 },
      { label: "BUSTER", count: 2956 },
    ],
  },
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
// One per kind of thing that can be searched: an entry id, a protein, the
// software that produced a model, a collection condition, a data type. Clicking
// one fills the search field; see LandingSearch.
const SAMPLE_SEARCHES = [
  "6T0K",
  "FKBP51",
  "qFit",
  "room temperature",
  "diffuse scattering",
];

const numberFormatter = new Intl.NumberFormat("en-US");

/**
 * Print a headline count as a round approximation.
 *
 * These totals come from a periodic snapshot, not a live query, so an exact
 * figure on a card this size would claim a precision we do not have. Rounding
 * is always *down* to a coarse step, which keeps the trailing "+" literally
 * true however stale the snapshot gets — the registry only grows. The steps are
 * deliberately blunt, one whole unit of the magnitude, so the figure reads as
 * an order of size rather than as a measurement. Anything under a thousand is
 * printed as-is: there is nothing to round, and a real zero should read as zero
 * rather than as an approximation.
 */
function approximateCount(value: number): string {
  if (value < 1000) {
    return numberFormatter.format(value);
  }
  const step = value >= 100000 ? 100000 : value >= 10000 ? 10000 : 1000;
  const rounded = Math.floor(value / step) * step;
  return `${numberFormatter.format(rounded / 1000)}K+`;
}

export default function Home() {
  return (
    // The footer lives here rather than in the root layout: as a second flex
    // item under `body` it competes with `.appContent`, which is allowed to
    // shrink (min-height: 0, needed by the full-height review layouts), and on
    // /browse that shrinking stopped the infinite scroll from ever reaching its
    // sentinel. Keeping it to this page sidesteps that until the app shell can
    // carry a footer properly.
    <>
      <main className={styles.page}>
        <section className={styles.hero}>
          <h1 className={styles.title}>The Dynamic PDB</h1>
          <p className={styles.tagline}>
            The Dynamic PDB is a living, open, registry for heterogeneity-aware
            structural biology: one experimental dataset is held fixed, and
            ensemble models are deposited, versioned, and validated against it.
          </p>

          {/* A plain GET form, so search still works before any JavaScript
              arrives and lands on the same list the header search does. */}
          <LandingSearch samples={SAMPLE_SEARCHES} />
        </section>

        <section aria-label="Registry totals">
          <div className={styles.counts}>
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
          </div>
          {/* Says once, quietly, what the rounded figures already imply, so the
              numbers do not have to carry the caveat themselves. */}
          <p className={styles.countsNote}>
            Totals from the latest snapshot, refreshed periodically.
          </p>
        </section>

        <section className={styles.panels}>
          <Breakdown
            heading="Datasets By Experiment (L1)"
            {...PLACEHOLDER.byExperiment}
          />
          <Breakdown
            heading="Datasets By Analysis (L2)"
            {...PLACEHOLDER.byAnalysis}
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

      <AppFooter />
    </>
  );
}

function CountCard({ value, label }: { value: number; label: string }) {
  return (
    <div className={styles.countCard}>
      <div className={styles.countValue}>{approximateCount(value)}</div>
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
