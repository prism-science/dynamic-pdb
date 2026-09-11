import type { ArtifactType, EntityLevel } from "@/lib/api/entries";
import type { ExtFileReference } from "@/lib/api/ext";

import { DRAFT_STORAGE_KEY, DRAFT_VERSION } from "./types";
import type {
  EntryMetadataDraft,
  FileSource,
  ParsedFile,
  UploadStatus,
} from "./types";

export type StoredFile = {
  id: string;
  source: FileSource;
  name: string;
  size: number;
  type: string;
  artifactType?: ArtifactType;
  level: EntityLevel;
  authors: string;
  metadata?: Record<string, unknown>;
  sha256?: string;
  preview?: string;
  url: string;
  extReference?: ExtFileReference;
};

export type StoredDraft = {
  version: number;
  entryId: string;
  metadata?: EntryMetadataDraft;
  title: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  thumbUploadStatus: UploadStatus;
  files: StoredFile[];
};

export function isPersistable(file: ParsedFile): boolean {
  return file.uploadStatus === "uploaded" && Boolean(file.url);
}

export function httpOnly(value: string | null | undefined): string | undefined {
  return value && /^https?:/i.test(value) ? value : undefined;
}

export function fileToDraft(file: ParsedFile): StoredFile {
  return {
    id: file.id,
    source: file.source,
    name: file.name,
    size: file.size,
    type: file.type,
    artifactType: file.artifactType,
    level: file.level,
    authors: file.authors,
    metadata: file.metadata,
    sha256: file.sha256,
    preview: httpOnly(file.preview),
    url: file.url,
    extReference: file.extReference,
  };
}

export function fileFromDraft(file: StoredFile): ParsedFile {
  return {
    id: file.id,
    source: file.source,
    name: file.name,
    size: file.size,
    type: file.type,
    artifactType: file.artifactType ?? "other",
    level: file.level,
    authors: file.authors,
    metadata: file.metadata,
    sha256: file.sha256,
    preview: file.preview,
    url: file.url,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    extReference: file.extReference,
  };
}

export function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.title.trim().length > 0 ||
    Boolean(draft.thumbUrl) ||
    draft.files.length > 0
  );
}

export function readStoredDraft(): StoredDraft | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    const raw = window.localStorage.getItem(DRAFT_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as StoredDraft;
    if (!parsed || parsed.version !== DRAFT_VERSION || !parsed.entryId) {
      return null;
    }
    parsed.files = Array.isArray(parsed.files) ? parsed.files : [];
    return parsed;
  } catch {
    return null;
  }
}

export function writeStoredDraft(draft: StoredDraft): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(DRAFT_STORAGE_KEY, JSON.stringify(draft));
  } catch {
    // Storage full or unavailable — drafts are best effort.
  }
}

export function clearStoredDraft(): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.removeItem(DRAFT_STORAGE_KEY);
  } catch {
    // ignore
  }
}
