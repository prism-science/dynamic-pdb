import { basename } from "@/lib/parse/runLog";

import { emptyProgramDraft } from "./helpers";
import type { ExpectedInput, MetricDraft, ModelDraft, ParsedFile, ProgramDraft } from "./types";
import { METRIC_FIELDS } from "./types";

// Reading a dropped file for what it already says about itself.
//
// Everything here is additive: a value only lands in a field that is still
// empty, and an existing program is extended rather than replaced. A depositor
// who typed something must never watch it get overwritten by a parser.

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
  if (facts.authors.length > 0) {
    const target = draft.files.find((file) => file.id === parsedFile.id);
    if (target && !target.authors.trim()) {
      patch.files = (patch.files ?? draft.files).map((file) =>
        file.id === parsedFile.id
          ? { ...file, authors: facts.authors.join("\n") }
          : file,
      );
      notes.push(
        `${facts.authors.length} author${facts.authors.length > 1 ? "s" : ""} — from ${fileName}`,
      );
    }
  }

  const metrics = mergeMetrics(draft.metrics, facts.metrics);
  if (metrics) {
    patch.metrics = metrics;
    notes.push(`R-factors — from ${fileName}`);
  }

  if (Object.keys(facts.metadata).length > 0) {
    patch.files = (patch.files ?? draft.files).map((file) =>
      file.id === parsedFile.id
        ? { ...file, metadata: { ...facts.metadata, ...(file.metadata ?? {}) } }
        : file,
    );
    notes.push(`Composition and cell — from ${fileName}`);
  }

  return patch;
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

function mergeMetrics(
  metrics: MetricDraft[],
  values: { r_work?: number; r_free?: number },
): MetricDraft[] | null {
  const found = Object.entries(values).filter(([, value]) => value !== undefined);
  if (found.length === 0) {
    return null;
  }
  const known = new Set(METRIC_FIELDS.map((field) => field.key));
  const target = metrics[0] ?? { id: crypto.randomUUID(), values: {} };
  const next = { ...target, values: { ...target.values } };
  let touched = false;
  for (const [key, value] of found) {
    if (!known.has(key) || next.values[key]) {
      continue;
    }
    next.values[key] = String(value);
    touched = true;
  }
  if (!touched) {
    return null;
  }
  return metrics.length === 0 ? [next] : metrics.map((m, i) => (i === 0 ? next : m));
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
