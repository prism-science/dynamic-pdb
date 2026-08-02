import type {
  CreateEntityInput,
  CreateEntityRelationInput,
  CreateModelInput,
  EntityLevel,
} from "@/lib/api/entries";
import { extFileKey, extFileReferenceURL, type ExtFile } from "@/lib/api/ext";
import {
  uploadFileToObjectStorage,
  type FileUploadContext,
} from "@/lib/api/uploads";

import { METRIC_FIELDS } from "./types";
import type {
  MetricDraft,
  ModelDraft,
  ParsedFile,
  ProgramDraft,
  UploadStatus,
} from "./types";

export const ONE_MODEL_FILE_ERROR =
  "Each model must contain exactly one PDB/mmCIF model file.";

export function toEntity(file: ParsedFile) {
  const entityType = fileEntityType(file);
  const metadata = file.extReference
    ? {
        ...(file.metadata ?? {}),
        _dynamic_pdb_source: {
          provider: "ext",
          experiment_id: file.extReference.experimentId,
          path: file.extReference.path,
          sha256: file.extReference.sha256,
        },
      }
    : file.metadata;
  const payload: Record<string, unknown> = {
    file_url: file.url,
    ...(file.size > 0 ? { size: file.size } : {}),
    ...(metadata ? { metadata } : {}),
  };
  if (entityType === "data") {
    payload.type = file.type;
  }
  const authors = authorsTextToList(file.authors);
  if (authors.length > 0) {
    payload.authors = authors;
  }
  const affiliation = file.affiliation.trim();
  if (affiliation) {
    payload.affiliation = affiliation;
  }

  return {
    id: file.id,
    type: entityType,
    level: entityType === "model" ? "L2" : file.level,
    name: file.name,
    payload,
  };
}

export function buildCreateModelInput(
  modelDraft: ModelDraft,
  options: {
    /**
     * Ids of entities that already exist on the entry and should be recorded as
     * inputs to this model's program — used when adding a model to an entry
     * whose baseline data was deposited earlier.
     */
    programInputEntityIds?: string[];
  } = {},
): CreateModelInput {
  const modelEntityFiles = modelDraft.files.filter(isModelFile);
  if (modelEntityFiles.length !== 1) {
    throw new Error(ONE_MODEL_FILE_ERROR);
  }
  const programMessage = programValidationMessage(modelDraft.program);
  if (programMessage) {
    throw new Error(programMessage);
  }

  const fileEntities = modelDraft.files.map(toEntity);
  const metricsEntities = modelDraft.metrics.map(toMetricEntity);
  const programEntity = modelDraft.program
    ? toProgramEntity(modelDraft.program)
    : null;
  const entities: CreateEntityInput[] = [
    ...fileEntities,
    ...metricsEntities,
    ...(programEntity ? [programEntity] : []),
  ];
  const canonicalModelEntity = fileEntities.find(
    (entity) => entity.id === modelEntityFiles[0].id,
  );
  if (!canonicalModelEntity) {
    throw new Error(ONE_MODEL_FILE_ERROR);
  }

  const relations: CreateEntityRelationInput[] = metricsEntities.map(
    (metricEntity) => ({
      source_entity_id: metricEntity.id,
      target_entity_id: canonicalModelEntity.id,
      relation_type: "metrics_for",
    }),
  );
  if (programEntity) {
    for (const entity of fileEntities) {
      if (entity.type === "data") {
        relations.push({
          source_entity_id: entity.id,
          target_entity_id: programEntity.id,
          relation_type: "input_to",
        });
      }
    }
    for (const entityId of options.programInputEntityIds ?? []) {
      relations.push({
        source_entity_id: entityId,
        target_entity_id: programEntity.id,
        relation_type: "input_to",
      });
    }
    relations.push({
      source_entity_id: canonicalModelEntity.id,
      target_entity_id: programEntity.id,
      relation_type: "output_of",
    });
  }

  return {
    id: modelDraft.id,
    name: modelDraft.name.trim(),
    description: modelDraft.description.trim() || null,
    thumbnail_image_url: modelDraft.thumbUrl,
    entities,
    relations,
  };
}

export function modelValidationMessage(modelDraft: ModelDraft): string | null {
  const modelEntityCount = modelDraft.files.filter(isModelFile).length;
  if (modelEntityCount === 0) {
    return "Add exactly one PDB/mmCIF model file.";
  }
  if (modelEntityCount > 1) {
    return "Keep only one PDB/mmCIF model file.";
  }
  return programValidationMessage(modelDraft.program);
}

export function programValidationMessage(program: ProgramDraft | null): string | null {
  if (!program) {
    return null;
  }
  if (
    !program.name.trim() ||
    !program.version.trim() ||
    !program.description.trim()
  ) {
    return "Fill program name, version, and description or remove the program.";
  }
  return null;
}

