"use client";

import dynamic from "next/dynamic";
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import type { HetstarDpdbConfig } from "@/lib/hetstar";

import styles from "./HetstarPanel.module.css";

// Mol* needs window and WebGL, so there is no server render of this worth
// having, and the bundle is large enough that it should not ship to readers of
// the other tabs.
const HetstarViewer = dynamic(
  () => import("@dynamic-pdb/hetstar").then((m) => m.HetstarViewer),
  {
    ssr: false,
    loading: () => <p className={styles.loading}>Loading the viewer…</p>,
  },
);

/**
 * The viewer's own tray, the row of squares in the top-right corner of the
 * canvas. The button we add lives in it, which means reaching into markup the
 * package does not expose.
 *
 * `.hetstar` is the wrapper the component renders; the tray is the absolutely
 * positioned box in its corner, and the row of buttons is that box's last child
 * (a bookmark row appears before it once anything is bookmarked). These are
 * hetstar's own Tailwind utilities, not hashed module classes, so they are
 * readable as a selector -- and they are also the part most likely to be
 * rewritten upstream, which is what the fallback below is for.
 */
const TRAY = ".hetstar .absolute.right-2.top-2";

/**
 * Copied from hetstar's own TrayButton so the button is the same object as the
 * three beside it rather than an approximation of one. Its stylesheet is loaded
 * for the whole site and every utility in it is scoped under `.hetstar`, so
 * these class names only resolve because the button is portalled inside that
 * wrapper.
 */
const TRAY_BUTTON =
  "flex h-6 w-6 items-center justify-center rounded border transition-colors";
const TRAY_BUTTON_IDLE =
  "border-line-strong bg-white/85 text-ink-secondary hover:bg-line";
const TRAY_BUTTON_ACTIVE = "border-accent bg-accent-soft text-accent";

/** Four corners pushing out, or the same arrows pulling back in. */
function ExpandIcon({ exit }: { exit: boolean }) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {exit ? (
        <>
          <path d="M5 1v4H1" />
          <path d="M7 11V7h4" />
        </>
      ) : (
        <>
          <path d="M1 5V1h4" />
          <path d="M11 7v4H7" />
        </>
      )}
    </svg>
  );
}

/**
 * The entry's structural heterogeneity, on the Structure tab.
 *
 * Unlike the viewer it replaced, this one is handed an entry id rather than a
 * model's coordinates: it resolves the entry against the catalogue itself, pairs
 * the qFit model against the deposited one, and downloads both plus the structure
 * factors from the browser. So the server does not prepare anything for it, and
 * the model the rail has selected does not reach it -- the pairing is the
 * viewer's own.
 *
 * The full-screen control is ours, but it is rendered into the viewer's own tray
 * through a portal, as the last square in that row. hetstar takes no prop for
 * this and we are not editing the submodule, so the alternative was a button of
 * our own floating over its corner -- which would have had to guess the tray's
 * width to sit beside it, and guess again whenever that changed. In the row, the
 * flex gap and the alignment are the tray's problem rather than ours.
 *
 * What goes full screen is the shell, so the button rides along inside the
 * viewer and the way back out is always on screen.
 */
export default function HetstarPanel({
  entryId,
  dpdb,
}: {
  entryId: string;
  dpdb: HetstarDpdbConfig;
}) {
  const shellRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLDivElement>(null);

  // Real fullscreen, owned by the browser: it is the authority on this, so the
  // flag is only ever set from its own event rather than optimistically on click.
  const [isFullscreen, setIsFullscreen] = useState(false);
  // The consolation prize where the Fullscreen API is refused or absent (iOS
  // Safari will not give it for a plain element): a fixed box over the viewport.
  const [isOverlay, setIsOverlay] = useState(false);
  const expanded = isFullscreen || isOverlay;

  // Where the button goes, once the viewer has drawn its tray. null until then,
  // and again if hetstar ever rebuilds that corner.
  const [tray, setTray] = useState<HTMLElement | null>(null);
  // Set once we have waited long enough to believe the tray is not coming, so
  // that the fallback does not flash past while the viewer is still mounting.
  const [trayMissing, setTrayMissing] = useState(false);

  useEffect(() => {
    const sync = () =>
      setIsFullscreen(document.fullscreenElement === shellRef.current);
    document.addEventListener("fullscreenchange", sync);
    return () => document.removeEventListener("fullscreenchange", sync);
  }, []);

  // Find the tray, and keep finding it. The viewer arrives well after this
  // component (dynamic import, then Mol* starting up), and its corner is
  // rebuilt when the entry changes, so a one-shot lookup would miss both.
  const trayRef = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;

    const look = () => {
      // The viewer's DOM churns constantly -- canvas, hover readouts, lanes --
      // and this runs on every mutation, so the common case has to be one
      // containment check rather than a fresh query.
      if (trayRef.current && frame.contains(trayRef.current)) return;
      const row = frame.querySelector(TRAY)?.lastElementChild ?? null;
      const next = row instanceof HTMLElement ? row : null;
      if (next === trayRef.current) return;
      trayRef.current = next;
      setTray(next);
    };

    look();
    const observer = new MutationObserver(look);
    observer.observe(frame, { childList: true, subtree: true });
    const giveUp = window.setTimeout(() => setTrayMissing(true), 4000);
    return () => {
      observer.disconnect();
      window.clearTimeout(giveUp);
    };
  }, []);

  // The overlay has to reimplement what the browser does for free in real
  // fullscreen: escape closes it, and the page underneath stops scrolling.
  useEffect(() => {
    if (!isOverlay) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setIsOverlay(false);
    };
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
    };
  }, [isOverlay]);

  const toggle = useCallback(async () => {
    const shell = shellRef.current;
    if (!shell) return;
    if (isFullscreen) {
      await document.exitFullscreen().catch(() => undefined);
      return;
    }
    if (isOverlay) {
      setIsOverlay(false);
      return;
    }
    if (typeof shell.requestFullscreen !== "function") {
      setIsOverlay(true);
      return;
    }
    try {
      await shell.requestFullscreen();
    } catch {
      // Refused rather than missing -- a permissions policy on an embed, say.
      // Either way the button should still do something.
      setIsOverlay(true);
    }
  }, [isFullscreen, isOverlay]);

  const label = expanded ? "Leave full screen (Esc)" : "Show full screen";

  const trayButton = (
    <button
      type="button"
      aria-label={label}
      aria-pressed={expanded}
      title={label}
      onClick={toggle}
      className={`${TRAY_BUTTON} ${expanded ? TRAY_BUTTON_ACTIVE : TRAY_BUTTON_IDLE}`}
      // Last in the row whatever order we were inserted in: the row is a flex
      // container, and hetstar appending its own buttons later would otherwise
      // put them after ours.
      style={{ order: 1 }}
    >
      <ExpandIcon exit={expanded} />
    </button>
  );

  return (
    <div
      ref={shellRef}
      className={styles.shell}
      data-overlay={isOverlay || undefined}
    >
      {/* Only if the tray never turned up -- hetstar's corner rewritten, or the
          viewer failed to mount at all. Better a button in the wrong place than
          no way out of a page that has no scrollbar left. */}
      {tray === null && trayMissing && (
        <div className={styles.bar}>
          <button type="button" className={styles.expand} onClick={toggle}>
            <ExpandIcon exit={expanded} />
            {expanded ? "Exit full screen" : "Full screen"}
          </button>
        </div>
      )}
      <div ref={frameRef} className={styles.frame}>
        <HetstarViewer entryId={entryId} dpdb={dpdb} />
      </div>
      {tray !== null && createPortal(trayButton, tray)}
    </div>
  );
}
