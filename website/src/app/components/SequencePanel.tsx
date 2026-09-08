"use client";

import { type CSSProperties, type ReactNode, useMemo, useState } from "react";

import {
  rulerTicks,
  type SequenceChain,
  type SequenceFeature,
  type SequenceTrack,
} from "@/lib/sequence-tracks";

import styles from "./SequencePanel.module.css";

/** Fitted to the column: one lane the width of the viewer, no letters. */
const FIT = null;
/** Pixels per residue, coarse to fine. */
const ZOOMS: (number | null)[] = [FIT, 5, 8, 11, 14, 18, 24];
/** Below this a letter is a texture rather than something to read. */
const LETTERS_FROM = 8;
/** Longer than this, one letter per residue is more scrolling than it is worth
 *  as a starting point, and the whole-chain shape is the more useful view. */
const FIT_ABOVE = 600;

/**
 * The sequence feature viewer, for one chain at a time.
 *
 * Laid out like RCSB's: the chain is chosen at the top, and under it a ruler
 * and one labelled row per feature, all in the same residue coordinates so a
 * column read down the rows is one residue. The chain's own row holds the
 * residue letters.
 *
 * Only the rows we can fill are drawn. RCSB has fifteen; several of theirs
 * come out of the wwPDB validation report, which we hold as a PDF rather than
 * as data. See sequenceTracks for what is left and why.
 */
