"use server";

import { redirect } from "next/navigation";

import { createStructure, type CreateStructureInput } from "@/lib/api/structures";
import { getAuthSession } from "@/lib/auth/session";

export async function createStructureAction(
  input: CreateStructureInput,
): Promise<{ error: string } | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to create a structure." };
  }

  try {
    await createStructure(session.token, input);
  } catch (error) {
    return {
      error:
        error instanceof Error ? error.message : "Failed to create the structure.",
    };
  }

  redirect("/");
}
