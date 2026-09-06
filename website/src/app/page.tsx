import Link from "next/link";

import {
  ApiRequestError,
  getEntry,
  listModels,
  type Entry,
  type Model,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import { formatEntryLabel } from "@/lib/entry-label";
import AppFooter from "./components/AppFooter";
import LandingSearch from "./components/LandingSearch";

import styles from "./landing.module.css";

/**
 * Everything on this page that counts something is a placeholder.
 *
 * The real figures are backend queries we have not built yet — totals per data
 * level, a group-by over L1 experiment types, and a group-by over the software
 * that produced each L2 model. They are kept together here, shaped the way the
 * API will return them, so wiring them up is a matter of deleting this block
 * and passing the fetched values in.
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
      // `query` is what the row searches for, kept separate from the label so a
      // shortened label can still search the full term. "X-ray" matches the
      // stored method "X-ray crystallography": the index is a tsvector, and the
      // parser splits the hyphenated word, so the shorter term is a prefix of
      // the same token set rather than a different string.
      { label: "X-ray", count: 225477, query: "X-ray" },
      // An aggregate of everything outside the top rows: there is no term that
      // means it, so the row does not link. Search cannot answer "the rows I
      // did not draw", and a link that lands on an empty list is worse than a
      // row that never offered.
      { label: "Other", count: 27, query: null },
    ],
  },
  // Real too. These five cover 219,785 of the 225,505 L2 models; the remaining
  // 5,720 sit outside the top 5 and are not drawn, which is why the bars do not
  // fill the row.
  //
  // Each label is also its search term. Note that these searches come back
  // empty today: entry_search_index covers entry and model revision metadata,
  // and the software that produced a model is a program node in the run graph,
  // which is not indexed. Wiring the rows is this page's half of the feature —
  // indexing program names is a separate backend change.
  byAnalysis: {
    total: 225505,
    rows: [
      { label: "PHENIX", count: 120125, query: "PHENIX" },
      { label: "qFit", count: 59577, query: "qFit" },
      { label: "REFMAC", count: 31616, query: "REFMAC" },
      { label: "CNS", count: 5511, query: "CNS" },
      { label: "BUSTER", count: 2956, query: "BUSTER" },
    ],
  },
};

// Shown under the search field as the kinds of thing that can be typed into it.
// One per kind: an entry id, a protein, the software that produced a model, a
// collection condition, a data type. Each is a link that runs its own search;
// see LandingSearch.
//
// The last three answer only by accident today. The index covers entry and
// model revision metadata: a program name lives on a node in the run graph and
// is not indexed at all, a collection condition has no field to live in, and
// "diffuse scattering" is not one of the two values the method enum holds. Any
// of them can still hit if the words happen to sit in an entry's title or
// description. They stay as they are: the row is a claim about what the field
// is for, and the index is the side that has to catch up.
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

type LatestDeposition = {
  entry: string;
  entryId: string;
  modelId: string;
  modelType: string;
  experiment: string;
  updatedAt: string;
};

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

export default async function Home() {
  const latest = await loadLatestDepositions();

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
                {latest.length > 0 ? (
                  latest.map((row) => (
                    // The whole row opens the model it describes, but it holds
                    // one link, not four: the anchor below is stretched over the
                    // row in CSS. Four cells wrapped in four copies of the same
                    // href would read the same destination four times to a
                    // screen reader and give the keyboard four stops to get past
                    // one row.
                    <tr key={row.modelId} className={styles.tableRow}>
                      <td className={styles.tableEntry}>
                        {/* The id is the link's text because it is the row's
                            name, but it is not dressed as a link: the row is the
                            target, and underlining one cell inside it would say
                            the click has to land there. The label says the entry
                            and the destination is a model inside it, so the
                            accessible name spells that out. */}
                        <Link
                          className={styles.rowLink}
                          href={`/entries/${row.entryId}/models/${row.modelId}`}
                          aria-label={`${row.entry} — ${row.modelType.toLowerCase()} model`}
                        >
                          {row.entry}
                        </Link>
                      </td>
                      <td>{row.modelType}</td>
                      <td>{row.experiment}</td>
                      <td className={styles.tableTime}>
                        {formatDepositedAt(row.updatedAt)}
                      </td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td colSpan={4}>No depositions yet.</td>
                  </tr>
                )}
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

async function loadLatestDepositions(): Promise<LatestDeposition[]> {
  const session = await getAuthSession();
  try {
    const models = await listModels(session?.token, { limit: 5, offset: 0 });
    const entries = await Promise.all(
      models.map((model) => getEntry(session?.token, model.entry_id)),
    );
    return models.map((model, index) =>
      latestDepositionFromModel(model, entries[index]),
    );
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}

function latestDepositionFromModel(
  model: Model,
  entry: Entry,
): LatestDeposition {
  return {
    entry: formatEntryLabel(entry),
    entryId: model.entry_id,
    modelId: model.id,
    modelType:
      stringMetadataValue(model.metadata ?? {}, "model_type") ?? "Unknown",
    experiment: experimentLabel(
      stringMetadataValue(entry.metadata ?? {}, "method"),
    ),
    updatedAt: model.updated_at,
  };
}

function experimentLabel(method: string | null): string {
  if (method === "X-ray crystallography") {
    return "X-ray";
  }
  return method ?? "Other";
}

function stringMetadataValue(
  metadata: Record<string, unknown>,
  key: string,
): string | null {
  const value = metadata[key];
  return typeof value === "string" && value.trim() ? value.trim() : null;
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

type BreakdownRow = {
  label: string;
  count: number;
  /** What the row searches for, or null when the row is not a search term. */
  query: string | null;
};

function Breakdown({
  heading,
  rows,
  total,
}: {
  heading: string;
  rows: BreakdownRow[];
  total: number;
}) {
  return (
    <div className={styles.panel}>
      <h2 className={styles.panelHeading}>{heading}</h2>
      <ul className={styles.breakdown}>
        {rows.map((row) => (
          <li key={row.label} className={styles.breakdownRow}>
            <BreakdownBody row={row} total={total} />
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * A row's contents, wrapped in a link to the same list the search field lands
 * on when the row names something searchable.
 *
 * The whole row is the target — label, count and bar — rather than the label
 * alone: the count and the bar are the same fact said twice more, so making
 * only the words clickable would leave most of the row inert for no reason.
 * A row with no term (see `query` in the data above) is rendered as the plain
 * markup it was before, so nothing looks clickable that is not.
 */
function BreakdownBody({ row, total }: { row: BreakdownRow; total: number }) {
  const body = (
    <>
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
    </>
  );

  if (!row.query) {
    return body;
  }

  return (
    <Link
      className={styles.breakdownLink}
      href={`/browse?query=${encodeURIComponent(row.query)}`}
    >
      {body}
    </Link>
  );
}
