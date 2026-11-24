import type { components } from "@/lib/api/schema.gen";

type MatchedModel = components["schemas"]["MatchedModel"];

/** An entry as a GET /v1/entries search returns it: with matched_models for a PDB ID search. */
type SearchedEntry = {
  id: string;
  matched_models?: readonly MatchedModel[] | null;
};

/**
 * The model a PDB ID search selected in an entry (researcher answer 4): the
 * first of matched_models, which lists archive models first. Null when the
 * search named no model's PDB ID, or the list did not come from a search.
 */
export function selectedModel(entry: SearchedEntry): MatchedModel | null {
  return entry.matched_models?.[0] ?? null;
}

/**
 * Where an entry card leads: the selected model's page for a PDB ID search,
 * the entry's page (which picks a model to start on) otherwise. Entries are
 * never merged; the model is only selected.
 */
export function entryCardHref(entry: SearchedEntry): string {
  const model = selectedModel(entry);
  return model ? `/entries/${entry.id}/models/${model.model_id}` : `/entries/${entry.id}`;
}

/** The line a card shows for the selected model, or null. */
export function selectedModelLine(entry: SearchedEntry): string | null {
  const model = selectedModel(entry);
  return model ? `Opens model PDB ${model.pdb_id.toUpperCase()}` : null;
}
