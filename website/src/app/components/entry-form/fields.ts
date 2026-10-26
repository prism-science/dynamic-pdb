import type { components } from "@/lib/api/schema.gen";

import type { CompoundListName } from "./types";

type Schemas = components["schemas"];

/**
 * The sections the forms are grouped into. They copy the names the entry and
 * model pages already use, so a depositor finds a value under the heading the
 * page will show it under: Overview, Macromolecules and Data on the Overview
 * tab, Experiment as its own tab, and Evaluations on a model's Overview tab.
 */
export type SectionId =
  | "overview"
  | "macromolecules"
  | "experiment"
  | "data"
  | "evaluations";

export const SECTIONS: Record<SectionId, { title: string; hint: string }> = {
  overview: {
    title: "Overview",
    hint: "What this is, and what the sample contained.",
  },
  macromolecules: {
    title: "Macromolecules",
    hint: "The polymer chains in the sample, one block per distinct sequence.",
  },
  experiment: {
    title: "Experiment",
    hint: "How the data were measured.",
  },
  data: {
    title: "Data",
    hint: "The files. Each one gets a level: L0 raw measurements, L1 processed data, L2 models, L3 evaluations.",
  },
  evaluations: {
    title: "Evaluations",
    hint: "Validation scores. Leave a score empty when you do not have it: an empty score is never read as 0.",
  },
};

/** Where a value comes from when Dynamic PDB fills it in itself. Such values
 *  are shown read-only, with their source. */
export type AutofillSource = "archive" | "file";

export type FieldSpec = {
  section: SectionId;
  label: string;
  /** One or two sentences for a depositor who is not a structural biologist. */
  help: string;
  required: boolean;
  /** The API tells "not known" (the field is left out) apart from "none" (an
   *  empty list), so the form offers both choices. */
  unknownAllowed: boolean;
  autofill: AutofillSource | null;
  /** JSON pointers of the value in the create request, with `*` for an array
   *  index. API errors are matched on them. Empty for a value the request
   *  does not carry. */
  pointers: readonly string[];
};

// Each pointer is built from a key of the generated request type, so renaming
// a field in backend/api/openapi.yaml fails typecheck here instead of leaving
// an error the form cannot place.
type Key<T> = Extract<keyof T, string>;

const entryPointer = (key: Key<Schemas["CreateEntryData"]>) => `/entry/${key}`;
const polymerEntityPointer = (key: Key<Schemas["DepositorPolymerEntity"]>) =>
  `/entry/polymer_entities/*/${key}`;
const crystalGrowthPointer = (key: Key<Schemas["EntryCrystalGrowth"]>) =>
  `/entry/crystallography/crystals/*/growth/${key}`;
const diffractionPointer = (key: Key<Schemas["EntryDiffraction"]>) =>
  `/entry/crystallography/crystals/*/diffractions/*/${key}`;
const modelPointer = (key: Key<Schemas["CreateModelData"]>) => `/model/${key}`;
const modelMetadataPointer = (key: Key<Schemas["ModelMetadata"]>) =>
  `/model/metadata/${key}`;
const runPointer = (key: Key<Schemas["CreateRunRequest"]>) =>
  `/model/runs/*/${key}`;

const COMPOUND_HELP =
  "CCD IDs, for example ATP or HEM. A typed ID is sorted into ligands or cofactors by the reference list; move it if the list is wrong.";

