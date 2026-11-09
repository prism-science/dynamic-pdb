"use client";

import { useState } from "react";

import styles from "./model-search.module.css";

/** Copies the API URL of the search, for a script or a terminal. */
export default function CopyLinkButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button type="button" className={styles.copyButton} onClick={copy} aria-label="Copy the API URL">
      {copied ? "Copied" : "Copy"}
    </button>
  );
}
