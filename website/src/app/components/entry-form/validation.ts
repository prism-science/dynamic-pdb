import type { StructureModelType } from "@/lib/api/entries";

import { COMPOUND_LIST_NAMES, compoundIdError } from "./compounds";
import {
  COLLECTION_TEMPERATURE_RANGE,
  GROWTH_TEMPERATURE_RANGE,
  celsiusToKelvin,
  crystalIds,
  crystalIsEmpty,
  formatTemperature,
  inTemperatureRange,
  optionalNumber,
  type TemperatureRange,
} from "./crystals";
import {
  ENTRY_COMPOUND_FIELDS,
  MODEL_COMPOUND_FIELDS,
  type CompoundListFields,
  type FieldId,
} from "./fields";
import { isModelFile, linkedRunId, parseRunParameters } from "./helpers";
import {
  advice,
  invalid,
  required,
  type FieldMessage,
} from "./messages";
import {
  PDB_ID_PATTERN,
  mutationError,
  organismIsEditable,
  unmappedMutationsReason,
} from "./polymerEntities";
import {
  DOI_PATTERN,
  MAX_RELATED_DATASETS,
  MAX_RELATED_DATASET_DETAILS_CHARS,
  MAX_RELATED_DATASET_LABEL_CHARS,
  normalizeDoi,
  relatedDatasetItem,
} from "./relatedDatasets";
import type { ModelEditDraft, ModelLocks } from "./revisionChanges";
import { METRIC_FIELDS, MODEL_PURPOSES, MODEL_TYPES } from "./types";
import type {
  CompoundsDraft,
  CrystalDraft,
  EntryMetadataDraft,
  MetricDraft,
  ModelDraft,
  PolymerEntityDraft,
  RelatedDatasetDraft,
} from "./types";

// The checks a form runs before it lets the depositor submit. They repeat
// the API's own rules (required title and method, resolution above zero,
// mutation format, organism name, allowed purposes and model types, and a
// computed model's programs and score software) so a mistake shows beside
// its field instead of coming back as a 422, and add
// the format checks the API leaves to the client: PDB ID, CCD ID, pH and
// temperatures. Related datasets repeat the API's rules exactly, because the
// API refuses a malformed one in every validation mode. A space group that does not look like Hermann–Mauguin
// notation is only flagged, because the API stores any text; so is a
// temperature outside the wwPDB's usual ranges.

const HERMANN_MAUGUIN_PATTERN =
  /^[PABCFIRH](\s*-?[1-6]{1,2}(\/[mnabcde])?|\s*[mnabcde])+(\s*:\s*[HR12])?$/i;

const TAXONOMY_ID_PATTERN = /^[1-9][0-9]*$/;

const MODEL_FILE_MISSING_LINE = "add exactly one PDB/mmCIF model file";

const DEPOSITED_MODEL_TYPE: StructureModelType = "Deposited";

const PROGRAM_MISSING_LINE = "add the program that made this model";
const PROGRAM_INPUT_MISSING_LINE = "choose at least one input file";
const PROGRAM_OUTPUT_MISSING_LINE = "choose at least one output file of this model";
const METRIC_SOFTWARE_MISSING_LINE =
  "name the software that computed the scores, or choose the program step that did";

export function entryFormMessages(
  title: string,
  metadata: EntryMetadataDraft,
): FieldMessage[] {
  const messages: FieldMessage[] = [];
  if (!title.trim()) {
    messages.push(required("entry.title"));
  }
  const pdb = metadata.pdb.trim();
  if (pdb && !PDB_ID_PATTERN.test(pdb)) {
    messages.push(
      invalid("entry.pdb", "use the four-character ID, a digit first, for example 5GY3"),
    );
  }
  messages.push(...compoundMessages(metadata.compounds, ENTRY_COMPOUND_FIELDS));
  messages.push(...polymerEntityMessages(metadata.polymerEntities));
  if (!metadata.method.trim()) {
    messages.push(required("entry.method"));
  }
  messages.push(
    ...numberMessages("entry.resolution", metadata.resolution, (value) =>
      value > 0 ? null : "must be greater than 0",
    ),
  );
  const spaceGroup = metadata.spaceGroup.trim();
  if (spaceGroup && !HERMANN_MAUGUIN_PATTERN.test(spaceGroup)) {
    messages.push(
      advice(
        "entry.spaceGroup",
        "this does not look like Hermann–Mauguin notation (for example P 21 21 21); keep it only if it is right",
      ),
    );
  }
  messages.push(...crystalMessages(metadata.crystals));
  messages.push(...relatedDatasetMessages(metadata.relatedDatasets));
  return messages;
}

