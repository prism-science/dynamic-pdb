import type { ArtifactType, EntityLevel } from "@/lib/api/entries";
import type { ExtFileReference } from "@/lib/api/ext";

import { compoundsAreUntouched } from "./compounds";
import {
  DRAFT_STORAGE_KEY,
  DRAFT_VERSION,
  MODEL_DRAFT_STORAGE_PREFIX,
  MODEL_DRAFT_VERSION,
  PREVIOUS_DRAFT_VERSION,
  emptyCompounds,
  emptyEntryMetadataDraft,
} from "./types";
import type {
  CompoundsDraft,
  EntryMetadataDraft,
  FileSource,
  MetricDraft,
  ModelDraft,
  ParsedFile,
  ProgramDraft,
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

export function entryMetadataHasContent(metadata: EntryMetadataDraft): boolean {
  return (
    [
      metadata.details,
      metadata.pdb,
      metadata.resolution,
      metadata.method,
      metadata.spaceGroup,
    ].some((value) => value.trim().length > 0) ||
    !compoundsAreUntouched(metadata.compounds) ||
    metadata.crystals.length > 0
  );
}

export function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.title.trim().length > 0 ||
    Boolean(draft.thumbUrl) ||
    draft.files.length > 0 ||
    (draft.metadata ? entryMetadataHasContent(draft.metadata) : false)
  );
}

/**
 * Reads the saved entry draft. A version 3 draft predates the saved metadata
 * block and is read with an empty one rather than thrown away.
 */
export function readStoredDraft(): StoredDraft | null {
  const parsed = readStoredJSON(DRAFT_STORAGE_KEY) as StoredDraft | null;
  if (
    !parsed ||
    (parsed.version !== DRAFT_VERSION &&
      parsed.version !== PREVIOUS_DRAFT_VERSION) ||
    !parsed.entryId
  ) {
    return null;
  }
  return {
    ...parsed,
    version: DRAFT_VERSION,
    title: typeof parsed.title === "string" ? parsed.title : "",
    files: Array.isArray(parsed.files) ? parsed.files : [],
    metadata: { ...emptyEntryMetadataDraft(), ...(parsed.metadata ?? {}) },
  };
}

export function writeStoredDraft(draft: StoredDraft): void {
  writeStoredJSON(DRAFT_STORAGE_KEY, draft);
}

export function clearStoredDraft(): void {
  clearStoredJSON(DRAFT_STORAGE_KEY);
}

// ------------------------------------------------------------ model drafts --

/** A model draft as saved: everything but files still uploading, a picked
 *  thumbnail that never finished uploading, and File objects. */
export type StoredModelDraft = {
  version: number;
  id: string;
  title: string;
  purpose: string;
  modelType: string;
  details: string;
  authors: string[];
  affiliation: string;
  compounds: CompoundsDraft;
  compoundsSource: string | null;
  thumbUrl: string | null;
  thumbPreview?: string;
  files: StoredFile[];
  metrics: MetricDraft[];
  programs: ProgramDraft[];
};

export function modelDraftStorageKey(entryId: string): string {
  return `${MODEL_DRAFT_STORAGE_PREFIX}${entryId}`;
}

export function modelDraftToStored(draft: ModelDraft): StoredModelDraft {
  return {
    version: MODEL_DRAFT_VERSION,
    id: draft.id,
    title: draft.title,
    purpose: draft.purpose,
    modelType: draft.modelType,
    details: draft.details,
    authors: draft.authors,
    affiliation: draft.affiliation,
    compounds: draft.compounds,
    compoundsSource: draft.compoundsSource,
    thumbUrl: draft.thumbUrl,
    thumbPreview: draft.thumbUrl ? httpOnly(draft.thumbPreview) : undefined,
    files: draft.files.filter(isPersistable).map(fileToDraft),
    metrics: draft.metrics,
    programs: draft.programs,
  };
}

export function modelDraftFromStored(stored: StoredModelDraft): ModelDraft {
  return {
    id: stored.id,
    title: stored.title,
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: stored.thumbPreview ?? null,
    thumbUrl: stored.thumbUrl,
    thumbProgress: stored.thumbUrl ? 1 : 0,
    thumbUploadStatus: stored.thumbUrl ? "uploaded" : "idle",
    thumbUploadError: null,
    files: stored.files.map(fileFromDraft),
    metrics: stored.metrics,
    purpose: stored.purpose,
    modelType: stored.modelType,
    details: stored.details,
    authors: stored.authors,
    affiliation: stored.affiliation,
    compounds: stored.compounds,
    compoundsSource: stored.compoundsSource,
    programs: stored.programs,
  };
}

export function storedModelDraftHasContent(stored: StoredModelDraft): boolean {
  return (
    [stored.title, stored.details, stored.affiliation].some(
      (value) => value.trim().length > 0,
    ) ||
    stored.authors.length > 0 ||
    Boolean(stored.thumbUrl) ||
    stored.files.length > 0 ||
    stored.metrics.length > 0 ||
    stored.programs.length > 0 ||
    !compoundsAreUntouched(stored.compounds)
  );
}

export function readStoredModelDraft(entryId: string): StoredModelDraft | null {
  const parsed = readStoredJSON(
    modelDraftStorageKey(entryId),
  ) as Partial<StoredModelDraft> | null;
  if (!parsed || parsed.version !== MODEL_DRAFT_VERSION || !parsed.id) {
    return null;
  }
  return {
    version: MODEL_DRAFT_VERSION,
    id: parsed.id,
    title: parsed.title ?? "",
    purpose: parsed.purpose ?? "",
    modelType: parsed.modelType ?? "",
    details: parsed.details ?? "",
    authors: Array.isArray(parsed.authors) ? parsed.authors : [],
    affiliation: parsed.affiliation ?? "",
    compounds: parsed.compounds ?? emptyCompounds(),
    compoundsSource: parsed.compoundsSource ?? null,
    thumbUrl: parsed.thumbUrl ?? null,
    thumbPreview: parsed.thumbPreview,
    files: Array.isArray(parsed.files) ? parsed.files : [],
    metrics: Array.isArray(parsed.metrics) ? parsed.metrics : [],
    programs: Array.isArray(parsed.programs) ? parsed.programs : [],
  };
}

export function writeStoredModelDraft(entryId: string, draft: StoredModelDraft): void {
  writeStoredJSON(modelDraftStorageKey(entryId), draft);
}

export function clearStoredModelDraft(entryId: string): void {
  clearStoredJSON(modelDraftStorageKey(entryId));
}

// ------------------------------------------------------------- storage --

// Drafts are best effort: storage may be full, disabled or absent (server
// render), and a draft that cannot be read is treated as no draft.

function readStoredJSON(key: string): unknown {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as unknown) : null;
  } catch {
    return null;
  }
}

function writeStoredJSON(key: string, value: unknown): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Storage full or unavailable.
  }
}

function clearStoredJSON(key: string): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.removeItem(key);
  } catch {
    // ignore
  }
}
