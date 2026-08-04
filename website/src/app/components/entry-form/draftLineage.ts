import { computeGenerations, type Lineage, type LineageNode } from "@/lib/lineage";

import { fileEntityType, isModelFile } from "./helpers";
import type { ModelDraft, ParsedFile } from "./types";

export type DraftGraph = {
  lineage: Lineage;
  /** Files nobody has attached to a run yet. The model itself is exempt: it is
   *  the subject of the form and always belongs on the canvas. */
  unlinkedFiles: ParsedFile[];
  /** Runs with neither an input nor an output. */
  unlinkedPrograms: { id: string; name: string; version: string }[];
};

/**
 * The same graph the model page will show, built from a draft that has not
 * been submitted yet. Everything the form knows becomes a node; what a run
 * declared but nobody uploaded becomes a placeholder.
 */
export function buildDraftGraph(
  draft: ModelDraft,
  baseline: ParsedFile[] = [],
): DraftGraph {
  const files = [...baseline, ...draft.files];
  const byId = new Map(files.map((file) => [file.id, file]));
  const modelFile = draft.files.find(isModelFile) ?? null;

  const linked = new Set<string>();
  const nodes = new Map<string, LineageNode>();
  const edges = new Map<string, { id: string; source: string; target: string; onPath: boolean }>();

  const addFile = (file: ParsedFile) => {
    if (nodes.has(file.id)) {
      return;
    }
    nodes.set(file.id, {
      id: file.id,
      kind: "artifact",
      name: file.name,
      version: null,
      level: isModelFile(file) ? "L2" : file.level,
      typeLabel: fileEntityType(file),
      entity: null,
      generation: 0,
      onPath: true,
      current: file.id === modelFile?.id,
    });
  };

  const link = (source: string, target: string) => {
    const id = `${source}->${target}`;
    edges.set(id, { id, source, target, onPath: true });
  };

  const unlinkedPrograms: DraftGraph["unlinkedPrograms"] = [];

  for (const program of draft.programs) {
    const inputs = program.inputFileIds.filter((id) => byId.has(id));
    const outputs = program.outputFileIds.filter((id) => byId.has(id));
    if (
      inputs.length === 0 &&
      outputs.length === 0 &&
      program.expectedInputs.length === 0
    ) {
      unlinkedPrograms.push({
        id: program.id,
        name: program.name.trim() || "Unnamed program",
        version: program.version.trim(),
      });
      continue;
    }

    nodes.set(program.id, {
      id: program.id,
      kind: "run",
      name: program.name.trim() || "Unnamed program",
      version: program.version.trim() || null,
      level: null,
      typeLabel: null,
      entity: null,
      generation: 0,
      onPath: true,
      current: false,
    });

    for (const fileId of inputs) {
      const file = byId.get(fileId) as ParsedFile;
      addFile(file);
      linked.add(fileId);
      link(fileId, program.id);
    }
    for (const fileId of outputs) {
      const file = byId.get(fileId) as ParsedFile;
      addFile(file);
      linked.add(fileId);
      link(program.id, fileId);
    }

    // Placeholders sit exactly where the real file would, so the shape of the
    // chain does not change when it finally arrives.
    for (const expected of program.expectedInputs) {
      nodes.set(expected.id, {
        id: expected.id,
        kind: "expected",
        name: expected.name,
        // Carries the log it was named by; the placeholder's tooltip says so.
        version: expected.source,
        level: null,
        typeLabel: null,
        entity: null,
        generation: 0,
        onPath: true,
        current: false,
      });
      link(expected.id, program.id);
    }
  }

  // The model always shows, even before anyone attaches it to a run.
  if (modelFile) {
    addFile(modelFile);
  }

  const edgeList = [...edges.values()];
  const generations = computeGenerations([...nodes.keys()], edgeList);
  const laidOut = [...nodes.values()].map((node) => ({
    ...node,
    generation: generations.get(node.id) ?? 0,
  }));
  laidOut.sort(
    (a, b) => a.generation - b.generation || a.name.localeCompare(b.name),
  );

  return {
    lineage: {
      nodes: laidOut,
      edges: edgeList,
      runCount: laidOut.filter((node) => node.kind === "run").length,
    },
    unlinkedFiles: draft.files.filter(
      (file) => !linked.has(file.id) && file.id !== modelFile?.id,
    ),
    unlinkedPrograms,
  };
}
