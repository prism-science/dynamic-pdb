"use client";

import {
  type CSSProperties,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  rulerTicks,
  type SequenceChain,
  type SequenceFeature,
  type SequenceTrack,
} from "@/lib/sequence-tracks";

import DisagreementRegions from "./DisagreementRegions";

import styles from "./SequencePanel.module.css";

/** Fitted to the column: one lane the width of the viewer. */
const FIT = null;
/** Pixels per residue, coarse to fine: the rungs the -- and + buttons walk. */
const ZOOMS = [5, 8, 11, 14, 18, 24];
/** Finer than the finest rung is more scrolling than it is reading. */
const MAX_ZOOM = ZOOMS[ZOOMS.length - 1];
/** Pixels per residue at which a letter is read rather than guessed at. */
const LETTERS_FROM = 8;
/** Below this the dots touch, so counting them out one element per residue
 *  buys nothing a repeating background cannot draw -- and a chain long enough
 *  to get here is long enough for the difference to be thousands of them. */
const DOTS_FROM = 1.5;
/** How fast a pinch zooms: e^(1/PINCH) per unit of wheel delta. */
const PINCH = 100;

/**
 * The sequence feature viewer, for one chain at a time.
 *
 * Laid out like RCSB's: the chain is chosen at the top, and under it a ruler
 * and one labelled row per feature, all in the same residue coordinates so a
 * column read down the rows is one residue. The chain's own row holds the
 * residues.
 *
 * It opens fitted -- the whole chain in the column, however long the chain --
 * and is zoomed with the buttons or by pinching the trackpad, which anchors on
 * the residue under the pointer the way a map does. Zoomed in, the viewer
 * scrolls sideways and the row labels stay put.
 *
 * Clicking holds a column: open ground holds the residue under the pointer, a
 * feature holds the whole feature, and clicking what is held lets it go.
 *
 * How much room a residue gets decides what its row can say: a letter where
 * there is space for one, otherwise a grey dot per residue, which still reads
 * as a sequence counted out one residue at a time rather than as a bar.
 *
 * Only the rows we can fill are drawn. RCSB has fifteen; several of theirs
 * come out of the wwPDB validation report, which we hold as a PDF rather than
 * as data. See sequenceTracks for what is left and why.
 */
