import type { MetricScale } from "@/lib/model-comparison";

import styles from "./MetricScales.module.css";

/**
 * Where the model on screen stands, one scale per figure.
 *
 * Each track is the scale the figure is judged on, not the spread of the models
 * in front of us: the same ends and the same thresholds on every entry, so a
 * dot near the good end means the model is good rather than merely the best of
 * whatever happens to be here, and two entries can be read against each other.
 * It also means a figure only one model carries still has somewhere to be
 * drawn -- there is no such thing as a lone model on an absolute scale, which
 * is the case a relative track had nothing to say about.
 *
 * Every track runs poor on the left to good on the right, whichever way the
 * figure itself happens to point, so the good end is the same end all the way
 * down the block.
 *
 * Two kinds of dot and no more: this model, and the rest of the entry. The
 * block is about where one model stands, and a dot per model in a colour per
 * model needed a key above it that cost more room than the tracks themselves.
 * Which of the others is which is a question asked of one dot at a time, so it
 * is answered on hover.
 */
export default function MetricScales({ scales }: { scales: MetricScale[] }) {
  if (scales.length === 0) {
    return null;
  }

  return (
    <div className={styles.scales}>
      {scales.map((scale) => (
        <div
          key={scale.key}
          className={styles.scale}
          data-absent={scale.currentLabel === null ? "true" : undefined}
        >
          <div className={styles.name}>
            {scale.label}
            <span className={styles.direction} aria-hidden="true">
              {scale.direction}
            </span>
          </div>

          <div className={styles.track} title={`${scale.label} — ${scale.guide}`}>
            {/* The numbers of the scale itself. Without them a dot two thirds
                along says nothing: this is a value axis, not a percentile. */}
            <div className={styles.ruler} aria-hidden="true">
              {scale.ticks.map((tick, index) => (
                <span
                  key={`${tick.label}-${tick.position}`}
                  className={styles.tick}
                  data-anchor={
                    index === 0
                      ? "start"
                      : index === scale.ticks.length - 1
                        ? "end"
                        : undefined
                  }
                  style={{ left: `${tick.position}%` }}
                >
                  {tick.label}
                </span>
              ))}
            </div>

            <div className={styles.bar} aria-hidden="true">
              {scale.bands.map((band) => (
                <span
                  key={band.status}
                  className={styles.band}
                  data-status={band.status}
                  style={{ left: `${band.start}%`, width: `${band.width}%` }}
                />
              ))}
            </div>

            {scale.points.map((point) => (
              // The label is a sibling rather than a child so the arrowhead an
              // off-scale dot is clipped into does not clip the label with it.
              <span key={point.modelId} className={styles.marker}>
                <span
                  className={styles.dot}
                  data-current={point.current ? "true" : undefined}
                  data-off={point.offScale ?? undefined}
                  style={{ left: `${point.position}%` }}
                  // Empty on purpose: without it the track's own tooltip shows
                  // through on top of the label below.
                  title=""
                  aria-label={`${point.title}: ${point.label}`}
                />
                <span
                  className={styles.tip}
                  data-anchor={
                    point.position <= 8
                      ? "start"
                      : point.position >= 92
                        ? "end"
                        : undefined
                  }
                  style={{ left: `${point.position}%` }}
                  aria-hidden="true"
                >
                  {point.title} · {point.label}
                  {point.offScale ? " · past the end of the scale" : ""}
                </span>
              </span>
            ))}
          </div>

          {/* This model's value, and nothing else. The verdict is already in
              the band the dot is standing in and in the colour of the figure,
              how many models the track was drawn from is answered by counting
              the dots, and a dash on a dimmed row already says the figure was
              not recorded for this one. Written out, every one of them was
              caption on caption. */}
          <div className={styles.current}>
            {scale.currentLabel ? (
              <span
                className={styles.currentValue}
                data-status={scale.currentStatus ?? undefined}
              >
                {scale.currentLabel}
              </span>
            ) : (
              <span
                className={styles.currentMissing}
                title={`Not recorded for this model — ${scale.label} comes from ${
                  scale.recorded === 1 ? "another model" : "other models"
                } of this entry`}
              >
                —
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
