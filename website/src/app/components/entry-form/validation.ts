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
import { isModelFile, parseRunParameters } from "./helpers";
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
} from "./polymerEntities";
import type { ModelEditDraft, ModelLocks } from "./revisionChanges";
import { METRIC_FIELDS, MODEL_PURPOSES, MODEL_TYPES } from "./types";
import type {
  CompoundsDraft,
  CrystalDraft,
  EntryMetadataDraft,
  MetricDraft,
  ModelDraft,
  PolymerEntityDraft,
} from "./types";

// The checks a form runs before it lets the depositor submit. They repeat
// the API's own rules (required title and method, resolution above zero,
// mutation format, organism name, allowed purposes and model types) so a
// mistake shows beside its field instead of coming back as a 422, and add
// the format checks the API leaves to the client: PDB ID, CCD ID, pH and
// temperatures. A space group that does not look like Hermann–Mauguin
// notation is only flagged, because the API stores any text; so is a
// temperature outside the wwPDB's usual ranges.

const HERMANN_MAUGUIN_PATTERN =
  /^[PABCFIRH](\s*-?[1-6]{1,2}(\/[mnabcde])?|\s*[mnabcde])+(\s*:\s*[HR12])?$/i;

const TAXONOMY_ID_PATTERN = /^[1-9][0-9]*$/;

const MODEL_FILE_MISSING_LINE = "add exactly one PDB/mmCIF model file";

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
  return messages;
}

export function modelFormMessages(draft: ModelDraft): FieldMessage[] {
  return [
    ...modelOverviewMessages(draft, { purpose: false, modelType: false, compounds: false }),
    ...modelFileMessages(draft),
    ...metricMessages(draft.metrics),
    ...programMessages(draft),
  ];
}

/**
 * The model edit form's checks. Files and runs are read-only there, and a
 * read-only value is not checked: the depositor could not fix it.
 */
export function modelEditMessages(
  draft: ModelEditDraft,
  locks: ModelLocks,
): FieldMessage[] {
  return [
    ...modelOverviewMessages(draft, {
      purpose: locks.purpose,
      modelType: locks.modelType,
      compounds: locks.fields.ligands !== undefined,
    }),
    ...metricMessages(draft.metrics),
  ];
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

function programMessages(draft: ModelDraft): FieldMessage[] {
  return draft.programs.flatMap((program, index) => {
    const itemLabel = `step ${index + 1}`;
    const messages: FieldMessage[] = [];
    if (!program.name.trim()) {
      messages.push(
        invalid("run.name", "name the program or remove this step", program.id, itemLabel),
      );
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
