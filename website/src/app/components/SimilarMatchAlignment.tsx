"use client";

import { useState } from "react";

import type { ProteinSequence, SimilarEntryMatch } from "@/lib/api/entries";
import {
  deflineTail,
  hasRecordedAlignment,
  sequenceLabel,
  similarityStats,
  type SimilarityStats,
} from "@/lib/similarity";

import styles from "./SimilarMatchAlignment.module.css";

/**
 * One similar entry's matches, presented the way the RCSB pairwise alignment
 * tool presents a hit: a defline band whose chain-pair field doubles as the
 * switcher (the same pattern the FASTA viewer uses for chains), flat tracks
 * marking where the matched region lies on either chain (RCSB's zoomed-out
 * level), and under them the residue comparison itself (RCSB's zoomed-in
 * level): the recorded aligned strings, column by column, matches and
 * differences each tinted. Runs that didn't record a valid alignment get the
 * tracks and an honest note, never a reconstruction.
 */
export default function SimilarMatchAlignment({
  matches,
  sequences,
}: {
  matches: SimilarEntryMatch[];
  /** The source entry's sequences; matches point into them by id. */
  sequences: ProteinSequence[];
}) {
  const [activeIndex, setActiveIndex] = useState(0);

  if (matches.length === 0) {
    return null;
  }

  const index = Math.min(activeIndex, matches.length - 1);
  const match = matches[index];
  const source =
    sequences.find((sequence) => sequence.id === match.source_sequence_id) ??
    null;
  const stats = similarityStats(match.metadata);
  const recorded = hasRecordedAlignment(
    stats,
    source?.sequence ?? null,
    match.similar_sequence.sequence,
  );

  const queryLabel = source
    ? sequenceLabel(source.header, source.record_index)
    : "query";
  const matchLabel = sequenceLabel(
    match.similar_sequence.header,
    match.similar_sequence.record_index,
  );
  const pairLabel = `${queryLabel} → ${matchLabel}`;

  const switchable = matches.length > 1;
  const tail = deflineTail(match.similar_sequence.header);

  return (
    <div className={styles.wrap}>
      <div className={styles.defline}>
        {switchable ? (
          <span className={styles.switch}>
            <span>{pairLabel}</span>
            <ChevronIcon />
            <select
              className={styles.switchSelect}
              aria-label="Chain pair"
              value={index}
              onChange={(event) => setActiveIndex(Number(event.target.value))}
            >
              {matches.map((option, position) => (
                <option key={position} value={position}>
                  {optionLabel(option, position, sequences)}
                </option>
              ))}
            </select>
          </span>
        ) : (
          <span className={styles.pair}>{pairLabel}</span>
        )}
        {tail.map((field, position) => (
          <span key={position}>
            <span className={styles.separator}>|</span>
            {field}
          </span>
        ))}
      </div>

      <div className={styles.body}>
        <AlignmentMap
          stats={stats}
          queryLabel={queryLabel}
          matchLabel={matchLabel}
          queryLength={source?.sequence.length ?? null}
          matchLength={match.similar_sequence.sequence.length}
        />
        {recorded ? (
          <AlignmentBlocks
            stats={stats}
            queryLabel={queryLabel}
            matchLabel={matchLabel}
          />
        ) : (
          // The numbers came from a run that did not store its alignment, so
          // the card says exactly that instead of reconstructing one.
          <p className={styles.missing}>
            This run didn&apos;t record the residue alignment — only the
            matched spans are shown.
          </p>
        )}
      </div>
    </div>
  );
}

function optionLabel(
  match: SimilarEntryMatch,
  position: number,
  sequences: ProteinSequence[],
): string {
  const source =
    sequences.find((sequence) => sequence.id === match.source_sequence_id) ??
    null;
  const from = source
    ? sequenceLabel(source.header, source.record_index)
    : `seq ${position + 1}`;
  const to = sequenceLabel(
    match.similar_sequence.header,
    match.similar_sequence.record_index,
  );
  return `${from} → ${to}`;
}

/* ------------------------------------------------------------------ */
/* Tracks — where on either chain the matched region lies              */
/* ------------------------------------------------------------------ */

type MapSegment = {
  left: number;
  width: number;
};

function AlignmentMap({
  stats,
  queryLabel,
  matchLabel,
  queryLength,
  matchLength,
}: {
  stats: SimilarityStats;
  queryLabel: string;
  matchLabel: string;
  queryLength: number | null;
  matchLength: number;
}) {
  const query = trackSegment(stats.qstart, stats.qend, stats.qcov, queryLength);
  const target = trackSegment(
    stats.tstart,
    stats.tend,
    stats.tcov,
    matchLength,
  );
  if (!query && !target) {
    return null;
  }

  return (
    <div className={styles.map}>
      <div className={styles.mapRow}>
        <span className={styles.mapLabel}>
          <b>{queryLabel}</b>
        </span>
        <span className={styles.mapBar}>
          {query ? (
            <i style={{ left: `${query.left}%`, width: `${query.width}%` }} />
          ) : null}
        </span>
        <span className={styles.mapMeta}>
          {spanText(stats.qstart, stats.qend, queryLength)}
        </span>
      </div>
      <div className={styles.mapRow}>
        <span className={styles.mapLabel}>
          <b>{matchLabel}</b>
        </span>
        <span className={styles.mapBar}>
          {target ? (
            <i
              style={{ left: `${target.left}%`, width: `${target.width}%` }}
            />
          ) : null}
        </span>
        <span className={styles.mapMeta}>
          {spanText(stats.tstart, stats.tend, matchLength)}
        </span>
      </div>
    </div>
  );
}