export default function SequencePanel({
  chains,
  comparison,
}: {
  chains: SequenceChain[];
  /** How the read of the coordinates is going, for the line that says what
   *  the rows were measured against and what could not be read. */
  comparison?: Comparison;
}) {
  const [activeKey, setActiveKey] = useState<string | null>(null);
  /** Pixels per residue, or FIT. Fitted until the reader says otherwise. */
  const [zoom, setZoom] = useState<number | null>(FIT);
  /** The lane's width when fitted, which is the column's width less the label
   *  and trailing gutters. Null until the browser has laid the viewer out. */
  const [fitLane, setFitLane] = useState<number | null>(null);
  const [viewerNode, setViewerNode] = useState<HTMLDivElement | null>(null);
  /** The residues held by a click, marked down every row so one column can be
   *  read across the whole board at once. Nothing is marked until the reader
   *  asks for it: a mark that follows the pointer is in the way of reading. */
  const [selection, setSelection] = useState<Span | null>(null);
  const [laneNode, setLaneNode] = useState<HTMLDivElement | null>(null);
  /** Where the pointer was over the lane when a pinch changed the zoom, so
   *  the scrollport can put that residue back under it. */
  const anchor = useRef<{ fraction: number; clientX: number } | null>(null);

  const groups = useMemo(() => chainGroups(chains), [chains]);

  const active =
    chains.length === 0
      ? null
      : (chains.find((chain) => chain.key === activeKey) ?? chains[0]);
  const fitCell =
    active === null || fitLane === null ? null : fitLane / active.length;

  useEffect(() => {
    if (
      viewerNode === null ||
      laneNode === null ||
      typeof ResizeObserver === "undefined"
    ) {
      return;
    }
    // The label and trailing gutters are fixed grid columns, so what the ruler
    // row measures beyond its own lane is exactly what a fitted lane cannot
    // have -- and that holds zoomed in, where the row is wider than the box it
    // scrolls inside.
    const measure = () => {
      const row = laneNode.parentElement;
      if (row === null) {
        return;
      }
      const gutters = row.offsetWidth - laneNode.offsetWidth;
      setFitLane(viewerNode.clientWidth - gutters);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(viewerNode);
    return () => observer.disconnect();
  }, [viewerNode, laneNode]);

  useEffect(() => {
    if (viewerNode === null || laneNode === null || fitCell === null) {
      return;
    }
    // A pinch on a trackpad arrives as a wheel event with ctrlKey set. The
    // default for that is the browser's own page zoom, which is not what the
    // gesture means inside a viewer that has a zoom of its own.
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey) {
        return;
      }
      event.preventDefault();
      const box = laneNode.getBoundingClientRect();
      anchor.current = {
        fraction: (event.clientX - box.left) / box.width,
        clientX: event.clientX,
      };
      const factor = Math.exp(-event.deltaY / PINCH);
      setZoom((previous) => {
        const next = (previous ?? fitCell) * factor;
        return next <= fitCell ? FIT : Math.min(next, MAX_ZOOM);
      });
    };
    viewerNode.addEventListener("wheel", onWheel, { passive: false });
    return () => viewerNode.removeEventListener("wheel", onWheel);
  }, [viewerNode, laneNode, fitCell]);

  useEffect(() => {
    if (selection === null) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setSelection(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selection]);

  useEffect(() => {
    const held = anchor.current;
    anchor.current = null;
    if (viewerNode === null || laneNode === null || held === null) {
      return;
    }
    const box = laneNode.getBoundingClientRect();
    viewerNode.scrollLeft +=
      box.left + held.fraction * box.width - held.clientX;
  }, [zoom, viewerNode, laneNode]);

  if (active === null) {
    return null;
  }

  // A click always marks what was clicked. It used to toggle -- clicking the
  // held range let it go -- and that made the mark look broken: ground in two
  // different rows at the same place is the same one residue, so clicking down
  // a column turned the mark on, off, on. Escape takes it off, and so does a
  // click outside the lane.
  const hold = (next: Span) => setSelection(next);

  const cell = zoom ?? fitCell;
  const laneWidth = zoom === FIT ? null : active.length * zoom;
  const mode = residueMode(cell);
  const missing = unmodelledResidues(active.tracks);
  const note = formatNote(comparison);
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
            onChange={(event) => {
              // Residue numbers belong to the chain they were read in.
              setSelection(null);
              setActiveKey(event.target.value);
            }}
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
              onClick={() => setZoom(zoomOut(cell, fitCell))}
              disabled={zoom === FIT}
              aria-label="Zoom out"
            >
              &minus;
            </button>
            <button
              type="button"
              className={styles.zoomButton}
              data-active={zoom === FIT ? "true" : undefined}
              onClick={() => setZoom(FIT)}
            >
              Fit
            </button>
            <button
              type="button"
              className={styles.zoomButton}
              onClick={() => setZoom(zoomIn(cell))}
              disabled={cell !== null && cell >= MAX_ZOOM}
              aria-label="Zoom in"
            >
              +
            </button>
          </span>
        </span>
      </header>

      {/* How the chart was arrived at, said out loud: it is a set of
          differences between coordinate files, measured after a superposition,
          and neither of those is visible in a bar. */}
      {note ? <p className={styles.method}>{note}</p> : null}


      {/* The lanes are sized in pixels once zoomed in, which is what makes the
          viewer scroll instead of squeezing 300 residues into the column. The
          row labels stay put through that scroll. */}
      <div
        className={styles.viewer}
        ref={setViewerNode}
        onClick={(event) => {
          // A click on open ground holds the one residue under it; a click on a
          // feature holds the whole feature, and is handled there.
          if (laneNode === null) {
            return;
          }
          const box = laneNode.getBoundingClientRect();
          const at = Math.floor(
            ((event.clientX - box.left) / box.width) * active.length,
          );
          if (at < 0 || at >= active.length) {
            setSelection(null);
            return;
          }
          hold({ start: at + 1, end: at + 1 });
        }}
      >
        <div
          className={styles.rows}
          data-fit={laneWidth === null ? "true" : undefined}
          style={
            {
              "--lane":
                laneWidth === null ? "minmax(0, 1fr)" : `${laneWidth}px`,
              "--residues": active.length,
            } as CSSProperties
          }
        >
          <div className={styles.ruler}>
            <span className={styles.rowLabel} />
            <div className={styles.rulerTrack} ref={setLaneNode}>
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
            sequence
          >
            {active.sequence === null ? (
              // Nothing to count out: the entry gives this chain a residue
              // count and no residues, so the row says only how far it runs.
              <span
                className={styles.feature}
                data-kind="span"
                style={{ left: 0, width: "100%" }}
                title={`${active.chainId ? `Chain ${active.chainId} · ` : ""}residues 1-${active.length}`}
              />
            ) : (
              <span
                className={styles.residues}
                data-mode={mode}
                style={{ "--dot": `${dotSize(cell ?? 0)}px` } as CSSProperties}
              >
                {mode === "dense"
                  ? null
                  : [...active.sequence].map((letter, index) => {
                      const position = index + 1;
                      const absent = missing.has(position);
                      return (
                        <span
                          key={position}
                          className={styles.residue}
                          data-missing={absent ? "true" : undefined}
                          style={
                            mode === "letters"
                              ? { fontSize: `${letterSize(cell ?? 0)}px` }
                              : undefined
                          }
                          title={
                            absent
                              ? `${letter}${position} · not modelled`
                              : `${letter}${position}`
                          }
                        >
                          {mode === "letters" ? letter : null}
                        </span>
                      );
                    })}
              </span>
            )}
          </Row>

          {active.tracks.map((track) => (
            <Row
              key={track.key}
              label={track.label}
              hint={track.hint}
              scale={track.scale}
              // A hairline of ground between the columns, once there is room
              // for one: at three pixels a residue they run together into a
              // silhouette, and below that the gap would eat the bar.
              separated={cell !== null && cell >= 3}
            >
              {track.features.map((feature) => (
                <span
                  key={feature.key}
                  className={styles.feature}
                  data-kind={track.kind}
                  data-track={track.key}
                  data-variant={feature.variant}
                  data-held={holds(selection, feature) ? "true" : undefined}
                  style={featureStyle(feature, active.length)}
                  title={feature.title}
                  onClick={(event) => {
                    event.stopPropagation();
                    hold({ start: feature.start, end: feature.end });
                  }}
                />
              ))}
            </Row>
          ))}

          {/* Nothing has arrived yet, and the tall row is where it will land.
              Held open rather than left out, because every figure on it comes
              out of a coordinate file read in the browser -- seconds on a
              large model -- and without this the board looks finished. */}
          {active.agreement === null && waiting(comparison) ? (
            <Row
              label={`Backbone spread${
                pendingFiles(comparison) > 0
                  ? ` · reading ${pendingFiles(comparison)}`
                  : " (between models)"
              }`}
              scale={{ max: 0, unit: "" }}
            >
              <span className={styles.waiting} aria-hidden="true">
                {GHOSTS.map((height, index) => (
                  <span
                    key={index}
                    className={styles.ghost}
                    style={{ height: `${height}%` }}
                  />
                ))}
              </span>
            </Row>
          ) : null}

          {/* Last, so it paints over every track: the mark is on top of the
              board, not under it. Still under the row labels, which carry a
              z-index of their own. */}
          {selection === null ? null : (
            <div
              className={styles.column}
              style={
                {
                  "--at": (selection.start - 1) / active.length,
                  "--span": selection.end - selection.start + 1,
                } as CSSProperties
              }
              aria-hidden
            />
          )}
        </div>
      </div>

      {active.agreement ? (
        <DisagreementRegions regions={active.agreement.regions} />
      ) : null}
    </div>
  );
}

