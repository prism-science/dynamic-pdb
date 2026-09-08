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
import { useResolvedFileURL } from "@/lib/api/useResolvedFileURL";
import { csvFileName, metricLabels, metricsCSV } from "@/lib/model-metrics";
import { fastaRecordText, fastaRecords } from "@/lib/fasta";
import { detectStructureKind, type StructureMap } from "@/lib/structureKind";
import SequenceView from "./SequenceView";

import styles from "./FilePreviewModal.module.css";

const StructureViewer = dynamic(() => import("./StructureViewer"), {
  ssr: false,
});

const metricFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 3,
});

export default function FilePreviewModal({
  entity,
  maps,
  onClose,
}: {
  entity: Entity | null;
  // Density maps offered alongside the structure, so opening a model from a
  // file list gives the same viewer as opening it from its card.
  maps?: StructureMap[];
  onClose: () => void;
}) {
  const [mounted, setMounted] = useState(false);
  const [copied, setCopied] = useState(false);
  const filePayload = (entity?.payload ?? {}) as DataPayload & {
    metadata?: Record<string, unknown>;
  };
  const sourceURL =
    typeof filePayload.file_url === "string" ? filePayload.file_url : null;
  const resolvedFile = useResolvedFileURL(sourceURL);
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

  const payload = filePayload;
  const url = resolvedFile.url;
  const kind = detectStructureKind(payload.type ?? sourceURL ?? undefined);
  const isFasta = payload.type === "fasta";
  const isMetrics = entity.type === "metrics";
  const isImage =
    payload.type === "image" ||
    (sourceURL != null && /\.(png|jpe?g|gif|webp|svg|bmp)/i.test(sourceURL));
  const wide = kind != null && !isMetrics && !isFasta;
  // A structure gets the whole window; everything else stays a dialog.
  const fullscreen = wide && url != null;

  // Header actions work on the file: Download saves it, Copy puts the same
  // content on the clipboard as valid FASTA, every record, wrapped at 60.
  async function copyFasta() {
    const metadata = payload.metadata as FastaMetadata | undefined;
    if (!metadata) {
      return;
    }
    try {
      await navigator.clipboard.writeText(
        fastaRecords(metadata)
          .map((record, index) => fastaRecordText(record, `Sequence ${index + 1}`))
          .join(""),
      );
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  // Metrics are not a file, so there is nothing to link to: the two columns
  // the dialog is showing are assembled here and handed over as one.
  function downloadMetrics() {
    if (!entity) {
      return;
    }
    const blob = new Blob([metricsCSV(entity.payload as Record<string, unknown>)], {
      type: "text/csv;charset=utf-8",
    });
    const href = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = href;
    link.download = csvFileName(entity.name);
    link.click();
    // The object URL holds the blob alive until it is let go of.
    URL.revokeObjectURL(href);
  }

  return createPortal(
    <div
      className={styles.backdrop}
      data-full={fullscreen ? "true" : undefined}
      onClick={onClose}
    >
      <div
        className={[
          styles.card,
          wide ? styles.cardWide : "",
          fullscreen ? styles.cardFull : "",
        ]
          .filter(Boolean)
          .join(" ")}
        role="dialog"
        aria-label={`Preview of ${entity.name}`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className={styles.head}>
          <span className={styles.title}>{entity.name}</span>
          <div className={styles.actions}>
            {isFasta && payload.metadata ? (
              <button type="button" className={styles.copy} onClick={copyFasta}>
                {copied ? "Copied" : "Copy"}
              </button>
            ) : null}
            {isMetrics ? (
              <button
                type="button"
                className={styles.download}
                onClick={downloadMetrics}
              >
                <DownloadIcon />
                Download
              </button>
            ) : null}
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

        <div
          className={styles.body}
          data-flush={isFasta && payload.metadata ? "true" : undefined}
        >
          {isMetrics ? (
            <MetricsView payload={entity.payload as MetricsPayload} />
          ) : isFasta && payload.metadata ? (
            <SequenceView metadata={payload.metadata as FastaMetadata} />
          ) : isImage && url ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img className={styles.previewImg} src={url} alt={entity.name} />
          ) : kind && url ? (
            <StructureViewer url={url} kind={kind} maps={maps} fill={fullscreen} />
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
  const rows = metricLabels.filter(
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