/**
 * The matched span on a chain: exact when the run recorded positions,
 * approximated from coverage (anchored at the start) when it did not.
 */
function trackSegment(
  start: number | null,
  end: number | null,
  coverage: number | null,
  length: number | null,
): MapSegment | null {
  if (length == null || length === 0) {
    return null;
  }
  const from = start ?? 1;
  const span =
    start != null && end != null
      ? end - start + 1
      : Math.round((coverage ?? 1) * length);
  if (span <= 0) {
    return null;
  }
  return {
    left: ((from - 1) / length) * 100,
    width: (span / length) * 100,
  };
}

/** "12–141 · 141 aa" when the run recorded positions, "141 aa" when not. */
function spanText(
  start: number | null,
  end: number | null,
  length: number | null,
): string {
  const total = length != null ? `${length} aa` : "";
  if (start != null && end != null) {
    return total ? `${start}–${end} · ${total}` : `${start}–${end}`;
  }
  return total;
}

/* ------------------------------------------------------------------ */
/* Residue comparison, wrapped at 60 like the FASTA viewer              */
/* ------------------------------------------------------------------ */

const LINE = 60;

type Run = {
  text: string;
  kind: "plain" | "match" | "diff" | "gap";
};

function AlignmentBlocks({
  stats,
  queryLabel,
  matchLabel,
}: {
  stats: SimilarityStats;
  queryLabel: string;
  matchLabel: string;
}) {
  const qaln = stats.qaln as string;
  const taln = stats.taln as string;
  const blocks: {
    query: { runs: Run[]; start: number; end: number };
    match: { runs: Run[]; start: number; end: number };
  }[] = [];

  let queryPosition = stats.qstart ?? 1;
  let matchPosition = stats.tstart ?? 1;

  for (let offset = 0; offset < qaln.length; offset += LINE) {
    const columns = Math.min(LINE, qaln.length - offset);
    const queryRuns: Run[] = [];
    const matchRuns: Run[] = [];
    let queryResidues = 0;
    let matchResidues = 0;

    for (let column = offset; column < offset + columns; column += 1) {
      const q = qaln[column];
      const t = taln[column];
      if (q !== "-") {
        queryResidues += 1;
      }
      if (t !== "-") {
        matchResidues += 1;
      }
      // Column-by-column comparison, told once on the match line: same
      // residue or a different one. The query line only marks its own gaps.
      pushRun(queryRuns, q, q === "-" ? "gap" : "plain");
      pushRun(
        matchRuns,
        t,
        t === "-" || q === "-" ? "gap" : t === q ? "match" : "diff",
      );
    }

    blocks.push({
      query: {
        runs: queryRuns,
        start: queryPosition,
        end: queryPosition + Math.max(queryResidues - 1, 0),
      },
      match: {
        runs: matchRuns,
        start: matchPosition,
        end: matchPosition + Math.max(matchResidues - 1, 0),
      },
    });
    queryPosition += queryResidues;
    matchPosition += matchResidues;
  }

  return (
    <div className={styles.blocks}>
      {blocks.map((block, position) => (
        <div key={position} className={styles.block}>
          <AlignmentLine label={queryLabel} line={block.query} />
          <AlignmentLine label={matchLabel} line={block.match} />
        </div>
      ))}
    </div>
  );
}

function AlignmentLine({
  label,
  line,
}: {
  label: string;
  line: { runs: Run[]; start: number; end: number };
}) {
  return (
    <div className={styles.line}>
      <span className={styles.lineLabel}>{label}</span>
      <span className={styles.lineNumber}>{line.start}</span>
      <span className={styles.lineSequence}>
        {line.runs.map((run, position) =>
          run.kind === "plain" ? (
            run.text
          ) : (
            <span key={position} className={styles[run.kind]}>
              {run.text}
            </span>
          ),
        )}
      </span>
      <span className={styles.lineNumberEnd}>{line.end}</span>
    </div>
  );
}

function pushRun(runs: Run[], char: string, kind: Run["kind"]) {
  const last = runs[runs.length - 1];
  if (last && last.kind === kind) {
    last.text += char;
    return;
  }
  runs.push({ text: char, kind });
}

function ChevronIcon() {
  return (
    <svg
      className={styles.chevron}
      width="9"
      height="9"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}