function relatedDatasetMessages(datasets: RelatedDatasetDraft[]): FieldMessage[] {
  const messages: FieldMessage[] = [];
  if (datasets.length > MAX_RELATED_DATASETS) {
    messages.push(
      invalid("entry.relatedDatasets", `list at most ${MAX_RELATED_DATASETS} datasets`),
    );
  }
  // DOIs are case-insensitive, so 10.15785/sbgrid/1303 is the same dataset.
  const seenDois = new Set<string>();
  datasets.forEach((dataset, index) => {
    const item = relatedDatasetItem(index);
    const itemLabel = `dataset ${item}`;
    const doi = normalizeDoi(dataset.doi);
    if (!doi) {
      messages.push(required("relatedDataset.doi", item, itemLabel));
    } else if (!DOI_PATTERN.test(doi)) {
      messages.push(
        invalid(
          "relatedDataset.doi",
          "use a DOI such as 10.15785/SBGRID/1303",
          item,
          itemLabel,
        ),
      );
    } else if (seenDois.has(doi.toLowerCase())) {
      messages.push(
        invalid("relatedDataset.doi", "this dataset is already listed", item, itemLabel),
      );
    } else {
      seenDois.add(doi.toLowerCase());
    }
    if (!dataset.dataType.trim()) {
      messages.push(required("relatedDataset.dataType", item, itemLabel));
    }
    const limits: [FieldId, string, number][] = [
      ["relatedDataset.dataType", dataset.dataType, MAX_RELATED_DATASET_LABEL_CHARS],
      ["relatedDataset.repository", dataset.repository, MAX_RELATED_DATASET_LABEL_CHARS],
      ["relatedDataset.details", dataset.details, MAX_RELATED_DATASET_DETAILS_CHARS],
    ];
    for (const [field, text, limit] of limits) {
      // Counted in characters (code points), as the API counts them.
      if ([...text.trim()].length > limit) {
        messages.push(invalid(field, `use at most ${limit} characters`, item, itemLabel));
      }
    }
  });
  return messages;
}

/**
 * Whether the API's computed-model rules apply to a model of this type: at
 * least one run with software, version, inputs and outputs, and scores that
 * name their software or their run. Only a stated Deposited model is exempt;
 * a blank or unknown type counts as computed, as ModelRevision.IsComputed in
 * backend/internal/validation/provenance.go decides it.
 */
export function isComputedModelType(modelType: string): boolean {
  return modelType.trim() !== DEPOSITED_MODEL_TYPE;
}

/**
 * The add-model form's checks. Depositors cannot choose Deposited, so every
 * model added here is computed and needs its programs and score software.
 *
 * @param baselineFileIds ids of the entry's own files, which a program may
 *   read as inputs (but never write as outputs).
 */
export function modelFormMessages(
  draft: ModelDraft,
  baselineFileIds: ReadonlySet<string> = new Set(),
): FieldMessage[] {
  return [
    ...modelOverviewMessages(draft, { purpose: false, modelType: false, compounds: false }),
    ...modelFileMessages(draft),
    ...metricMessages(draft.metrics),
    ...metricSoftwareMessages(draft.metrics, newModelSoftwareRule(draft)),
    ...programMessages(draft, baselineFileIds),
  ];
}

/**
 * The model edit form's checks. Files and runs are read-only there, and a
 * read-only value is not checked: the depositor could not fix it.
 */
