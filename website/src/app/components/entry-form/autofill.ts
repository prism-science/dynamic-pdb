import type { ArchiveEntryProposal, CreateEntryInput } from "@/lib/api/entries";

import {
  crystalDraftsFromStored,
  type ArchiveLock,
  type EntryLocks,
} from "./revisionChanges";
import type { EntryMetadataDraft } from "./types";

// The New entry form fills in what the PDB archive says about the typed PDB
// ID. Those values are shown read-only with a "from PDB X" badge and sent
// exactly as the lookup returned them, so the server finds them equal to the
// archive's, records them as the archive's, and the weekly sync keeps them
// current. What the depositor typed stays underneath: clearing or changing the
// PDB ID shows it again.

/** The values one PDB entry fills in on the New entry form. */
export type ArchiveAutofill = {
  locks: EntryLocks;
  title: string | null;
  metadata: Partial<
    Pick<EntryMetadataDraft, "details" | "resolution" | "method" | "spaceGroup" | "crystals">
  >;
  request: Partial<
    Pick<
      CreateEntryInput,
      "title" | "details" | "resolution" | "method" | "space_group" | "crystallography"
    >
  >;
};

/** What the archive's proposal fills in. A value the archive does not have
 *  stays the depositor's to type. */
export function archiveAutofill(pdbId: string, proposal: ArchiveEntryProposal): ArchiveAutofill {
  const lock: ArchiveLock = { accession: pdbId, release: proposal.latest_version ?? null };
  const autofill: ArchiveAutofill = { locks: {}, title: null, metadata: {}, request: {} };

  const title = proposal.title?.trim();
  if (title) {
    autofill.locks.title = lock;
    autofill.title = title;
    autofill.request.title = proposal.title ?? title;
  }
  if (proposal.details?.trim()) {
    autofill.locks.details = lock;
    autofill.metadata.details = proposal.details;
    autofill.request.details = proposal.details;
  }
  if (proposal.resolution != null) {
    autofill.locks.resolution = lock;
    autofill.metadata.resolution = String(proposal.resolution);
    autofill.request.resolution = proposal.resolution;
  }
  if (proposal.method?.trim()) {
    autofill.locks.method = lock;
    autofill.metadata.method = proposal.method;
    autofill.request.method = proposal.method;
  }
  if (proposal.space_group?.trim()) {
    autofill.locks.space_group = lock;
    autofill.metadata.spaceGroup = proposal.space_group;
    autofill.request.space_group = proposal.space_group;
  }
  if ((proposal.crystallography?.crystals ?? []).length > 0) {
    autofill.locks.crystallography = lock;
    autofill.metadata.crystals = crystalDraftsFromStored(proposal.crystallography);
    autofill.request.crystallography = proposal.crystallography;
  }
  return autofill;
}

/** The title the form shows: the archive's when it has one. */
export function autofilledTitle(typed: string, autofill: ArchiveAutofill | null): string {
  return autofill?.title ?? typed;
}

/** The values the form shows: the archive's where it has them. */
export function autofilledMetadata(
  typed: EntryMetadataDraft,
  autofill: ArchiveAutofill | null,
): EntryMetadataDraft {
  return autofill ? { ...typed, ...autofill.metadata } : typed;
}

/** The create request with each filled-in value exactly as the lookup
 *  returned it. */
export function withAutofilledRequest(
  input: CreateEntryInput,
  autofill: ArchiveAutofill | null,
): CreateEntryInput {
  return autofill ? { ...input, ...autofill.request } : input;
}
