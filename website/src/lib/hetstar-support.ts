import type { Entity, EntryPageData, Model } from "@/lib/api/entries";
import { getEntityFileURL, getFilePayload } from "@/lib/entities";
import { detectStructureKind, type StructureKind } from "@/lib/structureKind";

/**
 * Whether the heterogeneity viewer can draw the selected model.
 *
 * hetstar loads the model the page hands it as A and the entry's deposited
 * model as B, fetching both from the catalogue in the browser and converting
 * PDB to mmCIF on arrival. Its `pickPair` throws when either one has no
 * coordinate file -- several requests deep inside a component we do not edit,
 * and the tab shows an error where a structure should be.
 *
 * This answers the same question the viewer would answer, before it is mounted,
 * from data the page has already fetched: it repeats hetstar's model choice
 * (`lib/dpdb/client.ts` `deriveRole`, `lib/dpdb/types.ts` `pickPair`) against
 * the entry as this site sees it. Untrue only if hetstar changes its pick --
 * which is a submodule bump, not a deploy, and the cost of being wrong is the
 * old viewer where the new one would have worked.
 */

/** hetstar's model roles, derived the way it derives them: from the title. */
export type ModelRole =
  | "deposited"
  | "qfit"
  | "rerefined"
  | "ensemble"
  | "other";

/**
 * The role hetstar would give this model.
 *
 * Copied from its `deriveRole`, including the order of the tests and the fall
 * back to `metadata.model_type`, which it consults only because model titles
 * are the more reliable of the two.
 */
export function modelRole(model: Model): ModelRole {
  const title = (model.title ?? "").toLowerCase();
  if (title.includes("deposited")) return "deposited";
  if (title.includes("qfit")) return "qfit";
  if (title.includes("rerefined") || title.includes("re-refined"))
    return "rerefined";
  if (title.includes("ensemble")) return "ensemble";
  const modelType = (
    typeof model.metadata?.model_type === "string" ? model.metadata.model_type : ""
  ).toLowerCase();
  if (modelType === "deposited") return "deposited";
  if (modelType === "ensemble") return "ensemble";
  return "other";
}

/**
 * The coordinate file of one model, chosen as hetstar chooses it: the model's
 * primary artifact when that is one of its coordinate files, and otherwise the
 * first coordinate file listed.
 */
function coordinateEntity(model: Model, entities: Entity[]): Entity | null {
  const own = entities.filter(
    (entity) => entity.type === "model" && entity.model_id === model.id,
  );
  return (
    own.find((entity) => entity.id === model.primary_artifact_id) ??
    own[0] ??
    null
  );
}

function coordinateKind(
  model: Model,
  entities: Entity[],
): StructureKind | null {
  const entity = coordinateEntity(model, entities);
  if (!entity) {
    return null;
  }
  const url = getEntityFileURL(entity);
  if (!url) {
    return null;
  }
  // The catalogue's own format string first, the file name only when it has
  // none. hetstar reads the same field: "pdb" it converts, anything else it
  // reads as CIF; we go by what the file actually is, so a model it would have
  // tried and failed on lands on the old viewer instead.
  return detectStructureKind(getFilePayload(entity)?.type ?? url);
}

/**
 * The pair hetstar renders for the selected model: that model against the
 * deposited one, or the deposited model alone.
 *
 * Copied from its `pickPair` given a model. B is looked for only when A is not
 * itself the deposited model, so a deposited model without coordinates stops
 * the viewer on every other model and not on its own.
 */
function hetstarPair(data: EntryPageData, modelId: string): Model[] {
  const a = data.models.find((model) => model.id === modelId);
  if (!a) {
    return [];
  }
  const b =
    modelRole(a) === "deposited"
      ? null
      : (data.models.find((model) => modelRole(model) === "deposited") ?? null);
  return b ? [a, b] : [a];
}

/**
 * Whether the Structure tab can hand the selected model to the heterogeneity
 * viewer.
 *
 * False when the model is not in the entry, or when a model in the pair has no
 * coordinate file hetstar reads (mmCIF or PDB). The tab then falls back to the
 * plain Mol* viewer on the selected model.
 */
export function hetstarCanRender(data: EntryPageData, modelId: string): boolean {
  const pair = hetstarPair(data, modelId);
  return (
    pair.length > 0 &&
    pair.every((model) => {
      const kind = coordinateKind(model, data.entities);
      return kind === "mmcif" || kind === "pdb";
    })
  );
}
