"use server";

import { redirect } from "next/navigation";

import {
  createModel,
  createModelRequestBody,
  type CreateModelInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  requestFailure,
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
    return apiFailure(requestFailure(error), () => createModelRequestBody(input));
  }

  redirect(`/entries/${encodeURIComponent(entryId)}`);
}
