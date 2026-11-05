"use server";

import type { ArchiveModelLookup } from "@/app/components/entry-form/archiveModel";
import { ApiRequestError, getArchivePdb } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import { parseStructureFacts } from "@/lib/parse/structure";

const RCSB_FILES = "https://files.rcsb.org/download";
const PDB_ID_PATTERN = /^[1-9][A-Z0-9]{3}$/;
// The server's extraction reads as much of a file (lib/cif HeadReadLimit).
const COORDINATE_READ_LIMIT = 4 * 1024 * 1024;
const RCSB_TIMEOUT_MS = 30_000;

/** The file when it fits the read limit. A longer file gets no counts, rather than wrong ones. */
async function wholeCoordinateFile(url: string): Promise<string | null> {
  const response = await fetch(url, {
    cache: "no-store",
    headers: { range: `bytes=0-${COORDINATE_READ_LIMIT}` },
    signal: AbortSignal.timeout(RCSB_TIMEOUT_MS),
  });
  if (!response.ok) {
    return null;
  }
  const text = await response.text();
  return text.length > COORDINATE_READ_LIMIT ? null : text;
}

async function fileExists(url: string): Promise<boolean> {
  const response = await fetch(url, {
    method: "HEAD",
    cache: "no-store",
    signal: AbortSignal.timeout(RCSB_TIMEOUT_MS),
  });
  return response.ok;
}

function optionalNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

/** What to fill in for a model added from a PDB ID. Nothing is stored here. */
export async function lookupArchiveModelAction(
  rawPdbId: string,
): Promise<ArchiveModelLookup | { error: string }> {
  const session = await getAuthSession();
  if (!session) {
    return { error: "You must be signed in to add a model." };
  }
  const pdbId = rawPdbId.trim().toUpperCase();
  if (!PDB_ID_PATTERN.test(pdbId)) {
    return { error: "A PDB ID is four characters starting with a digit, such as 44PS." };
  }
  let archive;
  try {
    archive = await getArchivePdb(session.token, pdbId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      return { error: `The PDB has no entry ${pdbId}.` };
    }
    return { error: `Could not read ${pdbId} from the PDB.` };
  }

  const coordinateFileURL = `${RCSB_FILES}/${pdbId}.cif`;
  const structureFactorsURL = `${RCSB_FILES}/${pdbId}-sf.cif`;
  let text: string | null;
  let hasStructureFactors: boolean;
  try {
    [text, hasStructureFactors] = await Promise.all([
      wholeCoordinateFile(coordinateFileURL),
      fileExists(structureFactorsURL),
    ]);
  } catch {
    return { error: `Could not read the files of ${pdbId} from the PDB.` };
  }
  const facts = text === null ? null : parseStructureFacts(text, "mmcif").metadata;
  const altlocFraction = optionalNumber(facts?.altloc_fraction);
  const { model } = archive;
  return {
    pdbId,
    title: archive.entry.title ?? pdbId,
    coordinateFileURL,
    structureFactorsURL: hasStructureFactors ? structureFactorsURL : null,
    metadata: {
      authors: model.authors,
      ...(model.details ? { details: model.details } : {}),
      ...(model.affiliation ? { affiliation: model.affiliation } : {}),
      ...(model.atom_count !== undefined ? { atom_count: model.atom_count } : {}),
      ...(model.modeled_residues !== undefined ? { modeled_residues: model.modeled_residues } : {}),
      ...(model.unique_protein_chains !== undefined
        ? { unique_protein_chains: model.unique_protein_chains }
        : {}),
      ...(model.unmodeled_fraction !== undefined ? { unmodeled_fraction: model.unmodeled_fraction } : {}),
      ...(altlocFraction !== undefined ? { altloc_fraction: altlocFraction } : {}),
      ligands: model.ligands,
    },
    memberCount: optionalNumber(facts?.member_count) ?? null,
    metrics: model.metrics.map((metric) => ({
      key: metric.key,
      value: metric.value,
      ...(metric.software_name ? { software_name: metric.software_name } : {}),
      ...(metric.software_version ? { software_version: metric.software_version } : {}),
    })),
    relatedEntries: (model.related_entries ?? []).map((entry) => ({
      db_name: entry.db_name,
      db_id: entry.db_id,
      content_type: entry.content_type,
      ...(entry.details ? { details: entry.details } : {}),
    })),
  };
}
