"use client";

import { useState } from "react";

import styles from "./FileList.module.css";

export type FileItem = {
  id: string;
  name: string;
  type?: string;
  size?: number;
  url: string | null;
};

const PAGE_SIZE = 3;

export default function FileList({ items }: { items: FileItem[] }) {
  const [page, setPage] = useState(0);

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
            <FileRow item={item} />
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
    </div>
  );
}

function FileRow({ item }: { item: FileItem }) {
  const inner = (
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
      {item.url ? (
        <span className={styles.action} aria-hidden="true">
          <DownloadIcon />
        </span>
      ) : null}
    </>
  );

  if (item.url) {
    return (
      <a className={styles.row} href={item.url} rel="noreferrer" target="_blank">
        {inner}
      </a>
    );
  }
  return <div className={styles.row}>{inner}</div>;
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
