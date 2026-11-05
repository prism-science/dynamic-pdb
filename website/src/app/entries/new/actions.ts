"use server";

import { redirect } from "next/navigation";

import {
  ApiRequestError,
  createEntry,
  createEntryRequestBody,
  getArchivePdb,
  listEntries,
  type ArchiveEntryProposal,
  type ArchivePdbLookup,
  type ArchivePolymerEntityProposal,
  type CreateEntryInput,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import {
  apiFailure,
  requestFailure,
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
    return apiFailure(requestFailure(error), () => createEntryRequestBody(input));
  }

  redirect("/entries?tab=under-review");
}

export type ReRefinementOf = { pdbId: string; entryId: string };

/** The registered entry this deposition re-refines: the place its model belongs. */
async function registeredReRefinementTarget(
  token: string,
  lookup: ArchivePdbLookup,
): Promise<ReRefinementOf | null> {
  const targets = (lookup.model.related_entries ?? [])
    .filter((entry) => entry.content_type === "re-refinement" && entry.db_name === "PDB")
    .map((entry) => entry.db_id.toUpperCase());
  try {
    for (const pdbId of targets) {
      const entries = await listEntries(token, { pdbIds: [pdbId], limit: 1 });
      if (entries[0]) {
        return { pdbId, entryId: entries[0].id };
      }
    }
  } catch {
    // This only adds advice, so a failed search leaves the lookup as it is.
  }
  return null;
}

export type ArchiveLookupResult =
  | {
      entry: ArchiveEntryProposal;
      polymerEntities: ArchivePolymerEntityProposal[];
      warnings: ArchivePdbLookup["warnings"];
      /** The registered entry of the deposition this one re-refines, if any. */
      reRefinementOf: ReRefinementOf | null;
    }
  | { error: string };

/** What the PDB archive says about the entry: the values the form fills in,
 *  the polymer entities, so the form can ask for each one's construct and
 *  mutations, and the warnings about the lookup as a whole, so the form can
 *  show them. Nothing is stored. */
export async function lookupArchiveEntryAction(
  pdbId: string,
): Promise<ArchiveLookupResult> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to look up a PDB entry." };
  }

  try {
    const lookup = await getArchivePdb(session.token, pdbId);
    return {
      entry: lookup.entry,
      polymerEntities: lookup.polymer_entities,
      warnings: lookup.warnings,
      reRefinementOf: await registeredReRefinementTarget(session.token, lookup),
    };
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
