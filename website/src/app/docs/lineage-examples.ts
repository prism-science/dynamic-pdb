import type { EntityLevel } from "@/lib/api/entries";
import {
  computeGenerations,
  type Lineage,
  type LineageEdge,
  type LineageNode,
  type LineageRegion,
} from "@/lib/lineage";

export type LineageExample = {
  id: string;
  label: string;
  caption: string;
  lineage: Lineage;
};

type NodeSpec = { id: string; column?: number } & (
  | { run: string; version?: string }
  | { artifact: string; level: EntityLevel; type: "data" | "model" | "metrics" }
);

type Boundary = { label: string; external?: { label: string; nodeIds: string[] } };

function lineageOf(
  specs: NodeSpec[],
  links: [string, string][],
  boundary: Boundary = { label: "One entry" },
): Lineage {
  const edges: LineageEdge[] = links.map(([source, target]) => ({
    id: `${source}->${target}`,
    source,
    target,
    onPath: true,
  }));
  const generations = computeGenerations(
    specs.map((spec) => spec.id),
    edges,
  );
  const nodes: LineageNode[] = specs.map((spec) => ({
    id: spec.id,
    kind: "run" in spec ? "run" : "artifact",
    name: "run" in spec ? spec.run : spec.artifact,
    version: "run" in spec ? (spec.version ?? null) : null,
    level: "run" in spec ? null : spec.level,
    typeLabel: "run" in spec ? null : spec.type,
    entity: null,
    generation: generations.get(spec.id) ?? 0,
    onPath: true,
    current: false,
    column: spec.column,
  }));
  const regions: LineageRegion[] = [
    {
      id: "entry",
      label: boundary.label,
      nodeIds: specs.map((spec) => spec.id),
      tone: "registry",
    },
  ];
  if (boundary.external) {
    regions.push({ id: "external", tone: "external", ...boundary.external });
  }
  return {
    nodes,
    edges,
    runCount: specs.filter((spec) => "run" in spec).length,
    regions,
  };
}

export const LINEAGE_EXAMPLES: LineageExample[] = [
  {
    id: "shared-input",
    label: "Shared input",
    caption:
      "Alternative models can share the same experimental input while differing in processing methods and reported metrics. Check evaluation methods before comparing scores.",
    lineage: lineageOf(
      [
        { id: "data", artifact: "Processed experimental data", level: "L1", type: "data" },
        { id: "run-a", run: "Run A", version: "software, version" },
        { id: "run-b", run: "Run B", version: "software, version" },
        { id: "model-a", artifact: "Model A", level: "L2", type: "model" },
        { id: "model-b", artifact: "Model B", level: "L2", type: "model" },
        { id: "scores-a", artifact: "Reported metrics for Model A", level: "L3", type: "metrics" },
        { id: "scores-b", artifact: "Reported metrics for Model B", level: "L3", type: "metrics" },
      ],
      [
        ["data", "run-a"],
        ["data", "run-b"],
        ["run-a", "model-a"],
        ["run-b", "model-b"],
        ["model-a", "scores-a"],
        ["model-b", "scores-b"],
      ],
    ),
  },
  {
    id: "processing-chain",
    label: "Processing chain",
    caption:
      "A processing run turns raw L0 data into L1 processed data, which several modeling runs can then share.",
    lineage: lineageOf(
      [
        { id: "raw", artifact: "Raw dataset", level: "L0", type: "data" },
        { id: "process", run: "Processing run" },
        { id: "data", artifact: "Processed data", level: "L1", type: "data" },
        { id: "run-a", run: "Modeling run A" },
        { id: "run-b", run: "Modeling run B" },
        { id: "model-a", artifact: "Model A", level: "L2", type: "model" },
        { id: "model-b", artifact: "Model B", level: "L2", type: "model" },
      ],
      [
        ["raw", "process"],
        ["process", "data"],
        ["data", "run-a"],
        ["data", "run-b"],
        ["run-a", "model-a"],
        ["run-b", "model-b"],
      ],
    ),
  },
  {
    id: "merged-datasets",
    label: "Merged datasets",
    caption:
      "Two raw datasets contribute to one processed dataset. For example, diffraction images from two collections could be processed and merged before modeling.",
    lineage: lineageOf(
      [
        { id: "raw-a", artifact: "Raw dataset A", level: "L0", type: "data" },
        { id: "raw-b", artifact: "Raw dataset B", level: "L0", type: "data" },
        { id: "process", run: "Joint processing and merging" },
        { id: "data", artifact: "Combined processed data", level: "L1", type: "data" },
        { id: "run-a", run: "Modeling run A" },
        { id: "run-b", run: "Modeling run B" },
        { id: "model-a", artifact: "Model A", level: "L2", type: "model" },
        { id: "model-b", artifact: "Model B", level: "L2", type: "model" },
      ],
      [
        ["raw-a", "process"],
        ["raw-b", "process"],
        ["process", "data"],
        ["data", "run-a"],
        ["data", "run-b"],
        ["run-a", "model-a"],
        ["run-b", "model-b"],
      ],
    ),
  },
  {
    id: "building-on-the-pdb",
    label: "Building on the PDB",
    caption:
      "The dashed box holds what the PDB already provides: structure factors, the deposited model, and its validation report. The Dynamic PDB entry around it adds the raw diffraction images, the processing history, and alternative models built on the same data.",
    lineage: lineageOf(
      [
        { id: "raw", artifact: "Raw diffraction images", level: "L0", type: "data", column: 0 },
        { id: "process", run: "Processing run", column: 0 },
        { id: "data", artifact: "Structure factors", level: "L1", type: "data", column: 0 },
        { id: "run-a", run: "Original modeling run", column: 0 },
        { id: "run-b", run: "Alternative modeling run", column: 1 },
        { id: "model-a", artifact: "PDB-deposited model", level: "L2", type: "model", column: 0 },
        { id: "model-b", artifact: "Alternative model", level: "L2", type: "model", column: 1 },
        { id: "report-a", artifact: "wwPDB validation report", level: "L3", type: "metrics", column: 0 },
        { id: "scores-b", artifact: "Reported evaluation results", level: "L3", type: "metrics", column: 1 },
      ],
      [
        ["raw", "process"],
        ["process", "data"],
        ["data", "run-a"],
        ["data", "run-b"],
        ["run-a", "model-a"],
        ["run-b", "model-b"],
        ["model-a", "report-a"],
        ["model-b", "scores-b"],
      ],
      {
        label: "Dynamic PDB entry",
        external: { label: "PDB", nodeIds: ["data", "run-a", "model-a", "report-a"] },
      },
    ),
  },
];
