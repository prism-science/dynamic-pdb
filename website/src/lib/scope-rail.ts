import type { EntryPageData } from "@/lib/api/entries";
import { buildModelMetrics } from "@/lib/model-metrics";

/**
 * One row of the scope rail. R-free is what tells the models apart, so it is
 * the one number that fits beside the name.
 */
export type ScopeRailModel = {
  id: string;
  title: string;
  thumbnailImageURL: string | null;
  rFree: number | null;
};

/**
 * The rail's model list, in the order the backend returned -- which is the
 * order they were made in, and that order is itself information.
 *
 * Built here rather than on each page so the entry and the model render the
 * same list from the same data: both already load the whole entry graph, which
 * is what carries the metrics.
 */
export function scopeRailModels(data: EntryPageData): ScopeRailModel[] {
  const metrics = buildModelMetrics(data.entities, data.relations);
  return data.models.map((model) => {
    const rFree = metrics.get(model.id)?.r_free;
    return {
      id: model.id,
      title: model.title?.trim() || "Model",
      thumbnailImageURL:
        model.thumbnail_image_url?.trim() ||
        data.entry.thumbnail_image_url?.trim() ||
        null,
      rFree: typeof rFree === "number" ? rFree : null,
    };
  });
}
