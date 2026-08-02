import type { EntityLevel } from "@/lib/api/structures";
import type { ExtFileReference } from "@/lib/api/ext";

export type UploadStatus = "idle" | "uploading" | "uploaded" | "failed";

export type FileSource = "upload" | "url" | "ext";

export type ParsedFile = {
  id: string;
  file?: File;
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
  progress: number;
  uploadStatus: UploadStatus;
  uploadError: string | null;
  extReference?: ExtFileReference;
};

export type MetricDraft = {
  id: string;
  values: Record<string, string>;
};

export type ProgramDraft = {
  id: string;
  name: string;
  version: string;
  description: string;
};

export type ModelDraft = {
  id: string;
  name: string;
  description: string;
  thumbFileId: string;
  thumbFile: File | null;
  thumbPreview: string | null;
  thumbUrl: string | null;
  thumbProgress: number;
  thumbUploadStatus: UploadStatus;
  thumbUploadError: string | null;
  files: ParsedFile[];
  metrics: MetricDraft[];
  program: ProgramDraft | null;
};

export const LEVELS: EntityLevel[] = ["L0", "L1", "L2", "L3"];

export const METRIC_FIELDS: { key: string; label: string; example: string }[] = [
  { key: "r_work", label: "R-work", example: "e.g. 0.196" },
  { key: "r_free", label: "R-free", example: "e.g. 0.231" },
  { key: "rscc", label: "RSCC", example: "e.g. 0.96" },
  { key: "cc", label: "CC", example: "e.g. 0.98" },
];

export const DRAFT_STORAGE_KEY = "dpdb:new-structure-draft";
export const DRAFT_VERSION = 1;
