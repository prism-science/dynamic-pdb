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
  // Top rows by count. `total` is the whole level, not the sum of the rows: the
  // bar track is the level, so each bar reads as that slice's share of the
  // registry rather than as a bar against the biggest row. The rows need not add
  // up to it — whatever falls outside the top is simply not drawn.
  //
  // Real, exported from the registry. The export also carried a third row,
  // neutron scattering with a count of 1, which is left out: a row for a single
  // dataset says less than the space it takes. That is the one model missing
  // from the 225,505.
  byExperiment: {
    total: 225505,
    rows: [
      { label: "X-ray", count: 225477 },
      { label: "Other", count: 27 },
    ],
  },
  // Real too. These five cover 219,785 of the 225,505 L2 models; the remaining
  // 5,720 sit outside the top 5 and are not drawn, which is why the bars do not
  // fill the row.
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
  // Real, most recently updated first, five of them — the cut belongs to the
  // query this will become, so there is nothing here to slice at render time.
  // `entryId` is carried but unused: it is where the Entry column will link once
  // we wire the table up, so that change touches only the markup.
  //
  // The export's `model_name` is dropped — every row of it reads "Ensemble
  // refinement model", which the Model Type column already says.
  latest: [
    { entry: "5RGD", entryId: "d777266b-6d64-4848-bdb3-9ffd1a4db39a", modelType: "Ensemble", experiment: "X-ray", updatedAt: "2026-08-13T14:44:21.787Z" },
    { entry: "6ZBX", entryId: "059f1a59-41c1-42b1-86b3-0dd99a046cd3", modelType: "Ensemble", experiment: "X-ray", updatedAt: "2026-08-13T14:44:21.207Z" },
    { entry: "8H2V", entryId: "2bb891d8-5a34-442f-b470-fe5b8d4ec35d", modelType: "Ensemble", experiment: "Other", updatedAt: "2026-08-13T14:44:19.235Z" },
    { entry: "7ATM", entryId: "8b68663f-5d6f-4108-9ce5-33122959ea70", modelType: "Ensemble", experiment: "X-ray", updatedAt: "2026-08-13T14:44:19.195Z" },
    { entry: "7CCW", entryId: "d9dcd7a6-58a8-4cfa-bde7-697a935818bb", modelType: "Ensemble", experiment: "X-ray", updatedAt: "2026-08-13T14:44:18.789Z" },
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
 * Deposit timestamps, pinned to UTC and printed to the second.
 *
 * UTC because the string is produced during the server render: formatting in
 * whatever zone the server happens to run in would make the page say different
 * things on different machines. The column header carries the UTC so the reader
 * is not left guessing.
 *
 * To the second because a bulk import lands its models inside the same minute —
 * these ten span eight seconds — and a minute-resolution column would print ten
 * identical times, which makes the ordering look arbitrary.
 */
const depositTime = new Intl.DateTimeFormat("en-US", {
  timeZone: "UTC",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: true,
});

// en-CA for the YYYY-MM-DD the design asks for.
const depositDate = new Intl.DateTimeFormat("en-CA", {
  timeZone: "UTC",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

function formatDepositedAt(iso: string): string {
  const at = new Date(iso);
  return `${depositTime.format(at)} ${depositDate.format(at)}`;
}

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
                    Time (UTC)
                  </th>
                </tr>
              </thead>
              <tbody>
                {PLACEHOLDER.latest.map((row) => (
                  <tr key={row.entry}>
                    <td className={styles.tableEntry}>{row.entry}</td>
                    <td>{row.modelType}</td>
                    <td>{row.experiment}</td>
                    <td className={styles.tableTime}>
                      {formatDepositedAt(row.updatedAt)}
                    </td>
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

function barWidth(count: number, total: number): string {
  if (count <= 0 || total <= 0) {
    return "0";
  }
  return `max(3px, ${(count / total) * 100}%)`;
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
            {/* A share this lopsided — one row is 99.99% of the level, the
                next is 0.01% — rounds to less than a pixel, and an empty track
                reads as zero rather than as "very few". Anything non-zero keeps
                a hairline; the exact count is right above it either way. */}
            <div className={styles.bar}>
              <span
                className={styles.barFill}
                style={{ width: barWidth(row.count, total) }}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
