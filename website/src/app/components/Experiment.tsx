import type { ReactNode } from "react";

import type {
  ExperimentBlock,
  ExperimentColumns,
  ExperimentDatasets,
  ExperimentFacts,
  ExperimentRows,
  ExperimentView,
} from "@/lib/experiment";
import Crystallography from "./Crystallography";

import styles from "./Experiment.module.css";

/**
 * The Experiment tab: what was crystallised, how it was measured, and how the
 * selected model was refined against it.
 *
 * The order and the headings are RCSB's own experiment page -- crystal, then
 * diffraction, then what was refined from it -- so that a reader who knows
 * that page finds the same table here under the same name. The view decides
 * the order; this only draws the four shapes a block can have.
 *
 * Everything below the crystal comes from the selected model's own coordinate
 * file, so the refinement figures, the deviations and the software change with
 * the rail while the crystal and the cell do not.
 */
export default function Experiment({ view }: { view: ExperimentView }) {
  return (
    <div className={styles.grid}>
      {view.blocks.map((block) => (
        <Block key={block.key} block={block} />
      ))}
    </div>
  );
}

function Block({ block }: { block: ExperimentBlock }) {
  switch (block.kind) {
    case "facts":
      return <Facts block={block} />;
    case "columns":
      return <Columns block={block} />;
    case "rows":
      return <Rows block={block} />;
    case "datasets":
      return <Datasets block={block} />;
  }
}

function Facts({ block }: { block: ExperimentFacts }) {
  return (
    <Box title={block.title}>
      <dl className={styles.facts}>
        {block.facts.map((item) => (
          <div className={styles.row} key={item.label}>
            <dt>{item.label}</dt>
            <dd
              data-mono={item.mono ? "true" : undefined}
              data-wrap={item.wrap ? "true" : undefined}
            >
              {item.value}
            </dd>
          </div>
        ))}
      </dl>
    </Box>
  );
}

function Columns({ block }: { block: ExperimentColumns }) {
  return (
    <Box title={block.title} wide>
      <table className={styles.table}>
        <thead>
          <tr>
            <th scope="col">Statistic</th>
            {block.columns.map((column) => (
              <th scope="col" className={styles.num} key={column}>
                {column}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {block.rows.map((row) => (
            <tr key={row.key}>
              <th scope="row">{row.label}</th>
              <Cell value={row.overall} />
              <Cell value={row.shell} />
            </tr>
          ))}
        </tbody>
      </table>
    </Box>
  );
}

function Rows({ block }: { block: ExperimentRows }) {
  return (
    <Box title={block.title}>
      <table className={styles.table}>
        <thead>
          <tr>
            {block.head.map((column, index) => (
              <th
                scope="col"
                key={column}
                className={block.numeric[index] ? styles.num : undefined}
              >
                {column}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {block.rows.map((row) => (
            <tr key={row.key}>
              {/* The first cell names the row, so it is a header cell: a
                  screen reader should say it with each value. */}
              <th scope="row">{row.cells[0]}</th>
              {row.cells.slice(1).map((value, index) => (
                <Cell
                  key={index}
                  value={value}
                  numeric={block.numeric[index + 1]}
                />
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Box>
  );
}

function Datasets({ block }: { block: ExperimentDatasets }) {
  return (
    <Box title={block.title} wide nested>
      <Crystallography view={block.view} />
    </Box>
  );
}

function Box({
  title,
  wide,
  nested,
  children,
}: {
  title: string;
  /** Takes the full width of the grid rather than half of it. */
  wide?: boolean;
  /** Holds a component that draws its own frame. */
  nested?: boolean;
  children: ReactNode;
}) {
  const classes = [styles.box, wide ? styles.wide : "", nested ? styles.nested : ""];
  return (
    <section className={classes.filter(Boolean).join(" ")}>
      <header className={styles.band}>{title}</header>
      {children}
    </section>
  );
}

/** A value the file records in one column but not the other: the row is worth
 *  drawing for the column that has it, and the gap is stated rather than left
 *  blank. */
function Cell({
  value,
  numeric = true,
}: {
  value: string | null;
  numeric?: boolean;
}) {
  const className = numeric ? styles.num : undefined;
  if (value === null) {
    return (
      <td className={[className, styles.absent].filter(Boolean).join(" ")}>
        <span aria-label="not recorded">—</span>
      </td>
    );
  }
  return <td className={className}>{value}</td>;
}
