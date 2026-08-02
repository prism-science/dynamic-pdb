"use client";

import { useState } from "react";

import type { Entity } from "@/lib/api/entries";
import FilePreviewModal from "./FilePreviewModal";
import ResolvedFileLink from "./ResolvedFileLink";
import styles from "./FileList.module.css";

export type FileItem = {
  id: string;
  name: string;
  type?: string;
  size?: number;
  url: string | null;
  // When present the row opens an in-place preview (sequence, structure,
  // image, ...) instead of only offering the raw download.
  entity?: Entity;
};

const PAGE_SIZE = 3;

export default function FileList({ items }: { items: FileItem[] }) {
  const [page, setPage] = useState(0);
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
      <ul className={styles.list}>
        {visible.map((item) => (
          <li key={item.id}>
            <FileRow item={item} onPreview={setPreview} />
          </li>
        ))}
      </ul>

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

      <FilePreviewModal entity={preview} onClose={() => setPreview(null)} />
    </div>
  );
}

function FileRow({
  item,
  onPreview,
}: {
  item: FileItem;
  onPreview: (entity: Entity) => void;
}) {
  const label = (
    <>
      <span className={styles.icon}>
        <FileIcon type={item.type} />
      </span>
      <span className={styles.meta}>
        <span className={styles.name}>{item.name}</span>
        <span className={styles.sub}>
          {[item.type?.toUpperCase(), formatSize(item.size)]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </span>
    </>
  );

  const download = item.url ? (
    <ResolvedFileLink
      className={styles.action}
      href={item.url}
      download
      rel="noreferrer"
      target="_blank"
      aria-label={`Download ${item.name}`}
      title="Download"
    >
      <DownloadIcon />
    </ResolvedFileLink>
  ) : null;

  // Previewable row: the body opens the modal, the icon still downloads.
  if (item.entity) {
    const entity = item.entity;
    return (
      <div className={styles.row} data-interactive="true">
        <button
          type="button"
          className={styles.rowMain}
          onClick={() => onPreview(entity)}
          title="Open preview"
        >
          {label}
        </button>
        {download}
      </div>
    );
  }

  if (item.url) {
    return (
      <ResolvedFileLink
        className={styles.row}
        href={item.url}
        rel="noreferrer"
        target="_blank"
      >
        {label}
        <span className={styles.action} aria-hidden="true">
          <DownloadIcon />
        </span>
      </ResolvedFileLink>
    );
  }
  return <div className={styles.row}>{label}</div>;
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
    width: 20,
    height: 20,
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
    >
      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
    </svg>
  );
}

function formatSize(size: number | undefined): string | null {
  if (typeof size !== "number" || size <= 0) {
    return null;
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? value : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}
