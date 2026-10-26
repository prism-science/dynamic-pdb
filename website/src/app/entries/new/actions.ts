"use server";

import { redirect } from "next/navigation";

import {
  ApiRequestError,
  createEntry,
  createEntryRequestBody,
  getArchivePdb,
  type ArchivePolymerEntityProposal,
  type CreateEntryInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  type ApiFailure,
} from "@/app/components/entry-form/apiErrors";

export async function createEntryAction(
  input: CreateEntryInput,
): Promise<ApiFailure | void> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to create an entry.", fieldErrors: [] };
  }

  try {
    await createEntry(session.token, input);
  } catch (error) {
    return apiFailure(
      error instanceof Error ? error.message : "Failed to create the entry.",
      error instanceof ApiRequestError ? error.errors : [],
      () => createEntryRequestBody(input),
    );
  }

  redirect("/entries?tab=under-review");
}

export type ArchiveLookupResult =
  | { polymerEntities: ArchivePolymerEntityProposal[] }
  | { error: string };

/** The PDB entry's polymer entities, so the form can ask for each one's
 *  construct and mutations. Nothing is stored. */
export async function lookupArchivePolymerEntitiesAction(
  pdbId: string,
): Promise<ArchiveLookupResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to look up a PDB entry." };
  }

  try {
    const lookup = await getArchivePdb(session.token, pdbId);
    return { polymerEntities: lookup.polymer_entities };
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      return { error: `The PDB has no entry ${pdbId}.` };
    }
    return {
      error:
        error instanceof Error
          ? `Could not read ${pdbId} from the PDB: ${error.message}`
          : `Could not read ${pdbId} from the PDB.`,
    };
  }
}
