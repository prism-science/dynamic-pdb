import { basename } from "@/lib/parse/runLog";

import type { MetricKey } from "@/lib/api/entries";

import { compoundsAreUntouched, compoundsFromParsed } from "./compounds";
import { emptyHeaderFills } from "./headerFills";
import { emptyProgramDraft, isModelFile } from "./helpers";
import type {
  ExpectedInput,
  HeaderFill,
  MetricDraft,
  ModelDraft,
  ParsedFile,
  ProgramDraft,
} from "./types";
import { METRIC_FIELDS } from "./types";

// Reading a dropped file for what it already says about itself.
//
// Everything here is additive: a value only lands in a field that is still
// empty, and an existing program is extended rather than replaced. A depositor
// who typed something must never watch it get overwritten by a parser.
//
// A structure file's header describes the model only when it is the model's
// own coordinate file. Any other structure file, such as the archive's mmCIF
// dropped as an input of a run, fills nothing of the model: its program,
// authors, composition and scores belong to another model (ticket 3: archive
// scores are never assigned to a submitted model). Each value filled from the
// model file is recorded in headerFills, so the request can name the file as
// its source.

// phenix logs run to tens of megabytes. Only the head is read — the banner,
// the echoed parameter block and the written-file list all live near the top —
// so a large log costs a slice, not an upload.
const LOG_HEAD_BYTES = 512 * 1024;
const STRUCTURE_HEAD_BYTES = 4 * 1024 * 1024;

export type DetectionResult = {
  programs: ProgramDraft[];
  metrics: MetricDraft[];
  metadata: Record<string, unknown>;
  /** Human-readable summary of what was read, for the form to show. */
  notes: string[];
};

export async function detectFromFile(
  file: File,
  parsedFile: ParsedFile,
  draft: ModelDraft,
): Promise<Partial<ModelDraft> & { notes: string[] }> {
  const { detectStructureFormat, parseStructureFacts } = await import(
    "@/lib/parse/structure"
  );
  const { looksLikeRunLog, parseRunLog } = await import("@/lib/parse/runLog");

  const format = detectStructureFormat(file.name);
  if (format) {
    const text = await readHead(file, STRUCTURE_HEAD_BYTES);
    const facts = parseStructureFacts(text, format);
    return applyStructure(draft, parsedFile, facts, file.name);
  }

  if (looksLikeRunLog(file.name)) {
    const text = await readHead(file, LOG_HEAD_BYTES);
    const facts = parseRunLog(text);
    if (facts) {
      return applyRunLog(draft, parsedFile, facts, file.name);
    }
  }

  return { notes: [] };
}

async function readHead(file: File, bytes: number): Promise<string> {
  const slice = file.size > bytes ? file.slice(0, bytes) : file;
  return slice.text();
}

/* ---------------------------------------------------------- structure -- */

