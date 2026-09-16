"use client";

import dynamic from "next/dynamic";

import type { HetstarDpdbConfig } from "@/lib/hetstar";

import styles from "./HetstarPanel.module.css";

// Mol* needs window and WebGL, so there is no server render of this worth
// having, and the bundle is large enough that it should not ship to readers of
// the other tabs.
const HetstarViewer = dynamic(
  () => import("@dynamic-pdb/hetstar").then((m) => m.HetstarViewer),
  {
    ssr: false,
    loading: () => <p className={styles.loading}>Loading the viewer\u2026</p>,
  },
);

/**
 * The entry's structural heterogeneity, on the Structure tab.
 *
 * Unlike the viewer it replaced, this one is handed an entry id rather than a
 * model's coordinates: it resolves the entry against the catalogue itself, pairs
 * the qFit model against the deposited one, and downloads both plus the structure
 * factors from the browser. So the server does not prepare anything for it, and
 * the model the rail has selected does not reach it -- the pairing is the
 * viewer's own.
 */
export default function HetstarPanel({
  entryId,
  dpdb,
}: {
  entryId: string;
  dpdb: HetstarDpdbConfig;
}) {
  return (
    <div className={styles.frame}>
      <HetstarViewer entryId={entryId} dpdb={dpdb} />
    </div>
  );
}
