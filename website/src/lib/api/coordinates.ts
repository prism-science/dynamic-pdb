import "server-only";

import { readStructure, type StructureResidues } from "@/lib/structure-tracks";

/** Coordinate files are a few hundred kilobytes and never change once written,
 *  so a long cache costs nothing and keeps the tab off the wire. */
const CACHE_SECONDS = 60 * 60 * 24;

/** Above this the file is not a single model any more -- it is an ensemble or a
 *  virus capsid, and parsing it on a page render would block the response. */
const MAX_BYTES = 24 * 1024 * 1024;

/**
 * Read the residue-level facts out of a model's coordinate file, one entry per
 * chain the file contains.
 *
 * Returns nothing on anything going wrong -- the address is an s3:// URI we
 * cannot fetch, the host is unreachable, the file is too big, the format is
 * not mmCIF. The Sequence tab then shows the rows that come from the entry and
 * simply omits the ones that come from the coordinates, which is the same
 * thing it does for a model with no coordinate file at all.
 */
export async function readModelStructure(
  url: string | null,
): Promise<StructureResidues[]> {
  if (!url || !/^https?:/i.test(url)) {
    return [];
  }
  try {
    const response = await fetch(url, { next: { revalidate: CACHE_SECONDS } });
    if (!response.ok) {
      return [];
    }
    const length = Number(response.headers.get("content-length") ?? "0");
    if (length > MAX_BYTES) {
      return [];
    }
    const text = await response.text();
    if (text.length > MAX_BYTES) {
      return [];
    }
    return readStructure(text);
  } catch (error) {
    console.error("read model coordinates failed", url, error);
    return [];
  }
}
