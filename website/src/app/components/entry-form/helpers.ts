import type {
  CreateEntityInput,
  CreateEntityRelationInput,
  CreateEntryInput,
  CreateModelInput,
  ArtifactType,
  EntityLevel,
} from "@/lib/api/entries";
import { extFileKey, extFileReferenceURL, type ExtFile } from "@/lib/api/ext";
import {
  uploadFileToObjectStorage,
  type FileUploadContext,
} from "@/lib/api/uploads";

import { compoundListRequest, compoundReviewRequest } from "./compounds";
import { crystallographyRequest, optionalNumber } from "./crystals";
import { polymerEntitiesRequest } from "./polymerEntities";
import { METRIC_FIELDS } from "./types";
import type {
  EntryMetadataDraft,
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
  const sha256 = file.sha256 ?? file.extReference?.sha256;
  if (sha256) {
    payload.sha256 = sha256;
  }
  if (entityType === "data") {
    payload.type = file.type;
  }
  const authors = authorsTextToList(file.authors);
  if (authors.length > 0) {
    payload.authors = authors;
  }

  return {
    id: file.id,
    type: entityType,
    artifact_type: file.artifactType,
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
  const artifactTypeMessage = modelArtifactTypeValidationMessage(
    modelDraft.files,
  );
  if (artifactTypeMessage) {
    throw new Error(artifactTypeMessage);
  }
  const programMessage = programsValidationMessage(modelDraft.programs);
  if (programMessage) {
    throw new Error(programMessage);
  }

  const fileEntities = modelDraft.files.map(toEntity);
  const metricsEntities = modelDraft.metrics.map(toMetricEntity);
  const programEntities = modelDraft.programs.map(toProgramEntity);
  const entities: CreateEntityInput[] = [
    ...fileEntities,
    ...metricsEntities,
    ...programEntities,
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

  // Edges come from what each run says it touched. Nothing is inferred from
  // file type any more: a run that emits a density map records it as an
  // output, where the old rule would have filed it as an input.
  const known = new Set(fileEntities.map((entity) => entity.id));
  const baseline = options.programInputEntityIds ?? [];

  for (const program of modelDraft.programs) {
    for (const fileId of program.inputFileIds) {
      if (known.has(fileId) || baseline.includes(fileId)) {
        relations.push({
          source_entity_id: fileId,
          target_entity_id: program.id,
          relation_type: "input_to",
        });
      }
    }
    for (const fileId of program.outputFileIds) {
      if (known.has(fileId)) {
        relations.push({
          source_entity_id: fileId,
          target_entity_id: program.id,
          relation_type: "output_of",
        });
      }
    }
  }

  return {
    title: modelDraft.title.trim(),
    thumbnail_image_url: modelDraft.thumbUrl,
    entities,
    relations,
    metadata: modelMetadataInput(modelDraft),
  };
}

/**
 * The model metadata the depositor states. It is merged over what the
 * coordinate file says, so the composition lists are always set, even to
 * undefined: the form's lists, prefilled from the file and then edited, are
 * the ones to keep. For an uploaded file they are set to undefined, so neither
 * the form's lists nor the browser parser's are sent: the server reads the
 * file's compounds itself.
 */
function modelMetadataInput(modelDraft: ModelDraft): Record<string, unknown> {
  const metadata: Record<string, unknown> = {};
  if (modelDraft.purpose.trim()) {
    metadata.purpose = modelDraft.purpose.trim();
  }
  if (modelDraft.modelType.trim()) {
    metadata.model_type = modelDraft.modelType.trim();
  }
  if (modelDraft.details.trim()) {
    metadata.details = modelDraft.details.trim();
  }
  const authors = modelDraft.authors.map((author) => author.trim()).filter(Boolean);
  if (authors.length > 0) {
    metadata.authors = authors;
  }
  if (modelDraft.affiliation.trim()) {
    metadata.affiliation = modelDraft.affiliation.trim();
  }
  if (modelFileIsStored(modelDraft.files)) {
    metadata.ligands = undefined;
    metadata.cofactors = undefined;
    metadata.extensions = { compound_review: undefined };
    return metadata;
  }
  const review = compoundReviewRequest(modelDraft.compounds);
  metadata.ligands = compoundListRequest(modelDraft.compounds.ligands);
  metadata.cofactors = compoundListRequest(modelDraft.compounds.cofactors);
  metadata.extensions = {
    compound_review: review.length > 0 ? review : undefined,
  };
  return metadata;
}

export function buildCreateEntryInput(
  title: string,
  metadata: EntryMetadataDraft,
  thumbnailUrl: string | null,
  files: ParsedFile[],
): CreateEntryInput {
  const pdb = metadata.pdb.trim().toUpperCase();
  return {
    title: title.trim(),
    thumbnail_image_url: thumbnailUrl,
    details: metadata.details.trim() || undefined,
    external_refs: pdb ? { pdb } : undefined,
    method: metadata.method.trim(),
    resolution: optionalNumber(metadata.resolution),
    space_group: metadata.spaceGroup.trim() || undefined,
    crystallography: crystallographyRequest(metadata.crystals),
    ligands: compoundListRequest(metadata.compounds.ligands),
    cofactors: compoundListRequest(metadata.compounds.cofactors),
    // Typed polymer-entity values only take effect on entities the PDB
    // lookup found, so they are sent only with a PDB ID.
    polymer_entities: pdb
      ? polymerEntitiesRequest(metadata.polymerEntities)
      : undefined,
    entities: files.map(toEntity),
  };
}

/** A run's settings as the API takes them: one JSON object. Undefined when
 *  the text is blank, null when it is not an object. */
export function parseRunParameters(
  text: string,
): Record<string, unknown> | undefined | null {
  if (!text.trim()) {
    return undefined;
  }
  try {
    const value: unknown = JSON.parse(text);
    return typeof value === "object" && value !== null && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

function modelArtifactTypeValidationMessage(
  files: ParsedFile[],
): string | null {
  if (
    files.filter((file) => file.artifactType === "structure_factors").length >
    1
  ) {
    return "Keep only one structure factors file.";
  }
  return null;
}

export function programsValidationMessage(
  programs: ProgramDraft[],
): string | null {
  for (const program of programs) {
    if (!program.name.trim()) {
      return "Give every program a name or remove it.";
    }
  }
  return null;
}

/**
 * Inputs a run declared but that were never uploaded. Reported at save time as
 * a warning rather than an error: an incomplete chain is still a truthful
 * record, and refusing the deposit over it would only teach people to delete
 * the log.
 */
export function missingExpectedInputs(
  programs: ProgramDraft[],
): { program: string; name: string; source: string }[] {
  const missing: { program: string; name: string; source: string }[] = [];
  for (const program of programs) {
    for (const expected of program.expectedInputs) {
      missing.push({
        program: program.name.trim() || "a program",
        name: expected.name,
        source: expected.source,
      });
    }
  }
  return missing;
}

export function emptyProgramDraft(
  patch: Partial<ProgramDraft> = {},
): ProgramDraft {
  return {
    id: crypto.randomUUID(),
    name: "",
    version: "",
    description: "",
    command: "",
    parameters: "",
    inputFileIds: [],
    outputFileIds: [],
    expectedInputs: [],
    origin: "manual",
    ...patch,
  };
}

export function toProgramEntity(program: ProgramDraft): CreateEntityInput {
  const name = program.name.trim();
  const command = (program.command ?? "").trim();
  const parameters = parseRunParameters(program.parameters ?? "");
  return {
    id: program.id,
    type: "program",
    level: null,
    name,
    payload: {
      name,
      version: program.version.trim(),
      description: program.description.trim(),
      ...(command ? { command } : {}),
      ...(parameters ? { parameters } : {}),
    },
  };
}

export function fileEntityType(file: ParsedFile): "data" | "model" {
  return file.artifactType === "model" ? "model" : "data";
}

export function isModelFile(file: ParsedFile): boolean {
  return fileEntityType(file) === "model";
}

/**
 * Whether the model's coordinate file is uploaded to our bucket, where the
 * server reads every compound it models (T3-M). The form then neither asks for
 * nor sends the model's compound lists. A linked or external file is not read.
 */
export function modelFileIsStored(files: ParsedFile[]): boolean {
  return files.some((file) => isModelFile(file) && file.source === "upload");
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

export function assignCanonicalArtifactTypes(
  currentFiles: ParsedFile[],
  incomingFiles: ParsedFile[],
  scope: "entry" | "model",
): ParsedFile[] {
  const canonicalType: ArtifactType = scope === "entry" ? "fasta" : "model";
  let hasCanonical = currentFiles.some(
    (file) => file.artifactType === canonicalType,
  );

  return incomingFiles.map((file) => {
    let artifactType = file.artifactType;
    if (
      scope === "entry" &&
      artifactType !== "fasta" &&
      artifactType !== "other"
    ) {
      artifactType = "other";
    }
    if (scope === "model" && artifactType === "fasta") {
      artifactType = "other";
    }

    const matchesCanonicalFormat =
      scope === "entry"
        ? file.type === "fasta"
        : file.type === "pdb" || file.type === "mmcif";
    if (
      artifactType === canonicalType ||
      (!hasCanonical && matchesCanonicalFormat)
    ) {
      if (!hasCanonical) {
        hasCanonical = true;
        artifactType = canonicalType;
      } else {
        artifactType = "other";
      }
    }
    return normalizeModelLevel({ ...file, artifactType });
  });
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
  const payload: Record<string, number | string> = {};
  for (const field of METRIC_FIELDS) {
    const raw = metric.values[field.key];
    const value = raw != null && raw.trim() !== "" ? Number(raw) : NaN;
    if (!Number.isNaN(value)) {
      payload[field.key] = value;
    }
  }
  const softwareName = metric.softwareName?.trim();
  if (softwareName) {
    payload.software_name = softwareName;
  }
  const softwareVersion = metric.softwareVersion?.trim();
  if (softwareVersion) {
    payload.software_version = softwareVersion;
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
    artifactType: "other",
    level,
    authors: "",
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
  try {
    base.sha256 = await fileSHA256(file);
  } catch {
    // Browsers without Web Crypto can still submit the file without dedupe.
  }
  if (type === "image") {
    base.preview = URL.createObjectURL(file);
  }
  return base;
}

async function fileSHA256(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

export async function parseModelFile(
  file: File,
  assignAsModel = true,
): Promise<ParsedFile> {
  const parsed = await parseFile(file, "L2");
  const artifactType =
    assignAsModel && (parsed.type === "pdb" || parsed.type === "mmcif")
      ? "model"
      : "other";
  return normalizeModelLevel({ ...parsed, artifactType });
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
    artifactType: "other",
    level,
    authors: "",
    metadata: file.metadata,
    sha256: file.sha256,
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
    artifactType: "other",
    level,
    authors: "",
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
  assignAsModel = true,
): Promise<ParsedFile | null> {
  const parsed = await parseUrlFile(rawUrl, "L2");
  if (!parsed) {
    return null;
  }
  const artifactType =
    assignAsModel && (parsed.type === "pdb" || parsed.type === "mmcif")
      ? "model"
      : "other";
  return normalizeModelLevel({ ...parsed, artifactType });
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
  thumbFile: File | null,
  thumbUrl: string | null,
): boolean {
  if (thumbFile && !thumbUrl) {
    return false;
  }
  return files.every(
    (file) => file.uploadStatus === "uploaded" && Boolean(file.url),
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