export function modelEditMessages(
  draft: ModelEditDraft,
  locks: ModelLocks,
  metricsSent: MetricsSent,
): FieldMessage[] {
  return [
    ...modelOverviewMessages(draft, {
      purpose: locks.purpose,
      modelType: locks.modelType,
      compounds: locks.fields.ligands !== undefined,
    }),
    ...metricMessages(draft.metrics),
    ...metricSoftwareMessages(draft.metrics, editedModelSoftwareRule(draft, metricsSent)),
  ];
}

/** Says whether a score set must name its software. */
export type MetricSoftwareRule = (metric: MetricDraft) => boolean;

/**
 * The add-model form's rule: a computed model's scores name their software
 * unless they are linked to one of the draft's programs.
 */
export function newModelSoftwareRule(draft: ModelDraft): MetricSoftwareRule {
  const programIds = new Set(draft.programs.map((program) => program.id));
  return (metric) =>
    isComputedModelType(draft.modelType) && metricNeedsSoftware(metric, programIds);
}

/** What the edit form sends of the metrics. */
export type MetricsSent = {
  /** Metrics are sent only when one changed, and checked only then. */
  changed: boolean;
  /** Ids of the model's stored runs, which the new revision inherits. */
  storedRunIds: ReadonlySet<string>;
};

/**
 * The edit form's rule: when the change sends the metrics of a computed
 * model, each score set names its software unless it is stored with one of
 * the model's runs.
 */
export function editedModelSoftwareRule(
  draft: ModelEditDraft,
  { changed, storedRunIds }: MetricsSent,
): MetricSoftwareRule {
  return (metric) =>
    changed &&
    isComputedModelType(draft.modelType) &&
    metricNeedsSoftware(metric, storedRunIds);
}

/** A score set holding a score and linked to none of runIds. */
function metricNeedsSoftware(metric: MetricDraft, runIds: ReadonlySet<string>): boolean {
  const hasScore = METRIC_FIELDS.some(
    (field) => (metric.values[field.key] ?? "").trim() !== "",
  );
  return hasScore && linkedRunId(metric, runIds) === null;
}

function metricSoftwareMessages(
  metrics: MetricDraft[],
  softwareRequired: MetricSoftwareRule,
): FieldMessage[] {
  return metrics.flatMap((metric) =>
    softwareRequired(metric) && !metric.softwareName?.trim()
      ? [{ ...required("model.metrics.software", metric.id), message: METRIC_SOFTWARE_MISSING_LINE }]
      : [],
  );
}

function modelOverviewMessages(
  values: Pick<ModelDraft, "title" | "purpose" | "modelType" | "compounds">,
  readOnly: { purpose: boolean; modelType: boolean; compounds: boolean },
): FieldMessage[] {
  const messages: FieldMessage[] = [];
  if (!values.title.trim()) {
    messages.push(required("model.title"));
  }
  if (
    !readOnly.purpose &&
    values.purpose &&
    !(MODEL_PURPOSES as string[]).includes(values.purpose)
  ) {
    messages.push(invalid("model.purpose", "choose one of the listed purposes"));
  }
  if (
    !readOnly.modelType &&
    values.modelType &&
    !(MODEL_TYPES as string[]).includes(values.modelType)
  ) {
    messages.push(invalid("model.type", "choose one of the listed model types"));
  }
  if (!readOnly.compounds) {
    messages.push(...compoundMessages(values.compounds, MODEL_COMPOUND_FIELDS));
  }
  return messages;
}

function numberMessages(
  field: FieldId,
  text: string,
  check: (value: number) => string | null,
  item: string | null = null,
  itemLabel?: string,
): FieldMessage[] {
  const trimmed = text.trim();
  if (!trimmed) {
    return [];
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value)) {
    return [invalid(field, "enter a number", item, itemLabel)];
  }
  const problem = check(value);
  return problem ? [invalid(field, problem, item, itemLabel)] : [];
}