export default function SequencePanel({ chains }: { chains: SequenceChain[] }) {
  const [activeKey, setActiveKey] = useState<string | null>(null);
  // undefined until the reader touches the control, so switching to a chain of
  // a different length gets the zoom that suits that length.
  const [zoom, setZoom] = useState<number | null | undefined>(undefined);

  const groups = useMemo(() => chainGroups(chains), [chains]);

  if (chains.length === 0) {
    return null;
  }

  const active = chains.find((chain) => chain.key === activeKey) ?? chains[0];
  const cell = zoom === undefined ? defaultZoom(active.length) : zoom;
  const laneWidth = cell === FIT ? null : active.length * cell;
  // Wide enough to read a letter in, and there is a sequence to read: below
  // that the chain's row is a bar like any other.
  const letterCell = cell !== FIT && cell >= LETTERS_FROM ? cell : null;
  const letters = letterCell === null ? null : active.sequence;
  const missing = unmodelledResidues(active.tracks);
  const ticks = rulerTicks(
    active.length,
    laneWidth === null ? 12 : Math.max(2, Math.floor(laneWidth / 58)),
  );

  return (
    <div className={styles.panel}>
      <header className={styles.head}>
        <span className={styles.chainField}>
          <span className={styles.chainCaption}>Chain</span>
          <select
            className={styles.chainSelect}
            aria-label="Chain"
            value={active.key}
            onChange={(event) => setActiveKey(event.target.value)}
          >
            {groups.map((group) => (
              <optgroup key={group.key} label={group.label}>
                {group.chains.map((chain) => (
                  <option key={chain.key} value={chain.key}>
                    {chainName(chain)}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        </span>

        <span className={styles.molecule}>{active.moleculeName}</span>
        {active.organism ? (
          <span className={styles.organism}>{active.organism}</span>
        ) : null}

        <span className={styles.headEnd}>
          <span className={styles.length}>{`${active.length} residues`}</span>
          <span className={styles.zoom} role="group" aria-label="Zoom">
            <button
              type="button"
              className={styles.zoomButton}
              onClick={() => setZoom(stepZoom(cell, -1))}
              disabled={cell === ZOOMS[0]}
              aria-label="Zoom out"
            >
              &minus;
            </button>
            <button
              type="button"
              className={styles.zoomButton}
              data-active={cell === FIT ? "true" : undefined}
              onClick={() => setZoom(FIT)}
            >
              Fit
            </button>
            <button
              type="button"
              className={styles.zoomButton}
              onClick={() => setZoom(stepZoom(cell, 1))}
              disabled={cell === ZOOMS[ZOOMS.length - 1]}
              aria-label="Zoom in"
            >
              +
            </button>
          </span>
        </span>
      </header>

      {/* The lanes are sized in pixels once zoomed in, which is what makes the
          viewer scroll instead of squeezing 300 residues into the column. The
          row labels stay put through that scroll. */}
      <div className={styles.viewer}>
        <div
          className={styles.rows}
          data-fit={laneWidth === null ? "true" : undefined}
          style={
            {
              "--lane": laneWidth === null ? "minmax(0, 1fr)" : `${laneWidth}px`,
            } as CSSProperties
          }
        >
          <div className={styles.ruler}>
            <span className={styles.rowLabel} />
            <div className={styles.rulerTrack}>
              {/* Anchored by its left edge, not its centre: centred on
                  residue 1 half the glyph would sit outside the lane and be
                  clipped away by the scrollport. */}
              <span className={styles.tickStart}>1</span>
              {ticks.map((tick) => (
                <span
                  key={tick}
                  className={styles.tick}
                  style={{ left: `${(tick / active.length) * 100}%` }}
                >
                  {tick}
                </span>
              ))}
              <span className={styles.tickEnd}>{active.length}</span>
            </div>
            <span />
          </div>

          <Row
            label={active.chainId ? `Chain ${active.chainId}` : "Sequence"}
            caption={active.entityId ? `Entity ${active.entityId}` : null}
            sequence
          >
            {letters !== null && letterCell !== null ? (
              <span
                className={styles.letters}
                style={{ "--cell": `${letterCell}px` } as CSSProperties}
              >
                {[...letters].map((letter, index) => {
                  const position = index + 1;
                  const absent = missing.has(position);
                  return (
                    <span
                      key={position}
                      className={styles.letter}
                      data-missing={absent ? "true" : undefined}
                      style={{ fontSize: `${letterSize(letterCell)}px` }}
                      title={
                        absent
                          ? `${letter}${position} · not modelled`
                          : `${letter}${position}`
                      }
                    >
                      {letter}
                    </span>
                  );
                })}
              </span>
            ) : (
              <span
                className={styles.feature}
                data-kind="span"
                style={{ left: 0, width: "100%" }}
                title={`${active.chainId ? `Chain ${active.chainId} · ` : ""}residues 1-${active.length}`}
              />
            )}
          </Row>

          {active.tracks.map((track) => (
            <Row key={track.key} label={track.label} caption={track.caption}>
              {track.features.map((feature) => (
                <span
                  key={feature.key}
                  className={styles.feature}
                  data-kind={track.kind}
                  data-track={track.key}
                  data-variant={feature.variant}
                  style={featureStyle(feature, active.length, cell)}
                  title={feature.title}
                />
              ))}
            </Row>
          ))}
        </div>
      </div>
    </div>
  );
}

function Row({
  label,
  caption,
  sequence,
  children,
}: {
  label: string;
  caption: string | null;
  /** The chain's own row, which holds letters and so runs taller. */
  sequence?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={styles.row}>
      <span className={styles.rowLabel}>
        {label}
        {caption ? <span className={styles.rowCaption}>{caption}</span> : null}
      </span>
      <div
        className={styles.rowTrack}
        data-sequence={sequence ? "true" : undefined}
      >
        {children}
      </div>
      {/* Keeps the last residue off the right edge once the viewer scrolls. */}
      <span />
    </div>
  );
}

function defaultZoom(length: number): number | null {
  return length > FIT_ABOVE ? FIT : 14;
}

function stepZoom(cell: number | null, direction: 1 | -1): number | null {
  const index = ZOOMS.indexOf(cell);
  const next = Math.min(Math.max(index + direction, 0), ZOOMS.length - 1);
  return ZOOMS[next];
}

// A letter narrower than its cell, so neighbours do not touch.
function letterSize(cell: number): number {
  return Math.max(7, Math.min(cell - 2, 12));
}

// Percentages when the lane is fitted to the column, pixels when it is not:
// the same numbers either way, but a fitted lane has no width to compute
// against until it is laid out.
function featureStyle(
  feature: SequenceFeature,
  length: number,
  cell: number | null,
): CSSProperties {
  const width = feature.end - feature.start + 1;
  const box: CSSProperties =
    cell === FIT
      ? {
          left: `${((feature.start - 1) / length) * 100}%`,
          width: `${(width / length) * 100}%`,
        }
      : {
          left: `${(feature.start - 1) * cell}px`,
          width: `${width * cell}px`,
        };
  // A level row draws its value as height from the baseline, so the row reads
  // as a profile rather than as presence.
  return feature.level === undefined
    ? box
    : { ...box, height: `${Math.max(feature.level * 100, 6)}%` };
}

// The residues the coordinates hold no atoms for, so the letters for them can
// be drawn as absent rather than looking modelled.
function unmodelledResidues(tracks: SequenceTrack[]): Set<number> {
  const track = tracks.find((item) => item.key === "unobserved");
  const positions = new Set<number>();
  for (const feature of track?.features ?? []) {
    for (let position = feature.start; position <= feature.end; position += 1) {
      positions.add(position);
    }
  }
  return positions;
}

type ChainGroup = { key: string; label: string; chains: SequenceChain[] };

// The picker is grouped by entity: a bare "A" says little when the deposit
// holds three different molecules, and the group heading is what tells the
// reader which one they are moving inside.
function chainGroups(chains: SequenceChain[]): ChainGroup[] {
  const groups: ChainGroup[] = [];
  for (const chain of chains) {
    const key = chain.entityId ?? chain.moleculeName;
    const group = groups.find((item) => item.key === key);
    if (group) {
      group.chains.push(chain);
      continue;
    }
    groups.push({
      key,
      label: chain.entityId
        ? `Entity ${chain.entityId} · ${chain.moleculeName}`
        : chain.moleculeName,
      chains: [chain],
    });
  }
  return groups;
}

function chainName(chain: SequenceChain): string {
  return chain.chainId ?? chain.moleculeName;
}
