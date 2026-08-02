"use client";

import { useMemo, useState } from "react";

import type { FastaMetadata } from "@/lib/api/structures";

import styles from "./SequenceView.module.css";

const GROUP = 10;
const GROUPS_PER_LINE = 5;

export default function SequenceView({ metadata }: { metadata: FastaMetadata }) {
  const [copied, setCopied] = useState(false);

  const sequence = (
    typeof metadata.sequence === "string" ? metadata.sequence : ""
  )
    .replace(/\s+/g, "")
    .toUpperCase();

  const lines = useMemo(() => {
    const groups = sequence.match(new RegExp(`.{1,${GROUP}}`, "g")) ?? [];
    const result: string[] = [];
    for (let i = 0; i < groups.length; i += GROUPS_PER_LINE) {
      result.push(groups.slice(i, i + GROUPS_PER_LINE).join(" "));
    }
    return result;
  }, [sequence]);

  async function copy() {
    if (!sequence) {
      return;
    }
    try {
      await navigator.clipboard.writeText(sequence);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  if (!sequence) {
    return null;
  }

  return (
    <div className={styles.wrap}>
      <button type="button" className={styles.copy} onClick={copy}>
        {copied ? "Copied" : "Copy"}
      </button>
      <pre className={styles.seq}>
        {lines.map((line, index) => (
          <div className={styles.line} key={index}>
            {line}
          </div>
        ))}
      </pre>
    </div>
  );
}
