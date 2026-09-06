import type { ArtifactType, EntityLevel } from "@/lib/api/entries";
import type { ExtFileReference } from "@/lib/api/ext";

import { DRAFT_STORAGE_KEY, DRAFT_VERSION } from "./types";
import type {
  EntryMetadataDraft,
  FileSource,
  MetricDraft,
  ModelDraft,
  ParsedFile,
  ProgramDraft,
  UploadStatus,
} from "./types";
import { assignCanonicalArtifactTypes } from "./helpers";

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

export type StoredModel = {
  id: string;
  title: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  files: StoredFile[];
  metrics: MetricDraft[];
  purpose?: string;
  modelType?: string;
  programs?: ProgramDraft[];
  /** Version 1 shape, read once so an in-flight draft is not thrown away. */
  program?: ProgramDraft | null;
};

export type StoredDraft = {
  version: number;
  entryId: string;
  metadata?: EntryMetadataDraft;
  extExperimentId?: string | null;
  title: string;
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

export function modelToDraft(modelDraft: ModelDraft): StoredModel {
  return {
    id: modelDraft.id,
    title: modelDraft.title,
    thumbUrl: modelDraft.thumbUrl,
    thumbPreview: httpOnly(modelDraft.thumbPreview),
    files: modelDraft.files.filter(isPersistable).map(fileToDraft),
    metrics: modelDraft.metrics,
    purpose: modelDraft.purpose,
    modelType: modelDraft.modelType,
    programs: modelDraft.programs,
  };
}

export function modelFromDraft(modelDraft: StoredModel): ModelDraft {
  return {
    id: modelDraft.id,
    title: modelDraft.title,
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: modelDraft.thumbPreview ?? null,
    thumbUrl: modelDraft.thumbUrl,
    thumbProgress: modelDraft.thumbUrl ? 1 : 0,
    thumbUploadStatus: modelDraft.thumbUrl ? "uploaded" : "idle",
    thumbUploadError: null,
    files: assignCanonicalArtifactTypes(
      [],
      modelDraft.files.map(fileFromDraft),
      "model",
    ),
    metrics: modelDraft.metrics,
    purpose: modelDraft.purpose ?? "",
    modelType: modelDraft.modelType ?? "",
    programs: storedPrograms(modelDraft),
  };
}

// A version 1 draft carried a single `program` with no links. Its one run took
// every data file and produced the model, so that is what it is restored as —
// the same graph the old form would have submitted.
function storedPrograms(modelDraft: StoredModel): ProgramDraft[] {
  if (modelDraft.programs) {
    return modelDraft.programs;
  }
  const legacy = modelDraft.program;
  if (!legacy) {
    return [];
  }
  const files = modelDraft.files;
  const isModel = (file: StoredFile) =>
    file.artifactType === "model" ||
    (file.artifactType == null &&
      (file.type === "pdb" || file.type === "mmcif"));
  return [
    {
      ...legacy,
      inputFileIds: files.filter((file) => !isModel(file)).map((file) => file.id),
      outputFileIds: files.filter(isModel).map((file) => file.id),
      expectedInputs: [],
      origin: "manual" as const,
    },
  ];
}

export function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.title.trim().length > 0 ||
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