function Row({
  label,
  hint,
  sequence,
  scale,
  separated,
  children,
}: {
  label: string;
  /** What to say on the label's tooltip instead of the label itself. */
  hint?: string;
  /** The chain's own row, which holds residues and so runs taller. */
  sequence?: boolean;
  /** Set on a row that draws a quantity: it runs tall and carries an axis. */
  scale?: { max: number; unit: string };
  /** There is room to leave a hairline between neighbouring columns. */
  separated?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={styles.row}>
      {/* Titled as well as printed: a row named after a model carries that
          model's whole name, and the label column is 156px wide. A row that
          was computed rather than read says how, here. */}
      <span
        className={styles.rowLabel}
        data-tall={scale ? "true" : undefined}
        title={hint ?? label}
      >
        {label}
      </span>
      <div
        className={styles.rowTrack}
        data-sequence={sequence ? "true" : undefined}
        data-tall={scale ? "true" : undefined}
        data-separated={separated ? "true" : undefined}
      >
        {/* The axis, inside the lane: the label column is taken, and a profile
            whose height stands for nothing stated is not a profile. Both lines
            are numbered -- one number at the top tells a reader what full
            height means, two tell them the row is linear, which is the other
            half of reading a bar off it. */}
        {scale ? (
          <>
            <span className={styles.laneTop} aria-hidden="true" />
            <span className={styles.laneMid} aria-hidden="true" />
            {scale.max > 0 ? (
              <span className={styles.laneAxis} aria-hidden="true">
                <span className={styles.laneTick} data-at="top">
                  {axisValue(scale.max)} {scale.unit}
                </span>
                <span className={styles.laneTick} data-at="mid">
                  {axisValue(scale.max / 2)}
                </span>
              </span>
            ) : null}
          </>
        ) : null}
        {children}
      </div>
      {/* Keeps the last residue off the right edge once the viewer scrolls. */}
      <span />
    </div>
  );
}

// Enough decimals to tell the two ticks apart, and no more: half of 0.50 is
// 0.25, and half of 3 is 1.5.
function axisValue(value: number): string {
  return value >= 1 ? String(Number(value.toFixed(1))) : value.toFixed(2);
}

