"use client";

import { useMemo, useState } from "react";

import type { FastaMetadata } from "@/lib/api/entries";
import { fastaRecords, fastaText } from "@/lib/fasta";

import styles from "./SequenceView.module.css";

const GROUP = 10;
const GROUPS_PER_LINE = 5;

export default function SequenceView({ metadata }: { metadata: FastaMetadata }) {
  const [copied, setCopied] = useState(false);

  const records = useMemo(
    () =>
      fastaRecords(metadata).map((record) => ({
        ...record,
        lines: sequenceLines(record.sequence),
      })),
    [metadata],
  );

  async function copy() {
    const text = fastaText(metadata);
    if (!text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  if (records.length === 0) {
    return null;
  }

  return (
    <div className={styles.wrap}>
      <button type="button" className={styles.copy} onClick={copy}>
        {copied ? "Copied" : "Copy"}
      </button>
      <div className={styles.seq}>
        {records.map((record, recordIndex) => (
          <div className={styles.record} key={`${record.header}-${recordIndex}`}>
            {record.header ? (
              <div className={styles.header}>&gt;{record.header}</div>
            ) : null}
            {record.lines.map((line, lineIndex) => (
              <div className={styles.line} key={lineIndex}>
                {line}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function sequenceLines(sequence: string): string[] {
  const groups = sequence.match(new RegExp(`.{1,${GROUP}}`, "g")) ?? [];
  const lines: string[] = [];
  for (let index = 0; index < groups.length; index += GROUPS_PER_LINE) {
    lines.push(groups.slice(index, index + GROUPS_PER_LINE).join(" "));
  }
  return lines;
}
