import type { EntryPageData } from "@/lib/api/entries";
import { modelTitle } from "@/lib/model-metrics";

/** One row of the scope rail: which model it is, and nothing else. The numbers
 *  that tell the models apart are on the model's own page. */
export type ScopeRailModel = {
  id: string;
  title: string;
  thumbnailImageURL: string | null;
};

/**
 * The rail's model list, in the order the backend returned -- which is the
 * order they were made in, and that order is itself information.
 *
 * Built here rather than on each page so the entry and the model render the
 * same list from the same data.
 */
export function scopeRailModels(data: EntryPageData): ScopeRailModel[] {
  return data.models.map((model) => ({
    id: model.id,
    title: modelTitle(model),
    thumbnailImageURL:
      model.thumbnail_image_url?.trim() ||
      data.entry.thumbnail_image_url?.trim() ||
      null,
  }));
}