function compoundMessages(
  compounds: CompoundsDraft,
  fields: CompoundListFields,
): FieldMessage[] {
  return COMPOUND_LIST_NAMES.flatMap((list) => {
    const field = fields[list];
    return compounds[list].items.flatMap((item) => {
      const error = compoundIdError(item.id);
      return error ? [invalid(field, error)] : [];
    });
  });
}

function polymerEntityMessages(entities: PolymerEntityDraft[]): FieldMessage[] {
  return entities.flatMap((entity) => {
    const item = entity.labelEntityId;
    const itemLabel = `entity ${entity.labelEntityId}`;
    const messages = entity.mutations.flatMap((mutation) => {
      const error = mutationError(mutation);
      return error ? [invalid("entity.mutations", error, item, itemLabel)] : [];
    });
    const review = unmappedMutationsReason(entity);
    if (review) {
      messages.push(advice("entity.mutations", review, item));
    }
    if (organismIsEditable(entity)) {
      const name = entity.organismName.trim();
      const taxonomyId = entity.organismTaxonomyId.trim();
      if (taxonomyId && !TAXONOMY_ID_PATTERN.test(taxonomyId)) {
        messages.push(
          invalid(
            "entity.organism",
            "the NCBI taxonomy ID is a positive whole number",
            item,
            itemLabel,
          ),
        );
      }
      if (taxonomyId && !name) {
        messages.push(
          invalid(
            "entity.organism",
            "enter the scientific name that goes with the taxonomy ID",
            item,
            itemLabel,
          ),
        );
      }
    }
    return messages;
  });
}

function crystalMessages(crystals: CrystalDraft[]): FieldMessage[] {
  const ids = crystalIds(crystals);
  return crystals.flatMap((crystal, index) => {
    const { crystalId, diffractionIds } = ids[index];
    const itemLabel = `crystal ${crystalId}`;
    const messages: FieldMessage[] = [];
    if (crystalIsEmpty(crystal)) {
      messages.push(
        invalid("crystal.block", "fill in a value or remove this crystal", crystalId, itemLabel),
      );
    }
    messages.push(
      ...numberMessages(
        "crystal.ph",
        crystal.ph,
        (value) => (value >= 0 && value <= 14 ? null : "the pH scale runs from 0 to 14"),
        crystalId,
        itemLabel,
      ),
      ...numberMessages(
        "crystal.growthTemperature",
        crystal.growthTemperature,
        (value) => (value > 0 ? null : "must be above 0 K"),
        crystalId,
        itemLabel,
      ),
      ...growthTemperatureAdvice(crystal.growthTemperature, crystalId),
    );
    crystal.diffractions.forEach((diffraction, position) => {
      const item = `${crystalId}/${diffractionIds[position]}`;
      const datasetLabel = `${itemLabel}, dataset ${diffractionIds[position]}`;
      if (!diffraction.temperature.trim()) {
        messages.push(
          invalid(
            "diffraction.temperature",
            "enter the temperature or remove this dataset",
            item,
            datasetLabel,
          ),
        );
        return;
      }
      messages.push(
        ...numberMessages(
          "diffraction.temperature",
          diffraction.temperature,
          (value) => (value > 0 ? null : "must be above 0 K"),
          item,
          datasetLabel,
        ),
        ...collectionTemperatureAdvice(diffraction.temperature, item),
      );
    });
    return messages;
  });
}

/**
 * A growth temperature outside the usual range gets a warning. One that
 * would be in range read as °C, such as 20, was almost certainly typed in °C,
 * so it gets a hint with the kelvin value instead.
 */
function growthTemperatureAdvice(text: string, item: string): FieldMessage[] {
  const kelvin = usableKelvin(text);
  if (kelvin === null || inTemperatureRange(kelvin, GROWTH_TEMPERATURE_RANGE)) {
    return [];
  }
  const kelvinIfCelsius = celsiusToKelvin(kelvin);
  if (inTemperatureRange(kelvinIfCelsius, GROWTH_TEMPERATURE_RANGE)) {
    return [
      advice(
        "crystal.growthTemperature",
        `this looks like °C; ${formatTemperature(kelvin)} °C is ${formatTemperature(kelvinIfCelsius)} K`,
        item,
      ),
    ];
  }
  return [
    advice(
      "crystal.growthTemperature",
      outsideRangeLine(GROWTH_TEMPERATURE_RANGE, "crystal growth"),
      item,
    ),
  ];
}