export function toProgramEntity(program: ProgramDraft): CreateEntityInput {
  const name = program.name.trim();
  return {
    id: program.id,
    type: "program",
    level: null,
    name,
    payload: {
      name,
      version: program.version.trim(),
      description: program.description.trim(),
    },
  };
}

export function fileEntityType(file: ParsedFile): "data" | "model" {
  return file.type === "pdb" || file.type === "mmcif" ? "model" : "data";
}

export function isModelFile(file: ParsedFile): boolean {
  return fileEntityType(file) === "model";
}

export function canAddModelFiles(
  currentFiles: ParsedFile[],
  nextFiles: ParsedFile[],
): boolean {
  return (
    currentFiles.filter(isModelFile).length +
      nextFiles.filter(isModelFile).length <=
    1
  );
}

export function setFileLevel(file: ParsedFile, level: EntityLevel): ParsedFile {
  return {
    ...file,
    level: isModelFile(file) ? "L2" : level,
  };
}

export function authorsTextToList(text: string): string[] {
  return text
    .split(/\r?\n|;/)
    .map((author) => author.trim())
    .filter(Boolean);
}

// Restrict metric input to a single decimal number. Commas are treated as the
// decimal separator and normalized to a dot, so "0,205" and "0.205" both store
// as "0.205"; anything non-numeric is dropped as you type.
export function sanitizeNumeric(raw: string): string {
  let s = raw.replace(/,/g, ".").replace(/[^0-9.\-]/g, "");
  const firstDot = s.indexOf(".");
  if (firstDot !== -1) {
    s = s.slice(0, firstDot + 1) + s.slice(firstDot + 1).replace(/\./g, "");
  }
  s = s.replace(/(?!^)-/g, "");
  return s;
}

export function toMetricEntity(metric: MetricDraft) {
  const payload: Record<string, number> = {};
  for (const field of METRIC_FIELDS) {
    const raw = metric.values[field.key];
    const value = raw != null && raw.trim() !== "" ? Number(raw) : NaN;
    if (!Number.isNaN(value)) {
      payload[field.key] = value;
    }
  }
  return {
    id: metric.id,
    type: "metrics" as const,
    level: "L3" as const,
    name: "Metrics",
    payload,
  };
}

export async function parseFile(file: File, level: EntityLevel): Promise<ParsedFile> {
  const type = detectType(file.name);
  const base: ParsedFile = {
    id: crypto.randomUUID(),
    file,
    source: "upload",
    name: file.name,
    size: file.size,
    type,
    level,
    authors: "",
    affiliation: "",
    url: "",
    progress: 0,
    uploadStatus: "uploading",
    uploadError: null,
  };
  if (type === "fasta") {
    try {
      base.metadata = parseFasta(await file.text());
    } catch {
      // keep the file without metadata on parse failure
    }
  }
  if (type === "image") {
    base.preview = URL.createObjectURL(file);
  }
  return base;
}

export async function parseModelFile(file: File): Promise<ParsedFile> {
  return normalizeModelLevel(await parseFile(file, "L2"));
}

export function extFileToParsed(
  file: ExtFile,
  experimentId: string,
  level: EntityLevel,
): ParsedFile {
  return {
    id: crypto.randomUUID(),
    source: "ext",
    name: file.name || file.path.split("/").filter(Boolean).pop() || "ext-file",
    size: file.size,
    type: detectType(file.name || file.path),
    level,
    authors: "",
    affiliation: "",
    metadata: file.metadata,
    url: extFileReferenceURL(experimentId, file),
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    extReference: {
      experimentId,
      path: file.path,
      sha256: file.sha256,
    },
  };
}

export function extFileKeys(files: ParsedFile[]): Set<string> {
  return new Set(
    files.flatMap((file) =>
      file.extReference
        ? [
            extFileKey({
              path: file.extReference.path,
              sha256: file.extReference.sha256,
            }),
          ]
        : [],
    ),
  );
}

// Builds a file entry from an external URL. Nothing is uploaded — the link is
// stored as-is; a small, known-format file is fetched client-side (best effort)
// to build a preview. CORS failures are swallowed and just skip the preview.
export async function parseUrlFile(
  rawUrl: string,
  level: EntityLevel,
): Promise<ParsedFile | null> {
  const url = rawUrl.trim();
  if (!isValidHttpUrl(url)) {
    return null;
  }
  const name = fileNameFromUrl(url);
  const type = detectType(name);
  const base: ParsedFile = {
    id: crypto.randomUUID(),
    source: "url",
    name,
    size: 0,
    type,
    level,
    authors: "",
    affiliation: "",
    url,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };

  try {
    if (type === "image") {
      base.preview = url;
    } else if (type === "fasta") {
      const response = await fetch(url);
      if (response.ok) {
        const declared = Number(response.headers.get("content-length") ?? "0");
        if (!declared || declared < 2_000_000) {
          const text = await response.text();
          if (text.length < 4_000_000) {
            base.size = text.length;
            base.metadata = parseFasta(text);
          }
        }
      }
    }
  } catch {
    // Network/CORS error — keep the link without a preview.
  }
  return base;
}