/** Whether a feature falls inside the column the reader is holding. */
function holds(selection: Span | null, feature: SequenceFeature): boolean {
  return (
    selection !== null &&
    feature.start >= selection.start &&
    feature.end <= selection.end
  );
}

function pendingFiles(comparison?: Comparison): number {
  return (
    (comparison?.loadingBase ? 1 : 0) + (comparison?.pending ?? 0)
  );
}

/** A deterministic profile for the row held open while files arrive -- no
 *  randomness, so the server and the browser draw the same thing. */
const GHOSTS = Array.from({ length: 120 }, (_, index) =>
  Math.round(
    28 +
      18 * Math.sin(index / 4.5) +
      12 * Math.sin(index / 1.7) +
      8 * Math.sin(index / 11),
  ),
);

/** A run of residues, 1-based and inclusive, in the chain's own numbering. */
type Span = { start: number; end: number };

type Comparison = {
  /** Other models still to read. */
  pending: number;
  /** Other models that could not be read at all. */
  unread: number;
  /** The format of this model's own coordinates when it is not one these rows
   *  can be read from, or null when it is. */
  unreadableBase: string | null;
  /** This model's own file has not arrived yet. */
  loadingBase: boolean;
};

/**
 * What the residue row can say in the room each residue gets: a letter, a dot,
 * or -- once the dots would touch -- the same dots drawn as a repeating
 * background rather than as thousands of elements.
 *
 * Unmeasured counts as dense, because that is where a long chain settles and a
 * long chain is the one worth not flickering.
 */
type ResidueMode = "letters" | "dots" | "dense";

function residueMode(cell: number | null): ResidueMode {
  if (cell === null || cell < DOTS_FROM) {
    return "dense";
  }
  return cell >= LETTERS_FROM ? "letters" : "dots";
}

// The next rung up, which is where a button press lands however the reader got
// to the zoom they are at -- a pinch leaves them between rungs.
function zoomIn(cell: number | null): number {
  return ZOOMS.find((rung) => cell === null || rung > cell) ?? MAX_ZOOM;
}

// The next rung down, and off the bottom rung back to fitted: below fitted the
// lane would be narrower than the column it sits in.
function zoomOut(cell: number | null, fitCell: number | null): number | null {
  if (cell === null) {
    return FIT;
  }
  const rungs = ZOOMS.filter(
    (rung) => rung < cell && (fitCell === null || rung > fitCell),
  );
  return rungs.length === 0 ? FIT : rungs[rungs.length - 1];
}

// A letter narrower than its cell, so neighbours do not touch.
function letterSize(cell: number): number {
  return Math.max(7, Math.min(Math.floor(cell) - 2, 12));
}

// Half the cell, so there is as much ground around a dot as there is dot: less
// and the row reads as a dashed line rather than as residues.
function dotSize(cell: number): number {
  return Math.max(1, Math.min(3, Math.round(cell / 2)));
}

// Every position is a share of the lane, and the lane is the whole chain, so a
// feature is placed as a percentage of it -- which holds fitted and zoomed
// alike, because a zoomed lane is exactly as many pixels as it has residues.
function featureStyle(feature: SequenceFeature, length: number): CSSProperties {
  const width = feature.end - feature.start + 1;
  const box: CSSProperties = {
    left: `${((feature.start - 1) / length) * 100}%`,
    width: `${(width / length) * 100}%`,
  };
  // A level row draws its value as height from the baseline, so the row reads
  // as a profile rather than as presence. No floor under the height: on a tall
  // row a floor would give every residue a visible bar, and "these models
  // agree here" would look the same as "they are a little apart". A hairline
  // minimum comes from the stylesheet instead.
  return feature.level === undefined
    ? box
    : { ...box, height: `${feature.level * 100}%` };
}

// The residues the coordinates hold no atoms for, so they can be drawn as
// absent rather than looking modelled.
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

// Something is still coming: either this model's own coordinates, or the other
// models the comparison is with.
function waiting(comparison?: Comparison): boolean {
  return (
    comparison !== undefined &&
    (comparison.loadingBase || comparison.pending > 0)
  );
}

/**
 * The one sentence that is not a caveat but an explanation of an empty board.
 *
 * Everything else that used to be printed here -- what the comparison was
 * with, how well it fitted, what could not be read -- is on the tooltip of the
 * row it belongs to. This stays on the page because without it a PDB-format
 * model looks like a model nothing is known about.
 */
function formatNote(comparison?: Comparison): string | null {
  return comparison?.unreadableBase
    ? `Residue rows are read from mmCIF, and this model's coordinates are ${comparison.unreadableBase.toUpperCase()}.`
    : null;
}
