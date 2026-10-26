"use server";

import { redirect } from "next/navigation";

import {
  ApiRequestError,
  createModel,
  createModelRequestBody,
  type CreateModelInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  type ApiFailure,
} from "@/app/components/entry-form/apiErrors";

export async function createModelAction(
  entryId: string,
  input: CreateModelInput,
): Promise<ApiFailure | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to add a model.", fieldErrors: [] };
  }

  try {
    await createModel(session.token, entryId, input);
  } catch (error) {
    return apiFailure(
      error instanceof Error ? error.message : "Failed to add the model.",
      error instanceof ApiRequestError ? error.errors : [],
      () => createModelRequestBody(input),
    );
  }

  redirect(`/entries/${encodeURIComponent(entryId)}`);
}