function applyStructure(
  draft: ModelDraft,
  parsedFile: ParsedFile,
  facts: import("@/lib/parse/structure").StructureFacts,
  fileName: string,
): Partial<ModelDraft> & { notes: string[] } {
  const notes: string[] = [];
  const patch: Partial<ModelDraft> & { notes: string[] } = { notes };

  // The file describes itself whatever its role, so its own metadata is kept.
  if (Object.keys(facts.metadata).length > 0) {
    patch.files = draft.files.map((file) =>
      file.id === parsedFile.id
        ? { ...file, metadata: mergeFileMetadata(facts.metadata, file.metadata) }
        : file,
    );
    notes.push(`Composition and cell — from ${fileName}`);
  }
  if (!isModelFile(parsedFile)) {
    return patch;
  }

  const fills = draft.headerFills ?? emptyHeaderFills();
  const fill = <T>(value: T): HeaderFill<T> => ({ fileId: parsedFile.id, fileName, value });

  if (facts.program) {
    const programs = mergeProgram(draft.programs, {
      name: facts.program.name,
      version: facts.program.version ?? "",
      originFile: fileName,
      outputFileIds: [parsedFile.id],
    });
    patch.programs = programs;
    notes.push(
      `${facts.program.name}${facts.program.version ? ` ${facts.program.version}` : ""} — from ${fileName}`,
    );
  }

  // Only ever fills an empty field: a depositor who already typed the author
  // list must not watch a header overwrite it.
  let authorsFill = fills.authors;
  if (facts.authors.length > 0 && draft.authors.length === 0) {
    patch.authors = facts.authors;
    authorsFill = fill(facts.authors);
    notes.push(
      `${facts.authors.length} author${facts.authors.length > 1 ? "s" : ""} — from ${fileName}`,
    );
  }

  // The composition lists start from the coordinates, with the compounds the
  // parser is unsure about marked for the depositor to decide. Lists the
  // depositor already touched are theirs.
  if (
    (facts.metadata.ligands || facts.metadata.cofactors) &&
    compoundsAreUntouched(draft.compounds)
  ) {
    patch.compounds = compoundsFromParsed(facts.metadata, facts.needsReview);
    patch.compoundsSource = fileName;
  }

  const metricFills = { ...fills.metrics };
  const merged = mergeMetrics(draft.metrics, facts.metrics);
  if (merged) {
    patch.metrics = merged.metrics;
    for (const [key, value] of Object.entries(merged.filled) as [MetricKey, string][]) {
      metricFills[key] = fill(value);
    }
    notes.push(`R-factors — from ${fileName}`);
  }

  patch.headerFills = { metrics: metricFills, authors: authorsFill };
  return patch;
}

/**
 * Fills the file's metadata from what the header says without overwriting what
 * is already there. `extensions` is an open object, so it is merged key by key
 * rather than replaced: the compound review flags are added next to whatever
 * extensions the form already holds.
 */
