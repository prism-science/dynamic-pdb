import type { EntityLevel } from "@/lib/api/entries";
import type { ExtFileReference } from "@/lib/api/ext";

import { DRAFT_STORAGE_KEY, DRAFT_VERSION } from "./types";
import type {
  FileSource,
  MetricDraft,
  ModelDraft,
  ParsedFile,
  ProgramDraft,
  UploadStatus,
} from "./types";
import { normalizeModelLevel } from "./helpers";

export type StoredFile = {
  id: string;
  source: FileSource;
  name: string;
  size: number;
  type: string;
  level: EntityLevel;
  authors: string;
  affiliation: string;
  metadata?: Record<string, unknown>;
  preview?: string;
  url: string;
  extReference?: ExtFileReference;
};

export type StoredModel = {
  id: string;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  files: StoredFile[];
  metrics: MetricDraft[];
  program?: ProgramDraft | null;
};

export type StoredDraft = {
  version: number;
  entryId: string;
  extExperimentId?: string | null;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  thumbUploadStatus: UploadStatus;
  files: StoredFile[];
  models: StoredModel[];
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
    level: file.level,
    authors: file.authors,
    affiliation: file.affiliation,
    metadata: file.metadata,
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
    level: file.level,
    authors: file.authors,
    affiliation: file.affiliation,
    metadata: file.metadata,
    preview: file.preview,
    url: file.url,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    extReference: file.extReference,
  };
}

export function modelToDraft(modelDraft: ModelDraft): StoredModel {
  return {
    id: modelDraft.id,
    name: modelDraft.name,
    description: modelDraft.description,
    thumbUrl: modelDraft.thumbUrl,
    thumbPreview: httpOnly(modelDraft.thumbPreview),
    files: modelDraft.files.filter(isPersistable).map(fileToDraft),
    metrics: modelDraft.metrics,
    program: modelDraft.program,
  };
}

export function modelFromDraft(modelDraft: StoredModel): ModelDraft {
  return {
    id: modelDraft.id,
    name: modelDraft.name,
    description: modelDraft.description,
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: modelDraft.thumbPreview ?? null,
    thumbUrl: modelDraft.thumbUrl,
    thumbProgress: modelDraft.thumbUrl ? 1 : 0,
    thumbUploadStatus: modelDraft.thumbUrl ? "uploaded" : "idle",
    thumbUploadError: null,
    files: modelDraft.files.map(fileFromDraft).map(normalizeModelLevel),
    metrics: modelDraft.metrics,
    program: modelDraft.program ?? null,
  };
}

export function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.name.trim().length > 0 ||
    draft.description.trim().length > 0 ||
    Boolean(draft.extExperimentId) ||
    Boolean(draft.thumbUrl) ||
    draft.files.length > 0 ||
    draft.models.length > 0
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
    parsed.models = Array.isArray(parsed.models)
      ? parsed.models
      : [];
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
