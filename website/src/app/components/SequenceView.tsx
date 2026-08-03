"use client";

import { useMemo, useState } from "react";

import type { FastaMetadata } from "@/lib/api/entries";
import { FASTA_LINE, fastaRecords } from "@/lib/fasta";

import styles from "./SequenceView.module.css";

// Residues in blocks of ten, sixty to a line, as every sequence database prints
// them.
const GROUP = 10;
const PER_LINE = FASTA_LINE;

type Chain = {
  fields: string[];
  header: string;
  sequence: string;
  lines: string[];
};

type Parsed = {
  chains: Chain[];
  // Which defline field switches records. The switcher is rendered in that
  // field's own place so the header keeps the order the file wrote it in.
  switchColumn: number | null;
};

export default function SequenceView({ metadata }: { metadata: FastaMetadata }) {
  const { chains, switchColumn } = useMemo(() => parse(metadata), [metadata]);
  const [activeIndex, setActiveIndex] = useState(0);

  if (chains.length === 0) {
    return null;
  }

  const index = Math.min(activeIndex, chains.length - 1);
  const chain = chains[index];
  const switchable = chains.length > 1;

  function optionLabel(item: Chain, position: number): string {
    if (switchColumn === null) {
      return `Sequence ${position + 1}`;
    }
    return item.fields[switchColumn] ?? `Sequence ${position + 1}`;
  }

  const switcher = switchable ? (
    <span className={styles.switch}>
      <span>{optionLabel(chain, index)}</span>
      <ChevronIcon />
      <select
        className={styles.switchSelect}
        aria-label="Sequence"
        value={index}
        onChange={(event) => setActiveIndex(Number(event.target.value))}
      >
        {chains.map((item, position) => (
          <option key={position} value={position}>
            {optionLabel(item, position)}
          </option>
        ))}
      </select>
    </span>
  ) : null;

  return (
    <div className={styles.wrap}>
      <div className={styles.defline}>
        {/* No switch column but several records: the file offers nothing to
            hang the control on, so it goes in front of the defline. */}
        {switcher && switchColumn === null ? (
          <>
            {switcher}
            {chain.fields.length > 0 ? (
              <span className={styles.separator}>|</span>
            ) : null}
          </>
        ) : null}

        {chain.fields.map((field, position) => (
          <span key={position}>
            {position > 0 ? <span className={styles.separator}>|</span> : null}
            {switcher && position === switchColumn ? switcher : field}
          </span>
        ))}
      </div>

      <div className={styles.body}>
        {chain.lines.map((line, position) => (
          <div className={styles.line} key={position}>
            {line}
          </div>
        ))}
      </div>
    </div>
  );
}

// Deflines inside one FASTA file are written by the same producer, so their
// pipe-separated fields line up into columns:
//
//   4HHB_1|Chains A,C|Hemoglobin subunit alpha|Homo sapiens
//   4HHB_2|Chains B,D|Hemoglobin subunit beta|Homo sapiens
//
// Nothing is parsed out of the text and nothing is dropped: the defline is
// shown as written, and one of its fields doubles as the record switcher.
function parse(metadata: FastaMetadata): Parsed {
  const records = fastaRecords(metadata);
  const headers = records.map((record) => record.header.replace(/^>/, "").trim());
  const rows = headers.map(splitHeader);

  return {
    chains: records.map((record, index) => ({
      fields: rows[index],
      header: headers[index],
      sequence: record.sequence,
      lines: toLines(record.sequence),
    })),
    switchColumn: records.length > 1 ? findSwitchColumn(rows, records.length) : null,
  };
}

function splitHeader(header: string): string[] {
  return header
    .split("|")
    .map((field) => field.trim())
    .filter(Boolean);
}

// A column naming chains wins outright. Otherwise take the first column that
// tells the records apart — an accession does the job when chains are not
// spelled out. A column that repeats itself would give every option the same
// label, so it is no use.
function findSwitchColumn(rows: string[][], count: number): number | null {
  const width = Math.max(0, ...rows.map((row) => row.length));

  for (let column = 0; column < width; column += 1) {
    if (rows.every((row) => /\bchains?\b/i.test(row[column] ?? ""))) {
      return column;
    }
  }

  for (let column = 0; column < width; column += 1) {
    const values = rows.map((row) => row[column] ?? "");
    if (values.every(Boolean) && new Set(values).size === count) {
      return column;
    }
  }

  return null;
}

function toLines(sequence: string): string[] {
  const lines: string[] = [];
  for (let offset = 0; offset < sequence.length; offset += PER_LINE) {
    const slice = sequence.slice(offset, offset + PER_LINE);
    const groups: string[] = [];
    for (let position = 0; position < slice.length; position += GROUP) {
      groups.push(slice.slice(position, position + GROUP));
    }
    lines.push(groups.join(" "));
  }
  return lines;
}

function ChevronIcon() {
  return (
    <svg
      className={styles.chevron}
      width="9"
      height="9"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}