export async function parseModelUrlFile(
  rawUrl: string,
): Promise<ParsedFile | null> {
  const parsed = await parseUrlFile(rawUrl, "L2");
  return parsed ? normalizeModelLevel(parsed) : null;
}

export function normalizeModelLevel(file: ParsedFile): ParsedFile {
  return isModelFile(file) ? { ...file, level: "L2" } : file;
}

export function fileNameFromUrl(url: string): string {
  try {
    const parsed = new URL(url);
    const last = parsed.pathname.split("/").filter(Boolean).pop();
    return last ? decodeURIComponent(last) : parsed.hostname;
  } catch {
    return "linked-file";
  }
}

// Only claim a drag when it actually carries files, so dragging selected text
// or a link across the form doesn't light up the drop zone.
export function hasDraggedFiles(transfer: DataTransfer | null): boolean {
  if (!transfer) {
    return false;
  }
  const types = Array.from(transfer.types ?? []);
  return types.includes("Files");
}

// Dropping a folder yields a 0-byte File that would fail to upload, so keep
// only real files. The entries API is best-effort; without it, size is the
// only signal available.
export function droppedFiles(transfer: DataTransfer | null): File[] {
  if (!transfer) {
    return [];
  }
  const items = Array.from(transfer.items ?? []);
  const directoryNames = new Set(
    items.flatMap((item) => {
      const entry = item.webkitGetAsEntry?.();
      return entry && !entry.isFile ? [entry.name] : [];
    }),
  );
  return Array.from(transfer.files ?? []).filter(
    (file) => !directoryNames.has(file.name),
  );
}

export function isValidHttpUrl(value: string): boolean {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

type FileUploadLocation = Pick<FileUploadContext, "entryId" | "modelId">;

export async function uploadParsedFileNow(
  file: ParsedFile,
  location: FileUploadLocation,
  patchFile: (id: string, patch: Partial<ParsedFile>) => void,
): Promise<void> {
  if (!file.file) {
    return;
  }
  try {
    const url = await uploadFileToObjectStorage(
      file.file,
      { ...location, entityId: file.id, filename: file.name },
      (fraction) => patchFile(file.id, { progress: fraction }),
    );
    patchFile(file.id, {
      url,
      progress: 1,
      uploadStatus: "uploaded",
      uploadError: null,
    });
  } catch (error) {
    patchFile(file.id, {
      uploadStatus: "failed",
      uploadError: uploadErrorMessage(error),
    });
  }
}

export function uploadErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Upload failed.";
}

export function uploadStatusText(status: UploadStatus, error: string | null): string {
  if (status === "uploading") {
    return "Uploading...";
  }
  if (status === "uploaded") {
    return "Uploaded";
  }
  if (status === "failed") {
    return error ? `Upload failed: ${error}` : "Upload failed";
  }
  return "";
}

export function uploadsReady(
  files: ParsedFile[],
  models: ModelDraft[],
  thumbFile: File | null,
  thumbUrl: string | null,
): boolean {
  if (thumbFile && !thumbUrl) {
    return false;
  }
  if (files.some((file) => file.uploadStatus !== "uploaded" || !file.url)) {
    return false;
  }
  return models.every(
    (modelDraft) =>
      (!modelDraft.thumbFile || Boolean(modelDraft.thumbUrl)) &&
      modelDraft.files.every(
        (file) => file.uploadStatus === "uploaded" && Boolean(file.url),
      ),
  );
}

export function detectType(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase() ?? "";
  const map: Record<string, string> = {
    fasta: "fasta",
    fa: "fasta",
    faa: "fasta",
    seq: "fasta",
    pdb: "pdb",
    cif: "mmcif",
    mmcif: "mmcif",
    ccp4: "ccp4",
    mrc: "mrc",
    map: "ccp4",
    mtz: "mtz",
    h5: "hdf5",
    hdf5: "hdf5",
    cbf: "cbf",
    png: "image",
    jpg: "image",
    jpeg: "image",
    log: "log",
    txt: "text",
    json: "json",
    csv: "csv",
  };
  return map[ext] ?? ext ?? "file";
}

export function parseFasta(text: string): Record<string, unknown> {
  const records: Array<{
    header: string;
    sequence: string;
  }> = [];
  let header = "";
  let sequence = "";

  function appendRecord() {
    const normalizedSequence = sequence.replace(/\s+/g, "").toUpperCase();
    if (normalizedSequence.length > 0) {
      records.push({
        header,
        sequence: normalizedSequence,
      });
    }
    sequence = "";
  }

  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line.startsWith(">")) {
      appendRecord();
      header = line.slice(1).trim();
      continue;
    }
    sequence += line;
  }
  appendRecord();

  return {
    records,
  };
}

export function formatSize(size: number): string {
  if (size <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? value : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}
