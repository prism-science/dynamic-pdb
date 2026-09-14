import type { Entity, EntryPageData } from "@/lib/api/entries";
import { getEntityFileURL, getFilePayload } from "@/lib/entities";
import { modelTitle } from "@/lib/model-metrics";
import { detectStructureKind, type StructureKind } from "@/lib/structureKind";

/**
 * The colours models are told apart by, in the order the entry lists them.
 *
 * Three and then grey, deliberately. Past three, coloured traces drawn on top
 * of one another stop being distinguishable -- these three clear the contrast
 * and colour-vision-deficiency thresholds against white as a set, a fourth hue
 * does not -- so a fourth model is named rather than coloured. The name sits
 * beside the swatch everywhere the colour is used, so identity never rests on
 * the colour alone.
 */
export const MODEL_COLORS = [0x663be4, 0xc2691a, 0x0e9b7f] as const;
export const MODEL_COLOR_REST = 0x8b8fa6;

/**
 * A model's colour, fixed by its place in the entry rather than by which model
 * the reader happens to be standing on. Switching models must not repaint the
 * others: a colour that moves is worse than no colour at all.
 */
export function modelColor(index: number): number {
  return index < MODEL_COLORS.length ? MODEL_COLORS[index] : MODEL_COLOR_REST;
}

/** One model of the entry, as the structure viewer sees it. */
export type ComparableModel = {
  modelId: string;
  title: string;
  /** Coordinates, or null when this model has no file the viewer can draw. */
  url: string | null;
  kind: StructureKind | null;
  color: number;
};

/** A model that definitely has coordinates, which is what the viewer is handed. */
export type OverlayModel = ComparableModel & {
  url: string;
  kind: StructureKind;
};

export type ModelOverlays = {
  /**
   * The colour of the model the page is on. The viewer paints it this colour
   * once a comparison is running, so the legend and the canvas agree.
   */
  baseColor: number;
  /** Every other model of the entry the viewer could lay over it. */
  others: OverlayModel[];
  /**
   * Other models of the entry the viewer cannot draw -- no coordinate file, or
   * a file with no atoms in it. Counted rather than dropped so the bar can say
   * why it has nothing to offer instead of quietly not appearing.
   */
  skipped: number;
};

/**
 * Every model of the entry with whatever the viewer would need to draw it.
 *
 * Built from `data.models` rather than from the entity list so the order is
 * the entry's own -- that order is what the colours are pinned to -- and a
 * model with no coordinate file still gets a row, with a null url, instead of
 * silently disappearing.
 */
export function comparableModels(data: EntryPageData): ComparableModel[] {
  const modelEntities = new Map<string, Entity>();
  for (const entity of data.entities) {
    if (entity.type === "model" && entity.model_id) {
      modelEntities.set(entity.model_id, entity);
    }
  }

  return data.models.map((model, index) => {
    const entity = modelEntities.get(model.id) ?? null;
    const url = entity ? getEntityFileURL(entity) : null;
    const kind =
      entity && url
        ? detectStructureKind(getFilePayload(entity)?.type ?? url)
        : null;
    // A density map is a model file the viewer recognises and cannot overlay:
    // there are no atoms in it to superpose on.
    const drawable = kind === "pdb" || kind === "mmcif";
    return {
      modelId: model.id,
      title: modelTitle(model),
      url: drawable ? url : null,
      kind: drawable ? kind : null,
      color: modelColor(index),
    };
  });
}

/**
 * What the Structure tab needs to offer a comparison: the colour of the model
 * on screen, and the other models that can be laid over it.
 *
 * Returns no others when the entry has only one model -- and the viewer draws
 * no comparison controls at all in that case, rather than an empty menu.
 */
export function modelOverlays(
  data: EntryPageData,
  currentModelId: string | null,
): ModelOverlays {
  const all = comparableModels(data);
  const base = all.find((model) => model.modelId === currentModelId);
  const rest = all.filter((model) => model.modelId !== currentModelId);
  const others = rest.filter(
    (model): model is OverlayModel =>
      model.url !== null && model.kind !== null,
  );
  return {
    baseColor: base?.color ?? modelColor(0),
    others,
    skipped: rest.length - others.length,
  };
}
