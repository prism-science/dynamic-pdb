"use server";

import {
  ApiRequestError,
  extractUploadedFile,
  type ArtifactExtraction,
} from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

/**
 * How reading an uploaded file on the server went:
 * - read: the server's values;
 * - off: the server does not read uploads (404), so the form keeps its preview;
 * - not_read: the server will not read this object (422), such as a file that
 *   is not mmCIF; the preview stays too;
 * - busy: the server is reading other files (429 or 503): try again shortly;
 * - failed: anything else, with what went wrong.
 */
export type FileExtractionResult =
  | { status: "read"; extraction: ArtifactExtraction }
  | { status: "off" }
  | { status: "not_read"; message: string }
  | { status: "busy" }
  | { status: "failed"; message: string };

/** Reads an uploaded mmCIF on the server before the entry or model is saved. */
export async function extractUploadedFileAction(input: {
  artifactId: string;
  objectUrl: string;
  sha256: string;
}): Promise<FileExtractionResult> {
  const session = await getAuthSession();
  if (!session) {
    return { status: "failed", message: "you are not signed in" };
  }
  try {
    const extraction = await extractUploadedFile(session.token, {
      artifact_id: input.artifactId,
      object_url: input.objectUrl,
      sha256: input.sha256.toLowerCase(),
    });
    return { status: "read", extraction };
  } catch (error) {
    if (error instanceof ApiRequestError) {
      switch (error.status) {
        case 404:
          // Turned off: the form keeps its preview. Anything else is a failure.
          return error.errors.some((item) => item.code === "EXTRACTION_OFF")
            ? { status: "off" }
            : { status: "failed", message: error.message };
        case 422:
          return { status: "not_read", message: error.message };
        case 429:
        case 503:
          return { status: "busy" };
        default:
          return { status: "failed", message: error.message };
      }
    }
    return {
      status: "failed",
      message: error instanceof Error ? error.message : "the server could not be reached",
    };
  }
}