export function mergeFileMetadata(
  found: Record<string, unknown>,
  existing: Record<string, unknown> | undefined,
): Record<string, unknown> {
  const merged = { ...found, ...(existing ?? {}) };
  const foundExtensions = found.extensions;
  const existingExtensions = existing?.extensions;
  if (isPlainObject(foundExtensions) && isPlainObject(existingExtensions)) {
    merged.extensions = { ...foundExtensions, ...existingExtensions };
  }
  return merged;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/* -------------------------------------------------------------- log -- */

function applyRunLog(
  draft: ModelDraft,
  parsedFile: ParsedFile,
  facts: import("@/lib/parse/runLog").RunLogFacts,
  fileName: string,
): Partial<ModelDraft> & { notes: string[] } {
  const byName = new Map(
    draft.files.map((file) => [basename(file.name).toLowerCase(), file.id]),
  );

  const inputIds: string[] = [];
  const expected: ExpectedInput[] = [];
  for (const name of facts.inputs) {
    const match = byName.get(name.toLowerCase());
    if (match) {
      inputIds.push(match);
    } else {
      expected.push({ id: crypto.randomUUID(), name, source: fileName });
    }
  }

  const outputIds = facts.outputs
    .map((name) => byName.get(name.toLowerCase()))
    .filter((id): id is string => Boolean(id));

  const programs = mergeProgram(draft.programs, {
    name: facts.program.name,
    version: facts.program.version ?? "",
    originFile: fileName,
    inputFileIds: inputIds,
    outputFileIds: outputIds,
    expectedInputs: expected,
    // The log is evidence for the step, so it is filed as one of its outputs.
    logFileId: parsedFile.id,
  });

  const notes = [
    `${facts.program.name} — from ${fileName}`,
    ...(expected.length > 0
      ? [`${expected.length} input${expected.length > 1 ? "s" : ""} named but not uploaded`]
      : []),
  ];

  return { programs, notes };
}

/* ----------------------------------------------------------- merging -- */

type ProgramPatch = {
  name: string;
  version: string;
  originFile: string;
  inputFileIds?: string[];
  outputFileIds?: string[];
  expectedInputs?: ExpectedInput[];
  logFileId?: string;
};

/**
 * A model file and its log describe the same run from two sides. Matching by
 * name keeps them one program instead of two, and a program the user typed
 * keeps its own name.
 */
function mergeProgram(
  programs: ProgramDraft[],
  patch: ProgramPatch,
): ProgramDraft[] {
  const key = normalizeName(patch.name);
  const index = programs.findIndex(
    (program) => normalizeName(program.name) === key,
  );

  const outputs = [...(patch.outputFileIds ?? [])];
  if (patch.logFileId) {
    outputs.push(patch.logFileId);
  }

  if (index === -1) {
    return [
      ...programs,
      emptyProgramDraft({
        name: patch.name,
        version: patch.version,
        origin: "parsed",
        originFile: patch.originFile,
        inputFileIds: unique(patch.inputFileIds ?? []),
        outputFileIds: unique(outputs),
        expectedInputs: patch.expectedInputs ?? [],
      }),
    ];
  }

  const existing = programs[index];
  const merged: ProgramDraft = {
    ...existing,
    version: existing.version || patch.version,
    originFile: existing.originFile ?? patch.originFile,
    inputFileIds: unique([...existing.inputFileIds, ...(patch.inputFileIds ?? [])]),
    outputFileIds: unique([...existing.outputFileIds, ...outputs]),
    expectedInputs: mergeExpected(
      existing.expectedInputs,
      patch.expectedInputs ?? [],
    ),
  };
  return programs.map((program, at) => (at === index ? merged : program));
}

function mergeExpected(
  existing: ExpectedInput[],
  incoming: ExpectedInput[],
): ExpectedInput[] {
  const seen = new Set(existing.map((item) => item.name.toLowerCase()));
  return [
    ...existing,
    ...incoming.filter((item) => !seen.has(item.name.toLowerCase())),
  ];
}

/** The metrics with each empty score filled from the header, and the values
 *  filled, by key; null when nothing was filled. */
function mergeMetrics(
  metrics: MetricDraft[],
  values: { r_work?: number; r_free?: number },
): { metrics: MetricDraft[]; filled: Partial<Record<MetricKey, string>> } | null {
  const found = Object.entries(values).filter(([, value]) => value !== undefined);
  if (found.length === 0) {
    return null;
  }
  const known = new Set<string>(METRIC_FIELDS.map((field) => field.key));
  const target = metrics[0] ?? { id: crypto.randomUUID(), values: {} };
  const next = { ...target, values: { ...target.values } };
  const filled: Partial<Record<MetricKey, string>> = {};
  for (const [key, value] of found) {
    if (!known.has(key) || next.values[key]) {
      continue;
    }
    next.values[key] = String(value);
    filled[key as MetricKey] = String(value);
  }
  if (Object.keys(filled).length === 0) {
    return null;
  }
  return {
    metrics: metrics.length === 0 ? [next] : metrics.map((m, i) => (i === 0 ? next : m)),
    filled,
  };
}

/**
 * `PHENIX`, `phenix.refine` and `Phenix` are one program as far as a deposit is
 * concerned: the header shouts the suite name while the log names the command.
 */
function normalizeName(name: string): string {
  return name.trim().toLowerCase().split(".")[0];
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

/**
 * Re-run matching after new files arrive: a placeholder whose file has since
 * been uploaded turns into a real input.
 */
export function resolveExpectedInputs(draft: ModelDraft): ProgramDraft[] {
  const byName = new Map(
    draft.files.map((file) => [basename(file.name).toLowerCase(), file.id]),
  );
  let changed = false;
  const programs = draft.programs.map((program) => {
    const stillMissing: ExpectedInput[] = [];
    const resolved: string[] = [];
    for (const expected of program.expectedInputs) {
      const match = byName.get(expected.name.toLowerCase());
      if (match) {
        resolved.push(match);
      } else {
        stillMissing.push(expected);
      }
    }
    if (resolved.length === 0) {
      return program;
    }
    changed = true;
    return {
      ...program,
      inputFileIds: unique([...program.inputFileIds, ...resolved]),
      expectedInputs: stillMissing,
    };
  });
  return changed ? programs : draft.programs;
}
