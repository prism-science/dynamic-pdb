"use client";

import type { ResidueDetail, ResidueFact } from "@/lib/residue-detail";

import styles from "./ResidueInspector.module.css";

/**
 * One residue, in full, under the board.
 *
 * The rows answer "where along the chain"; this answers "what is this
 * residue" -- the question a reader has as soon as a peak makes them point at
 * one. It sits under the board rather than beside it because the board is the
 * thing that scrolls sideways, and a column taken off its right would be taken
 * off the part of the page that is already short of room.
 *
 * Every group says where its values came from. An empty field is printed
 * empty, with the reason underneath, rather than left out: a panel that
 * changes shape from residue to residue cannot be read, and "this file does
 * not record it" must not look like "we do not show it".
 */
export default function ResidueInspector({
  detail,
  held,
  onClose,
}: {
  detail: ResidueDetail;
  /** The range the click is holding, when it is wider than this one residue. */
  held?: { start: number; end: number } | null;
  onClose: () => void;
}) {
  return (
    <section className={styles.inspector} aria-label="Selected residue">
      <header className={styles.head}>
        <span className={styles.title} aria-live="polite">
          {detail.title}
        </span>
        {/* No badges beside the name: the chain is chosen directly above the
            board, and everything else a badge could say is a row below. */}
        {held ? (
          <span className={styles.held}>
            {`holding ${held.start}–${held.end}`}
          </span>
        ) : null}
        <span className={styles.end}>
          <span className={styles.keys}>&larr; &rarr; next residue</span>
          <button
            type="button"
            className={styles.close}
            onClick={onClose}
            aria-label="Clear the selection"
          >
            &times;
          </button>
        </span>
      </header>

      {/* A residue this model left out has nothing under the heading, and
          the heading is the answer -- so there is no body at all rather than
          an empty bordered box under the name. */}
      {detail.groups.length > 0 ? (
        <div className={styles.body}>
          {detail.groups.map((group) => (
            <div key={group.key} className={styles.group}>
              <div className={styles.groupHead}>{group.title}</div>
              {group.facts.map((fact) => (
                <Fact key={fact.key} fact={fact} />
              ))}
            </div>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function Fact({ fact }: { fact: ResidueFact }) {
  return (
    // A note qualifies the value rather than explaining it, so it sits on the
    // tooltip: four columns of qualifications would bury the numbers.
    <div className={styles.fact} title={fact.note}>
      {/* The schema's own name for the field sits on the tooltip rather than
          on the page: a depositor querying the API needs that exact string,
          and nobody reading a protein should have to work out that
          `pdbx_pdb_ins_code` means this residue has an insertion code. */}
      <span className={styles.factLabel} title={fact.field}>
        {fact.label}
      </span>
      <span className={styles.factValue}>{fact.value}</span>
    </div>
  );
}