function collectionTemperatureAdvice(text: string, item: string): FieldMessage[] {
  const kelvin = usableKelvin(text);
  if (kelvin === null || inTemperatureRange(kelvin, COLLECTION_TEMPERATURE_RANGE)) {
    return [];
  }
  return [
    advice(
      "diffraction.temperature",
      outsideRangeLine(COLLECTION_TEMPERATURE_RANGE, "data collection"),
      item,
    ),
  ];
}

/** The temperature to range-check, or null when the box is empty or already
 *  has a blocking message (not a number, or not above 0 K). */
function usableKelvin(text: string): number | null {
  const kelvin = optionalNumber(text);
  return kelvin !== undefined && kelvin > 0 ? kelvin : null;
}

function outsideRangeLine(range: TemperatureRange, stage: string): string {
  return `outside the usual ${range.min}–${range.max} K for ${stage}; keep it only if it is right`;
}

function modelFileMessages(draft: ModelDraft): FieldMessage[] {
  const modelFileCount = draft.files.filter(isModelFile).length;
  if (modelFileCount === 0) {
    return [{ ...required("model.files"), message: MODEL_FILE_MISSING_LINE }];
  }
  const messages: FieldMessage[] = [];
  if (modelFileCount > 1) {
    messages.push(invalid("model.files", "keep only one PDB/mmCIF model file"));
  }
  if (draft.files.filter((file) => file.artifactType === "structure_factors").length > 1) {
    messages.push(invalid("model.files", "keep only one structure factors file"));
  }
  return messages;
}

function metricMessages(metrics: MetricDraft[]): FieldMessage[] {
  return metrics.flatMap((metric) =>
    METRIC_FIELDS.flatMap((field) =>
      numberMessages(
        "model.metrics",
        metric.values[field.key] ?? "",
        () => null,
        field.key,
        field.label,
      ),
    ),
  );
}

/**
 * The programs (runs) of a model. A computed model needs at least one, and
 * each needs a version, an input and an output: the API's ModelRuns rule.
 * Only links to files the request carries count. An input may be one of this
 * model's files or one of the entry's; an output only one of this model's.
 */
function programMessages(
  draft: ModelDraft,
  baselineFileIds: ReadonlySet<string>,
): FieldMessage[] {
  const computed = isComputedModelType(draft.modelType);
  if (computed && draft.programs.length === 0) {
    return [{ ...required("model.runs"), message: PROGRAM_MISSING_LINE }];
  }
  const modelFileIds = new Set(draft.files.map((file) => file.id));
  return draft.programs.flatMap((program, index) => {
    const itemLabel = `step ${index + 1}`;
    const messages: FieldMessage[] = [];
    if (!program.name.trim()) {
      messages.push(
        invalid("run.name", "name the program or remove this step", program.id, itemLabel),
      );
    }
    if (computed) {
      if (!program.version.trim()) {
        messages.push(required("run.version", program.id, itemLabel));
      }
      const hasInput = program.inputFileIds.some(
        (id) => modelFileIds.has(id) || baselineFileIds.has(id),
      );
      if (!hasInput) {
        messages.push({
          ...required("run.files", program.id, itemLabel),
          message: PROGRAM_INPUT_MISSING_LINE,
        });
      }
      if (!program.outputFileIds.some((id) => modelFileIds.has(id))) {
        messages.push({
          ...required("run.files", program.id, itemLabel),
          message: PROGRAM_OUTPUT_MISSING_LINE,
        });
      }
    }
    if (parseRunParameters(program.parameters) === null) {
      messages.push(
        invalid(
          "run.parameters",
          "enter one JSON object, for example {\"cycles\": 5}",
          program.id,
          itemLabel,
        ),
      );
    }
    return messages;
  });
}
