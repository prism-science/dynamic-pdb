"use client";

import { useState } from "react";

import type { Entity } from "@/lib/api/entries";
import {
  type FileItem,
  formatFileSize,
  formatLabel,
  structureMaps,
} from "@/lib/entities";
import FileDetailsModal from "./FileDetailsModal";
import FilePreviewModal from "./FilePreviewModal";
import ResolvedFileLink from "./ResolvedFileLink";

import styles from "./FileTable.module.css";

export type { FileItem };

// Everything a file records is a column, so the section is a table rather than
// a list of cards with a caption underneath: four values line up down the page
// and can be compared between rows, which is most of what anyone does with a
// size or a level.
//
// Eight rows rather than the three the old list paged at — a deposit of four
// raw files should not arrive split across two pages.
const PAGE_SIZE = 8;

export default function FileTable({ items }: { items: FileItem[] }) {
  const [page, setPage] = useState(0);
  const [details, setDetails] = useState<FileItem | null>(null);
  const [preview, setPreview] = useState<Entity | null>(null);

  const pageCount = Math.max(1, Math.ceil(items.length / PAGE_SIZE));
  const current = Math.min(page, pageCount - 1);
  const start = current * PAGE_SIZE;
  const visible = items.slice(start, start + PAGE_SIZE);

  if (items.length === 0) {
    return null;
  }

  return (
    <div className={styles.wrap}>
      {/* The table keeps its columns and scrolls sideways inside its own frame
          rather than collapsing them, so a narrow window loses no values. */}
      <div className={styles.scroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Level</th>
              <th scope="col">Format</th>
              <th scope="col" className={styles.numHead}>
                Size
              </th>
              <th scope="col">SHA-256</th>
              <th scope="col">
                <span className={styles.srOnly}>Download</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {visible.map((item) => (
              <FileRow key={item.id} item={item} onOpen={setDetails} />
            ))}
          </tbody>
        </table>
      </div>

      {items.length > PAGE_SIZE ? (
        <div className={styles.pager}>
          <span className={styles.range}>
            {start + 1}–{start + visible.length} of {items.length}
          </span>
          <div className={styles.pagerButtons}>
            <button
              type="button"
              className={styles.pageBtn}
              onClick={() => setPage(current - 1)}
              disabled={current === 0}
              aria-label="Previous page"
            >
              Prev
            </button>
            <span className={styles.pageIndicator}>
              {current + 1} / {pageCount}
            </span>
            <button
              type="button"
              className={styles.pageBtn}
              onClick={() => setPage(current + 1)}
              disabled={current >= pageCount - 1}
              aria-label="Next page"
            >
              Next
            </button>
          </div>
        </div>
      ) : null}

      {/* The details window steps aside while the viewer is up and comes back
          when it closes, so "look inside" is a detour rather than a dead end. */}
      <FileDetailsModal
        item={preview ? null : details}
        onClose={() => setDetails(null)}
        onPreview={setPreview}
      />

      <FilePreviewModal
        entity={preview}
        maps={structureMaps(
          items
            .map((item) => item.entity)
            .filter((entity): entity is Entity => entity != null),
        )}
        onClose={() => setPreview(null)}
      />
    </div>
  );
}

function FileRow({
  item,
  onOpen,
}: {
  item: FileItem;
  onOpen: (item: FileItem) => void;
}) {
  const format = formatLabel(item.type);
  const size = formatFileSize(item.size);

  return (
    <tr
      className={styles.row}
      // The whole row is the target, but the name is a real button so the row
      // is reachable by keyboard; the two controls inside stop the bubble so a
      // copy or a download does not also open the dialog.
      onClick={() => onOpen(item)}
    >
      <td className={styles.nameCell}>
        <button
          type="button"
          className={styles.nameBtn}
          onClick={(event) => {
            event.stopPropagation();
            onOpen(item);
          }}
          title="Open file details"
        >
          <span className={styles.icon}>
            <FileIcon type={item.type} />
          </span>
          <span className={styles.name}>{item.name}</span>
        </button>
      </td>
      <td className={styles.valueCell}>
        {item.level ? (
          <span className={styles.level} data-level={item.level}>
            {item.level}
          </span>
        ) : (
          <span className={styles.absent}>—</span>
        )}
      </td>
      <td className={`${styles.valueCell} ${styles.format}`}>
        {format ?? <span className={styles.absent}>—</span>}
      </td>
      <td className={`${styles.valueCell} ${styles.size}`}>
        {size ?? <span className={styles.absent}>—</span>}
      </td>
      <td className={styles.valueCell}>
        <HashCell sha256={item.sha256} />
      </td>
      <td className={styles.actionCell}>
        {item.url ? (
          <ResolvedFileLink
            className={styles.download}
            href={item.url}
            download
            rel="noreferrer"
            target="_blank"
            aria-label={`Download ${item.name}`}
            title="Download"
            onClick={(event) => event.stopPropagation()}
          >
            <DownloadIcon />
          </ResolvedFileLink>
        ) : null}
      </td>
    </tr>
  );
}

// Eight hex characters is what a cell has room for and what a person compares
// by eye; the button hands over all sixty-four, and the dialog prints them.
function HashCell({ sha256 }: { sha256?: string | null }) {
  const [copied, setCopied] = useState(false);
  const value =
    typeof sha256 === "string" && sha256.trim() !== ""
      ? sha256.trim().toLowerCase()
      : null;

  if (!value) {
    return <span className={styles.absent}>—</span>;
  }

  async function copy(digest: string) {
    try {
      await navigator.clipboard.writeText(digest);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button
      type="button"
      className={styles.hash}
      title={copied ? "Copied" : "Copy the full digest"}
      onClick={(event) => {
        event.stopPropagation();
        void copy(value);
      }}
    >
      <span className={styles.hashValue}>{value.slice(0, 8)}</span>
      {copied ? <CheckIcon /> : <CopyIcon />}
    </button>
  );
}

type IconKind = "structure" | "image" | "text" | "file";

function iconKind(type: string | undefined): IconKind {
  const t = (type ?? "").toLowerCase();
  if (/(pdb|cif|mmcif|h5|hdf5|cbf|3d|structure|diffraction|trajectory)/.test(t)) {
    return "structure";
  }
  if (/(png|jpg|jpeg|tif|tiff|mrc|ccp4|map|image|micrograph|density)/.test(t)) {
    return "image";
  }
  if (/(log|txt|text|json|yaml|yml|csv|config|param)/.test(t)) {
    return "text";
  }
  return "file";
}

function FileIcon({ type }: { type: string | undefined }) {
  const kind = iconKind(type);
  const common = {
    width: 18,
    height: 18,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.5,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true,
  };

  if (kind === "structure") {
    return (
      <svg {...common}>
        <path d="M12 3 4 7.5v9L12 21l8-4.5v-9L12 3Z" />
        <path d="m4 7.5 8 4.5 8-4.5M12 12v9" />
      </svg>
    );
  }
  if (kind === "image") {
    return (
      <svg {...common}>
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <path d="m3 16 5-5 4 4 3-3 6 6" />
        <circle cx="9" cy="9" r="1.4" fill="currentColor" stroke="none" />
      </svg>
    );
  }
  if (kind === "text") {
    return (
      <svg {...common}>
        <path d="M6 3h8l4 4v14H6z" />
        <path d="M14 3v4h4M9 12h6M9 16h6" />
      </svg>
    );
  }
  return (
    <svg {...common}>
      <path d="M6 3h8l4 4v14H6z" />
      <path d="M14 3v4h4" />
    </svg>
  );
}

function CopyIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m4 12.5 5.5 5.5L20 7" />
    </svg>
  );
}

function DownloadIcon() {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
    </svg>
  );
}
