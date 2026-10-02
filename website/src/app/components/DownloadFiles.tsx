"use client";

import { useEffect, useRef, useState } from "react";

import type { DownloadGroup } from "@/lib/download-files";
import ResolvedFileLink from "./ResolvedFileLink";

import styles from "./DownloadFiles.module.css";

/**
 * The record's named files, behind one button.
 *
 * The Files tab lists everything; this is the short list a reader comes for --
 * the sequence, the coordinates, the measured data -- offered by name rather
 * than found by scanning a table. Which coordinates and which data depends on
 * the model selected in the rail, so the menu changes with it.
 */
export default function DownloadFiles({ groups }: { groups: DownloadGroup[] }) {
  const [open, setOpen] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);

  // A menu that stays open after the page has moved on under it is a menu the
  // reader has to dismiss twice, so it closes on anything that means "not this".
  useEffect(() => {
    if (!open) {
      return;
    }
    function onPointerDown(event: MouseEvent) {
      if (!wrapper.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  if (groups.length === 0) {
    return null;
  }

  return (
    <div className={styles.wrapper} ref={wrapper} data-tour="download">
      <button
        type="button"
        className={styles.button}
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-haspopup="menu"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
        </svg>
        Download Files
        <span className={styles.caret} aria-hidden="true" />
      </button>

      {open ? (
        <div className={styles.menu} role="menu">
          {groups.map((group) => (
            <div key={group.key} className={styles.group}>
              {group.files.map((entry) => (
                <ResolvedFileLink
                  key={entry.id}
                  className={styles.item}
                  href={entry.href}
                  download
                  rel="noreferrer"
                  target="_blank"
                  role="menuitem"
                  onClick={() => setOpen(false)}
                >
                  {entry.label}
                </ResolvedFileLink>
              ))}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
