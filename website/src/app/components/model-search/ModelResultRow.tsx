import Link from "next/link";
import type { ReactNode } from "react";

import type { ModelSearchResult } from "@/lib/api/entries";
import { entryIdentity } from "@/lib/entry-label";
import { modelTitle } from "@/lib/model-metrics";
import { relatedDatasetKey, relatedDatasetName } from "@/lib/related-datasets";
import { reviewStatus } from "@/lib/review-flags";
import { REVIEW_FLAG_TOOLTIP } from "@/app/components/ReviewFlagNotice";

import styles from "./model-search.module.css";

/** The scores a row shows when the model has them, in this order. */
const ROW_METRICS: readonly { key: string; label: string; decimals: number }[] = [
  { key: "r_free", label: "R-free", decimals: 3 },
  { key: "r_work", label: "R-work", decimals: 3 },
];

/**
 * One model search result: the model, labelled with its entry, and what a
 * reader must know before using it (whether it is flagged for review, compounds
 * that need review, the entry's raw datasets). Everyone sees the "Flagged for
 * review" badge; the issues follow it only when the API sent them, which it
 * does for reviewers and admins.
 *
 * `leading` is the cell before the model, kept for the selection checkbox the
 * download panel adds. Rendered on the server for the first page and in the
 * browser for the pages the scroll adds, so it holds no state.
 */
export default function ModelResultRow({
  result,
  leading,
}: {
  result: ModelSearchResult;
  leading?: ReactNode;
}) {
  const entry = result.entry;
  const identity = entry ? entryIdentity(entry) : { name: null, id: result.entry_id };
  const review = reviewStatus(result);
  const compounds = result.compounds_needing_review ?? [];
  const datasets = entry?.related_datasets ?? [];
  const facts = rowFacts(result);

  return (
    <li className={styles.row} data-model-id={result.id}>
      {leading ? <div className={styles.rowLeading}>{leading}</div> : null}
      <div className={styles.rowBody}>
        <div className={styles.rowHead}>
          <Link className={styles.rowTitle} href={`/entries/${result.entry_id}/models/${result.id}`}>
            {modelTitle(result)}
          </Link>
          {result.metadata.model_type ? (
            <span className={styles.badge}>{result.metadata.model_type}</span>
          ) : null}
          {result.metadata.purpose ? (
            <span className={styles.badgeMuted}>{result.metadata.purpose}</span>
          ) : null}
        </div>
        <p className={styles.rowEntry}>
          <span className={styles.rowEntryLabel}>Entry</span>{" "}
          <Link className={styles.rowEntryLink} href={`/entries/${result.entry_id}`}>
            {identity.name ? <span>{identity.name}</span> : null}
            <span className={styles.mono}>{identity.id}</span>
          </Link>
          {/* Without a PDB code the label already is the title. */}
          {entry?.title?.trim() && identity.name !== entry.title.trim() ? (
            <span className={styles.rowEntryTitle}>{entry.title}</span>
          ) : null}
        </p>
        {facts.length > 0 ? (
          <ul className={styles.facts} aria-label="Key values">
            {facts.map((fact) => (
              <li key={fact}>{fact}</li>
            ))}
          </ul>
        ) : null}
        {review.flagged ? (
          <p className={styles.flagWarning} role="note" title={REVIEW_FLAG_TOOLTIP}>
            <WarningIcon />
            <span>
              <strong>Flagged for review</strong>
              {review.issueLines !== null && review.issueLines.length > 0
                ? `: ${review.issueLines.join("; ")}`
                : null}
            </span>
          </p>
        ) : null}
        {compounds.length > 0 ? (
          <ul className={styles.compoundReview} aria-label="Matched compounds that need review">
            {compounds.map((compound) => (
              <li key={`${compound.ccd_id}-${compound.review_reason}`}>
                <strong>{compound.ccd_id}</strong> needs review: {compound.review_reason}
              </li>
            ))}
          </ul>
        ) : null}
        {datasets.length > 0 ? (
          <p className={styles.datasets}>
            <span className={styles.rowEntryLabel}>Raw data</span>{" "}
            {datasets.map((dataset, index) => {
              const name = relatedDatasetName(dataset);
              return (
                <span key={relatedDatasetKey(dataset)}>
                  {index > 0 ? ", " : null}
                  {name.href ? (
                    <a href={name.href} target="_blank" rel="noreferrer">
                      {name.label}
                    </a>
                  ) : (
                    name.label
                  )}
                  {dataset.data_type ? ` (${dataset.data_type})` : null}
                </span>
              );
            })}
          </p>
        ) : null}
      </div>
    </li>
  );
}

/** Method, resolution and the leading scores, as short phrases. */
function rowFacts(result: ModelSearchResult): string[] {
  const facts: string[] = [];
  const method = result.entry?.method;
  if (typeof method === "string" && method.trim()) {
    facts.push(method);
  }
  const resolution = result.entry?.resolution;
  if (typeof resolution === "number" && Number.isFinite(resolution)) {
    facts.push(`${resolution.toFixed(2)} Å`);
  }
  for (const metric of ROW_METRICS) {
    const value = result.metrics.find((candidate) => candidate.key === metric.key)?.value;
    if (typeof value === "number" && Number.isFinite(value)) {
      facts.push(`${metric.label} ${value.toFixed(metric.decimals)}`);
    }
  }
  return facts;
}

function WarningIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M8 1.8 15 14H1L8 1.8Z" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" />
      <path d="M8 6.2v3.6M8 11.6v.4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}