export const FIELDS = {
  // ---------------------------------------------------------- entry form --
  "entry.title": {
    section: "overview",
    label: "Title",
    help: "A name a colleague would recognise, such as the protein and the condition.",
    required: true,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("title")],
  },
  "entry.thumbnail": {
    section: "overview",
    label: "Preview image",
    help: "Shown on the entry's card.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("thumbnail_image_url")],
  },
  "entry.details": {
    section: "overview",
    label: "Description",
    help: "What was studied and why, and anything about the sample the other fields do not say.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("details")],
  },
  "entry.pdb": {
    section: "overview",
    label: "PDB ID",
    help: "The four-character archive ID, if the structure is in the PDB. The macromolecules are read from it.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [`${entryPointer("external_refs")}/pdb`],
  },
  "entry.ligands": {
    section: "overview",
    label: "Ligands",
    help: `Small molecules in the sample. Leave out water, buffer and cryoprotectant. ${COMPOUND_HELP}`,
    required: false,
    unknownAllowed: true,
    autofill: null,
    pointers: [entryPointer("ligands")],
  },
  "entry.cofactors": {
    section: "overview",
    label: "Cofactors",
    help: "Cofactors, and metal ions that are part of the protein (for example ZN).",
    required: false,
    unknownAllowed: true,
    autofill: null,
    pointers: [entryPointer("cofactors")],
  },
  "entity.description": {
    section: "macromolecules",
    label: "Molecule",
    help: "The molecule's name in the PDB.",
    required: false,
    unknownAllowed: false,
    autofill: "archive",
    pointers: [],
  },
  "entity.uniprot": {
    section: "macromolecules",
    label: "UniProt",
    help: "The UniProt accession the PDB maps this chain to. It cannot be typed.",
    required: false,
    unknownAllowed: false,
    autofill: "archive",
    pointers: [],
  },
  "entity.organism": {
    section: "macromolecules",
    label: "Source organism",
    help: "The organism the sequence comes from. Typed only when the PDB names none.",
    required: false,
    unknownAllowed: false,
    autofill: "archive",
    pointers: [polymerEntityPointer("source_organisms")],
  },
  "entity.construct": {
    section: "macromolecules",
    label: "Construct",
    help: "What was expressed, for example residues 1–164 with an N-terminal His tag. Leave empty to keep the PDB's text.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [polymerEntityPointer("construct")],
  },
  "entity.mutations": {
    section: "macromolecules",
    label: "Mutations",
    help: "One substitution each, in UniProt numbering: original residue, position, new residue, for example C54T. Typed mutations replace the PDB's.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [polymerEntityPointer("mutations")],
  },
  "entity.block": {
    section: "macromolecules",
    label: "Macromolecule",
    help: "One distinct sequence of the sample.",
    required: false,
    unknownAllowed: false,
    autofill: "archive",
    pointers: [polymerEntityPointer("label_entity_id")],
  },
  "entry.method": {
    section: "experiment",
    label: "Method",
    help: "The experimental method, for example X-ray crystallography or cryo-EM. Any text is accepted.",
    required: true,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("method")],
  },
  "entry.resolution": {
    section: "experiment",
    label: "Resolution (Å)",
    help: "The highest resolution measured, in ångströms. Leave empty when the method has none.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("resolution")],
  },
  "entry.spaceGroup": {
    section: "experiment",
    label: "Space group",
    help: "The crystal's symmetry in Hermann–Mauguin notation, for example P 21 21 21.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("space_group")],
  },
  "crystal.block": {
    section: "experiment",
    label: "Crystal",
    help: "Each crystal's growth conditions, and the temperature of each dataset collected from it. Crystals stay separate: their conditions are never merged into one value.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: ["/entry/crystallography/crystals/*/id"],
  },
  "crystal.ph": {
    section: "experiment",
    label: "Growth pH",
    help: "pH of the crystallisation drop.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [crystalGrowthPointer("ph")],
  },
  "crystal.growthTemperature": {
    section: "experiment",
    label: "Growth temperature (K)",
    help: "In kelvin. Room temperature is about 293 K.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [crystalGrowthPointer("temperature_kelvin")],
  },
  "diffraction.temperature": {
    section: "experiment",
    label: "Data-collection temperature (K)",
    help: "In kelvin, one per dataset. 100 K is a typical cryo temperature.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [diffractionPointer("temperature_kelvin"), diffractionPointer("id")],
  },
  "entry.files": {
    section: "data",
    label: "Baseline files",
    help: "Data every model of this entry shares, such as the sequence and raw measurements.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [entryPointer("artifacts")],
  },

  // ---------------------------------------------------------- model form --
  "model.title": {
    section: "overview",
    label: "Title",
    help: "A name that says how this model differs from the others, for example Refined with qFit.",
    required: true,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelPointer("title")],
  },
  "model.thumbnail": {
    section: "overview",
    label: "Preview image",
    help: "Shown on the model's card.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelPointer("thumbnail_image_url")],
  },
  "model.purpose": {
    section: "overview",
    label: "Purpose",
    help: "Model building: a model built from the data. Refinement: an existing model improved against the data.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelMetadataPointer("purpose")],
  },
  "model.type": {
    section: "overview",
    label: "Model type",
    help: "Single conformer: one position per atom. Multiconformer: alternate positions where the data support them. Ensemble: several whole models.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelMetadataPointer("model_type")],
  },
  "model.details": {
    section: "overview",
    label: "Description",
    help: "How this model was made and what it shows that the others do not.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelMetadataPointer("details")],
  },
  "model.authors": {
    section: "overview",
    label: "Authors",
    help: "Who made this model, as they should be credited. Press Enter after each name.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelMetadataPointer("authors")],
  },
  "model.affiliation": {
    section: "overview",
    label: "Affiliation",
    help: "The authors' institution.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelMetadataPointer("affiliation")],
  },
  "model.ligands": {
    section: "overview",
    label: "Ligands",
    help: `Small molecules this model contains, read from the coordinate file. ${COMPOUND_HELP}`,
    required: false,
    unknownAllowed: true,
    autofill: "file",
    pointers: [modelMetadataPointer("ligands")],
  },
  "model.cofactors": {
    section: "overview",
    label: "Cofactors",
    help: "Cofactors and bound metal ions this model contains, read from the coordinate file.",
    required: false,
    unknownAllowed: true,
    autofill: "file",
    pointers: [modelMetadataPointer("cofactors")],
  },
  "model.files": {
    section: "data",
    label: "Files",
    help: "Exactly one coordinate file (.pdb or .cif), plus any reflections, maps and run logs.",
    required: true,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelPointer("artifacts"), modelPointer("primary_artifact_id")],
  },
  "model.metrics": {
    section: "evaluations",
    label: "Metrics",
    help: "One set of scores for this model.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [modelPointer("metrics")],
  },
  "run.name": {
    section: "experiment",
    label: "Name",
    help: "The program that ran, for example phenix.refine.",
    required: true,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("name"), runPointer("software_name"), runPointer("id")],
  },
  "run.version": {
    section: "experiment",
    label: "Version",
    help: "The program's version.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("software_version")],
  },
  "run.description": {
    section: "experiment",
    label: "Description",
    help: "What this step did.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("metadata")],
  },
  "run.command": {
    section: "experiment",
    label: "Command",
    help: "The command line exactly as it was run.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("command")],
  },
  "run.parameters": {
    section: "experiment",
    label: "Parameters (JSON)",
    help: "The settings as one JSON object, for example {\"cycles\": 5}.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("parameters")],
  },
  "run.files": {
    section: "experiment",
    label: "Inputs and outputs",
    help: "Which files this step read and which it wrote.",
    required: false,
    unknownAllowed: false,
    autofill: null,
    pointers: [runPointer("artifacts")],
  },
} satisfies Record<string, FieldSpec>;

export type FieldId = keyof typeof FIELDS;

/** The field of each compound list, on the entry form and on the model form. */
export type CompoundListFields = Record<CompoundListName, FieldId>;

export const ENTRY_COMPOUND_FIELDS: CompoundListFields = {
  ligands: "entry.ligands",
  cofactors: "entry.cofactors",
};

export const MODEL_COMPOUND_FIELDS: CompoundListFields = {
  ligands: "model.ligands",
  cofactors: "model.cofactors",
};

export function fieldSpec(id: FieldId): FieldSpec {
  return FIELDS[id];
}
