import type { EntryPageData } from "@/lib/api/entries";
import {
  formatMetric,
  metricSpecs,
  type MetricSpec,
  type MetricStatus,
} from "@/lib/model-metrics";
import { entryModels } from "@/lib/entry-models";

/** One stretch of a track, between two thresholds. */
export type MetricBand = {
  status: MetricStatus;
  /** Percent of the track, measured from its poor end. */
  start: number;
  width: number;
};

/** A number printed along the track, so the scale can be read off it. */
export type ScaleTick = {
  label: string;
  position: number;
};

/** One model's dot on a metric's track. */
export type ScalePoint = {
  modelId: string;
  title: string;
  value: number;
  label: string;
  /** Where the dot sits: 0 is the poor end of the fixed scale, 100 the good end. */
  position: number;
  status: MetricStatus;
  /** Set when the value falls outside the fixed scale, so the dot is parked at
   *  an end rather than drawn where it belongs. */
  offScale: "worse" | "better" | null;
  current: boolean;
  /** Best among this entry's models -- which, on an absolute track, is not the
   *  same thing as good. */
  best: boolean;
};

/**
 * One figure, drawn as the scale it is actually judged on.
 *
 * The track is the same on every entry: the ends and the two thresholds come
 * from the metric, not from the models in front of us. That is the whole point
 * of it. A track normalised to the entry's own spread answers "which of these
 * is best" -- a question the deltas below already answer exactly -- while
 * silently making a thousandth of a difference look like a chasm, and leaving a
 * lone model with nothing to draw at all. Against a fixed scale one model is
 * enough, because the question is where the model stands, not where it stands
 * among these three.
 *
 * It is a value scale, not a percentile one. wwPDB's sliders rank a structure
 * against the whole archive; ranking needs the archive's distribution, which we
 * do not hold. Fixed thresholds are the honest version of the same picture, and
 * the legend says so rather than letting the bands be mistaken for percentiles.
 */
export type MetricScale = {
  key: string;
  label: string;
  /** Poor, acceptable and good, in that order: the track runs worse to better. */
  bands: MetricBand[];
  /** The ends and the two thresholds, as numbers to print along the track. */
  ticks: ScaleTick[];
  points: ScalePoint[];
  /** The value of the model the page is on, or null when it has none. */
  currentLabel: string | null;
  currentStatus: MetricStatus | null;
  /** The thresholds in words, for the row's tooltip. */
  guide: string;
  /** Models carrying this figure, out of the entry's total. */
  recorded: number;
  total: number;
};

/**
 * Every figure any model of the entry carries, each on its own fixed scale.
 *
 * A figure only one model carries is still drawn, and drawn in full: it has a
 * scale to stand on whether or not anything else stands beside it. A figure the
 * model on screen lacks is drawn too, dimmed -- that another model of this
 * entry was measured on it, and this one was not, is a fact about this model.
 */
export function metricScales(
  data: EntryPageData,
  currentModelId: string | null,
): MetricScale[] {
  const models = entryModels(data);

  return metricSpecs.flatMap((spec) => {
    const carrying = models.filter(
      (model) => typeof model.metrics[spec.key] === "number",
    );
    if (carrying.length === 0) {
      return [];
    }

    const values = carrying.map((model) => model.metrics[spec.key] as number);
    const best = spec.lowerIsBetter ? Math.min(...values) : Math.max(...values);

    const points: ScalePoint[] = carrying.map((model) => {
      const value = model.metrics[spec.key] as number;
      const exact = place(spec, value);
      return {
        modelId: model.id,
        title: model.title,
        value,
        label: formatMetric(spec, value),
        position: Math.min(100, Math.max(0, exact)),
        status: spec.status(value),
        offScale: exact < 0 ? "worse" : exact > 100 ? "better" : null,
        current: model.id === currentModelId,
        best: value === best,
      };
    });

    const [poor, good] = spec.breaks;
    const poorAt = place(spec, poor);
    const goodAt = place(spec, good);

    const current = points.find((point) => point.current) ?? null;
    return [
      {
        key: String(spec.key),
        label: spec.label,
        bands: [
          { status: "bad" as const, start: 0, width: poorAt },
          { status: "warn" as const, start: poorAt, width: goodAt - poorAt },
          { status: "good" as const, start: goodAt, width: 100 - goodAt },
        ],
        ticks: [
          { label: tick(spec, spec.worstEnd), position: 0 },
          { label: tick(spec, poor), position: poorAt },
          { label: tick(spec, good), position: goodAt },
          { label: tick(spec, spec.bestEnd), position: 100 },
        ],
        points,
        currentLabel: current?.label ?? null,
        currentStatus: current?.status ?? null,
        guide: guide(spec),
        recorded: carrying.length,
        total: models.length,
      },
    ];
  });
}

/**
 * Where a value falls on the fixed track, as a percentage from its poor end.
 *
 * One expression for both directions: a falling figure simply has its good end
 * below its poor one, so the span comes out negative and the arithmetic turns
 * the track round by itself. Rounded on the way out, which also settles the
 * negative zero an exact hit on the poor end would otherwise produce --
 * "left: -0%" is valid CSS and an unpleasant thing to read in a snapshot.
 */
function place(spec: MetricSpec, value: number): number {
  const span = spec.bestEnd - spec.worstEnd;
  return Number((((value - spec.worstEnd) / span) * 100).toFixed(4));
}

/**
 * A number as a ruler prints it, rather than as a value is printed.
 *
 * Three decimals are the precision a measurement is reported to; on the marks
 * of a scale they are three characters of noise. A clashscore threshold is 10,
 * not 10.0, and an R-free one is 0.25, not 0.250.
 */
function tick(spec: MetricSpec, value: number): string {
  const fractional = Math.max(Math.abs(spec.worstEnd), Math.abs(spec.bestEnd)) <= 1;
  return fractional ? value.toFixed(2) : String(value);
}

function guide(spec: MetricSpec): string {
  const [poor, good] = spec.breaks;
  return spec.lowerIsBetter
    ? `good below ${tick(spec, good)}, poor at ${tick(spec, poor)} and above`
    : `good at ${tick(spec, good)} and above, poor below ${tick(spec, poor)}`;
}

