"use client";

import dynamic from "next/dynamic";

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
 * No density maps are handed to it: the Layers bar stays off this tab for now.
 * Passing none is what hides it -- the bar draws itself only when there is a
 * layer to list.
 */
export default function StructurePanel({
  url,
  kind,
  square,
}: {
  url: string;
  kind: StructureKind;
  /** Square the canvas off, for a tab where it is the page rather than a card. */
  square?: boolean;
}) {
  return <StructureViewer url={url} kind={kind} square={square} />;
}
