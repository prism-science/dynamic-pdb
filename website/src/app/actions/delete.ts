"use server";

import { revalidatePath } from "next/cache";

import { ApiRequestError, deleteStructure, deleteModel } from "@/lib/api/structures";
import { getAuthSession } from "@/lib/auth/session";

type ActionResult = { error: string } | void;

function messageForStatus(error: unknown, fallback: string): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) {
      return "Only the creator can delete this.";
    }
    if (error.status === 404) {
      return "It looks like this was already deleted.";
    }
    if (error.status === 401) {
      return "You must be signed in to delete this.";
    }
  }
  return error instanceof Error ? error.message : fallback;
}

export async function deleteStructureAction(structureId: string): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to delete a structure." };
  }

  try {
    await deleteStructure(session.token, structureId);
  } catch (error) {
    return { error: messageForStatus(error, "Failed to delete the structure.") };
  }

  revalidatePath("/");
}

export async function deleteModelAction(
  structureId: string,
  modelId: string,
): Promise<ActionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to delete a model." };
  }

  try {
    await deleteModel(session.token, structureId, modelId);
  } catch (error) {
    return { error: messageForStatus(error, "Failed to delete the model.") };
  }

  revalidatePath(`/structures/${structureId}`);
}
