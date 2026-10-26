import type {
  ArtifactType,
  EntityLevel,
  MetricKey,
  ModelPurpose,
  StructureModelType,
} from "@/lib/api/entries";
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
  artifactType: ArtifactType;
  level: EntityLevel;
  authors: string;
  metadata?: Record<string, unknown>;
  sha256?: string;
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

/**
 * A file a run declared but that nobody has uploaded yet. Kept on the program
 * so the graph can show it as a placeholder and the form can nag about it at
 * save time — a refinement whose free-R set is missing is a common and quiet
 * kind of incomplete deposit.
 */
export type ExpectedInput = {
  id: string;
  /** Basename exactly as the log wrote it. */
  name: string;
  /** Which log it came from, so the hint can say where we read it. */
  source: string;
};

export type ProgramDraft = {
  id: string;
  name: string;
  version: string;
  description: string;
  /** The command line as it was run. Optional. */
  command: string;
  /** Settings as JSON object text, parsed on submit. Optional. */
  parameters: string;
  /** Ids of files fed to this run. */
  inputFileIds: string[];
  /** Ids of files it produced. */
  outputFileIds: string[];
  expectedInputs: ExpectedInput[];
  /** Whether a human typed this or a header/log did. */
  origin: "manual" | "parsed";
  /** Filename the facts were read from, for the "from refine_001.pdb" hint. */
  originFile?: string;
};

/**
 * One compound of a ligand or cofactor list. `review` holds the reason a
 * parser kept it but is unsure about it, until the depositor decides.
 */
export type CompoundEntry = { id: string; review: string | null };

/**
 * The API reads an omitted list as "not known" and an empty one as "none",
 * so the form keeps the two apart instead of letting a blank mean either.
 */
export type CompoundListMode = "unknown" | "none" | "list";

export type CompoundList = { mode: CompoundListMode; items: CompoundEntry[] };

export type CompoundsDraft = { ligands: CompoundList; cofactors: CompoundList };

export type CompoundListName = keyof CompoundsDraft;

export type DiffractionDraft = { key: string; temperature: string };

/** One crystal's conditions. Crystals are never merged into one value. */
export type CrystalDraft = {
  key: string;
  ph: string;
  growthTemperature: string;
  diffractions: DiffractionDraft[];
};

/**
 * One polymer entity of the PDB entry. The archive* values come from the PDB
 * lookup and are shown read-only; the rest is what the depositor typed, and
 * only that is sent, so a later archive update can still fill what they left
 * empty.
 */
export type PolymerEntityDraft = {
  labelEntityId: string;
  description: string;
  uniprotAccessions: string[];
  archiveOrganisms: string[];
  archiveConstruct: string;
  archiveMutations: string[];
  construct: string;
  mutations: string[];
  /** Used only when the PDB names no organism. */
  organismName: string;
  organismTaxonomyId: string;
};

/**
 * Properties of the structure, not of any one model. Kept on the entry
 * because two models of the same crystal share them.
 *
 * Method and space group are free text.
 */
export type EntryMetadataDraft = {
  details: string;
  pdb: string;
  resolution: string;
  method: string;
  spaceGroup: string;
  compounds: CompoundsDraft;
  crystals: CrystalDraft[];
  polymerEntities: PolymerEntityDraft[];
};

export const METHODS = ["X-ray crystallography", "CryoEM"] as const;

/**
 * Keyed by the backend enum so a value added to openapi.yaml fails to compile
 * here until someone decides whether depositors may pick it. Reference and
 * Deposited are set by the system for unchanged archive imports.
 */
const MODEL_PURPOSE_DEPOSITOR_CHOICE: Record<ModelPurpose, boolean> = {
  "Model Building": true,
  Refinement: true,
  Reference: false,
};

const MODEL_TYPE_DEPOSITOR_CHOICE: Record<StructureModelType, boolean> = {
  "Single Conformer": true,
  Multiconformer: true,
  Ensemble: true,
  Deposited: false,
};

function depositorChoices<Value extends string>(
  choices: Record<Value, boolean>,
): Value[] {
  return (Object.keys(choices) as Value[]).filter((value) => choices[value]);
}

export const MODEL_PURPOSES = depositorChoices(MODEL_PURPOSE_DEPOSITOR_CHOICE);

export const MODEL_TYPES = depositorChoices(MODEL_TYPE_DEPOSITOR_CHOICE);

export function emptyCompounds(): CompoundsDraft {
  return {
    ligands: { mode: "unknown", items: [] },
    cofactors: { mode: "unknown", items: [] },
  };
}

export function emptyEntryMetadataDraft(): EntryMetadataDraft {
  return {
    details: "",
    pdb: "",
    resolution: "",
    method: "",
    spaceGroup: "",
    compounds: emptyCompounds(),
    crystals: [],
    polymerEntities: [],
  };
}

export type ModelDraft = {
  id: string;
  title: string;
  thumbFileId: string;
  thumbFile: File | null;
  thumbPreview: string | null;
  thumbUrl: string | null;
  thumbProgress: number;
  thumbUploadStatus: UploadStatus;
  thumbUploadError: string | null;
  files: ParsedFile[];
  metrics: MetricDraft[];
  /** Judgements no header states: what the run was for, what came out. */
  purpose: string;
  modelType: string;
  details: string;
  authors: string[];
  affiliation: string;
  /** Composition of this model's coordinates, prefilled from the file. */
  compounds: CompoundsDraft;
  /** Name of the file the composition was read from, for its source badge. */
  compoundsSource: string | null;
  /**
   * One entry per run. A deposit that went model building -> refinement ->
   * multiconformer build is three, and each states its own inputs, so the
   * chain no longer has to be inferred from file extensions.
   */
  programs: ProgramDraft[];
};

export const LEVELS: EntityLevel[] = ["L0", "L1", "L2", "L3"];

export type MetricField = { key: MetricKey; label: string; example: string };

// The first five keep the order the form has always shown; the rest follow.
const METRIC_FIELD_BY_KEY: Record<MetricKey, Omit<MetricField, "key">> = {
  r_work: { label: "R-work", example: "e.g. 0.196" },
  r_free: { label: "R-free", example: "e.g. 0.231" },
  clashscore: { label: "Clashscore", example: "e.g. 4.8" },
  molprobity_score: { label: "MolProbity score", example: "e.g. 1.42" },
  rscc: { label: "RSCC", example: "e.g. 0.96" },
  cc: { label: "Correlation coefficient (CC)", example: "e.g. 0.91" },
  ramachandran_outliers: {
    label: "Ramachandran outliers (%)",
    example: "e.g. 0.2",
  },
  side_chain_outliers: {
    label: "Side-chain outliers (%)",
    example: "e.g. 1.5",
  },
  wilson_b: { label: "Wilson B (Å²)", example: "e.g. 21.3" },
  rmsf: { label: "RMSF (Å)", example: "e.g. 0.65" },
};

export const METRIC_FIELDS: MetricField[] = (
  Object.keys(METRIC_FIELD_BY_KEY) as MetricKey[]
).map((key) => ({ key, ...METRIC_FIELD_BY_KEY[key] }));

export const DRAFT_STORAGE_KEY = "dpdb:new-entry-draft";
// 3: entry and model identity changed from name/description to title.
// 4: the metadata block (description, experiment, composition, crystals,
//    macromolecules) is saved too. A version 3 draft is read with an empty one.
export const DRAFT_VERSION = 4;
export const PREVIOUS_DRAFT_VERSION = 3;

export const MODEL_DRAFT_STORAGE_PREFIX = "dpdb:new-model-draft:";
export const MODEL_DRAFT_VERSION = 1;
