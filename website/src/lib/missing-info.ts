import type { Metric, MissingInfo } from "@/lib/api/entries";
import { metricLabels } from "@/lib/model-metrics";

/**
 * The "information missing" label (researcher answer Q5): old records that
 * lack details a write would now be rejected for stay public, and say so. The
 * backend decides what is missing with the write rules themselves; this module
 * only words it.
 */

/** A run of the model, as the page knows it. */
export type MissingInfoRun = {
  name: string;
  /** The program it ran; null when it is the run's own name. */
  software: string | null;
};

export type MissingInfoContext = {
  /** The model's metrics, from the same response as the gaps. */
  metrics: readonly Metric[];
  /** The model's runs by ID. */
  runs: ReadonlyMap<string, MissingInfoRun>;
};

/**
 * One line per gap, in reading order: the metrics without software first (as
 * one line), then each run without a version, then a computed model without a
 * run. Empty when nothing is missing or the backend did not say. Throws on a
 * gap it cannot word, rather than print a wrong line.
 */
export function missingInfoLines(
  gaps: readonly MissingInfo[] | undefined,
  context: MissingInfoContext,
): string[] {
  const metricNames: string[] = [];
  const runLines: string[] = [];
  let modelHasNoRun = false;
  for (const gap of gaps ?? []) {
    switch (gap.kind) {
      case "metric_software":
        metricNames.push(metricName(gap.metric_id, context.metrics));
        break;
      case "run_software_version":
        runLines.push(`Software version not recorded for run ${runName(gap.run_id, context.runs)}`);
        break;
      case "model_run":
        modelHasNoRun = true;
        break;
      default:
        throw new Error(`unknown missing info kind: ${String(gap.kind)}`);
    }
  }

  const lines: string[] = [];
  if (metricNames.length > 0) {
    const noun = metricNames.length === 1 ? "metric" : "metrics";
    lines.push(`Software not recorded for ${noun}: ${metricNames.join(", ")}`);
  }
  lines.push(...runLines);
  if (modelHasNoRun) {
    lines.push("No run recorded: how this computed model was made is not on file");
  }
  return lines;
}

/**
 * missingInfoLines for the page: the label is a bonus, so a gap that cannot be
 * worded is logged and the page renders without the label.
 */
export function missingInfoLinesForPage(
  gaps: readonly MissingInfo[] | undefined,
  context: MissingInfoContext,
): string[] {
  try {
    return missingInfoLines(gaps, context);
  } catch (error) {
    console.error("word missing info failed", error);
    return [];
  }
}

function metricName(metricId: string | undefined, metrics: readonly Metric[]): string {
  const metric = metrics.find((candidate) => candidate.id === metricId);
  if (!metric) {
    throw new Error(`missing info names metric ${String(metricId)}, which the model does not have`);
  }
  return metricLabels.find((known) => known.key === metric.key)?.label ?? metric.key;
}

function runName(runId: string | undefined, runs: ReadonlyMap<string, MissingInfoRun>): string {
  const run = runId === undefined ? undefined : runs.get(runId);
  if (!run) {
    throw new Error(`missing info names run ${String(runId)}, which the model does not have`);
  }
  return run.software ? `"${run.name}" (${run.software})` : `"${run.name}"`;
}
