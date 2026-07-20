"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import dynamic from "next/dynamic";

import type {
  DataPayload,
  Entity,
  FastaMetadata,
  MetricsPayload,
} from "@/lib/api/entries";
import { detectStructureKind } from "@/lib/structureKind";
import SequenceView from "./SequenceView";

import styles from "./FilePreviewModal.module.css";

const StructureViewer = dynamic(() => import("./StructureViewer"), {
  ssr: false,
});

const METRIC_LABELS: { key: keyof MetricsPayload; label: string }[] = [
  { key: "r_work", label: "R-work" },
  { key: "r_free", label: "R-free" },
  { key: "cc", label: "CC" },
  { key: "rscc", label: "RSCC" },
];

const metricFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 3,
});

export default function FilePreviewModal({
  entity,
  onClose,
}: {
  entity: Entity | null;
  onClose: () => void;
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (!entity) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [entity, onClose]);

  if (!mounted || !entity) {
    return null;
  }

  const payload = (entity.payload ?? {}) as DataPayload & {
    metadata?: Record<string, unknown>;
  };
  const url = typeof payload.file_url === "string" ? payload.file_url : null;
  const kind = detectStructureKind(payload.type ?? url ?? undefined);
  const isFasta = payload.type === "fasta";
  const isMetrics = entity.type === "metrics";
  const wide = kind != null && !isMetrics && !isFasta;

  return createPortal(
    <div className={styles.backdrop} onClick={onClose}>
      <div
        className={`${styles.card} ${wide ? styles.cardWide : ""}`}
        role="dialog"
        aria-label={`Preview of ${entity.name}`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className={styles.head}>
          <span className={styles.title}>{entity.name}</span>
          <div className={styles.actions}>
            {url ? (
              <a className={styles.download} href={url} download target="_blank" rel="noreferrer">
                <DownloadIcon />
                Download
              </a>
            ) : null}
            <button
              type="button"
              className={styles.close}
              onClick={onClose}
              aria-label="Close"
            >
              ×
            </button>
          </div>
        </div>

        <div className={styles.body}>
          {isMetrics ? (
            <MetricsView payload={entity.payload as MetricsPayload} />
          ) : isFasta && payload.metadata ? (
            <SequenceView metadata={payload.metadata as FastaMetadata} />
          ) : kind && url ? (
            <StructureViewer url={url} kind={kind} />
          ) : (
            <GenericView type={payload.type} size={payload.size} url={url} />
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}

function MetricsView({ payload }: { payload: MetricsPayload }) {
  const rows = METRIC_LABELS.filter(
    (metric) => typeof payload?.[metric.key] === "number",
  );
  if (rows.length === 0) {
    return <p className={styles.note}>No metrics recorded.</p>;
  }
  return (
    <dl className={styles.metrics}>
      {rows.map((metric) => (
        <div key={metric.key} className={styles.metricRow}>
          <dt>{metric.label}</dt>
          <dd>{metricFormatter.format(payload[metric.key] as number)}</dd>
        </div>
      ))}
    </dl>
  );
}

function GenericView({
  type,
  size,
  url,
}: {
  type?: string;
  size?: number;
  url: string | null;
}) {
  return (
    <dl className={styles.facts}>
      {type ? (
        <div className={styles.factRow}>
          <dt>Type</dt>
          <dd>{type.toUpperCase()}</dd>
        </div>
      ) : null}
      <div className={styles.factRow}>
        <dt>Size</dt>
        <dd>{formatSize(size)}</dd>
      </div>
      {!url ? <p className={styles.note}>No preview available.</p> : null}
    </dl>
  );
}

function DownloadIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 21h14" />
    </svg>
  );
}

function formatSize(size: number | undefined): string {
  if (typeof size !== "number" || size <= 0) {
    return "—";
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
