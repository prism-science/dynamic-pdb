"use client";

import dynamic from "next/dynamic";
import { useEffect } from "react";

import { track } from "@/lib/analytics";
import type { OverlayModel } from "@/lib/model-overlays";
import type { StructureKind } from "@/lib/structureKind";

import styles from "./StructurePanel.module.css";

// Mol* is a large client-only bundle and there is no server render of a canvas
// worth having, so the tab loads it on arrival rather than shipping it to
// every reader of every other tab.
const StructureViewer = dynamic(() => import("./StructureViewer"), {
  ssr: false,
  loading: () => <p className={styles.loading}>Loading the viewer…</p>,
});

/**
 * The selected model's coordinates, in the viewer, on their own tab.
 *
 * Same component the file preview opens in a dialog -- a structure is worth a
 * whole page, and a reader who came to look at it should not have to open
 * something first.
 *
 * This is what the Structure tab held before the heterogeneity viewer, and it
 * is what the tab falls back to now: hetstar reads mmCIF only, and plenty of
 * this catalogue's coordinates are PDB. `lib/hetstar-support.ts` decides which
 * of the two opens. It draws one model rather than a qFit/deposited pair, and
 * it has no density or per-residue metrics -- but it follows the rail, which on
 * a PDB model is the only way to see that model at all.
 *
 * No density maps are handed to it: the Layers bar stays off this tab for now.
 * Passing none is what hides it -- the bar draws itself only when there is a
 * layer to list.
 *
 * The entry's other models are handed to it, though. Comparing models is what
 * this record is for, and on this tab the comparison is a thing you look at
 * rather than a table you read, so it belongs in the viewer itself rather than
 * on a page of its own.
 */
export default function StructurePanel({
  url,
  kind,
  overlays,
  overlaysSkipped,
  baseColor,
  square,
}: {
  url: string;
  kind: StructureKind;
  /** The entry's other models, for laying over this one. */
  overlays?: OverlayModel[];
  /** Other models that exist but have no coordinates the viewer can draw. */
  overlaysSkipped?: number;
  /** This model's own colour, used once a comparison is running. */
  baseColor?: number;
  /** Square the canvas off, for a tab where it is the page rather than a card. */
  square?: boolean;
}) {
  useEffect(() => {
    track("viewer-open", { viewer: "molstar" });
  }, [url]);

  return (
    <StructureViewer
      url={url}
      kind={kind}
      overlays={overlays}
      overlaysSkipped={overlaysSkipped}
      baseColor={baseColor}
      square={square}
    />
  );
}
