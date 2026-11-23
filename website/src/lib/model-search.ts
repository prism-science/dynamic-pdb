import { METRIC_FIELDS } from "@/app/components/entry-form/types";
import {
  artifactLevelValues,
  metricKeyValues,
  modelPurposeValues,
  modelSearchSourceValues,
  structureMethodValues,
  structureModelTypeValues,
  type components,
  type operations,
} from "@/lib/api/schema.gen";

/**
 * The model search on /browse: which filters the panel offers and how a search
 * travels between the panel, the page's URL and GET /v1/models.
 *
 * One rule holds it together: the website's filter parameters are the API's,
 * with the same names and the same repeated form, and the page forwards their
 * values exactly as the URL holds them. So /browse?model_type=Ensemble&r_free_max=0.2
 * runs GET /v1/models?model_type=Ensemble&r_free_max=0.2, a shared URL restores
 * the same search, and the API's 400 explains a hand-edited value the API
 * refuses.
 *
 * New filters (chains, residues) are new entries in MODEL_SEARCH_FILTERS; the
 * panel renders them from there.
 */

type Schemas = components["schemas"];
type ModelListQuery = NonNullable<operations["listModelsAcrossEntries"]["parameters"]["query"]>;

/** GET /v1/models parameters that page the list rather than filter it. */
export const MODEL_LIST_PAGING_PARAMETERS = ["limit", "offset", "cursor"] as const;

/** A filter parameter of GET /v1/models: every query parameter but paging. */
export type ModelSearchParameter = Exclude<
  keyof ModelListQuery,
  (typeof MODEL_LIST_PAGING_PARAMETERS)[number]
>;

export type FilterGroupId = "entry" | "model" | "scores" | "review";

export const FILTER_GROUPS: readonly { id: FilterGroupId; label: string }[] = [
  { id: "entry", label: "Entry" },
  { id: "model", label: "Model" },
  { id: "scores", label: "Scores" },
  { id: "review", label: "Review" },
];

export type FilterChoice = { value: string; label: string };

type FilterBase = {
  /** Unique key of the control; the parameter name for one-parameter filters. */
  id: string;
  group: FilterGroupId;
  label: string;
  /** One line under the label, when the label alone could mislead. */
  help?: string;
  /** Offered only to reviewers and admins; the API answers 403 to anyone else. */
  reviewersOnly?: true;
};

/** Free text values, each its own repeated parameter (organism=A&organism=B). */
export type TextListFilter = FilterBase & {
  kind: "text_list";
  parameter: ModelSearchParameter;
  placeholder: string;
  /** Offered while typing; any other value is allowed too. */
  suggestions?: readonly string[];
};

/** Values from one of the API's vocabularies, as checkboxes. */
export type ChoiceListFilter = FilterBase & {
  kind: "choice_list";
  parameter: ModelSearchParameter;
  choices: readonly FilterChoice[];
};

/** An inclusive numeric range: <name>_min and <name>_max. */
export type RangeFilter = FilterBase & {
  kind: "range";
  minParameter: ModelSearchParameter;
  maxParameter: ModelSearchParameter;
  unit?: string;
};

/** A boolean parameter that may also be left out: any, true or false. */
export type YesNoFilter = FilterBase & {
  kind: "yes_no";
  parameter: ModelSearchParameter;
  anyLabel: string;
  yesLabel: string;
  noLabel: string;
};

export type ModelSearchFilter = TextListFilter | ChoiceListFilter | RangeFilter | YesNoFilter;

/** Most values one list filter takes, and longest value, as GET /v1/models allows. */
export const MAX_FILTER_VALUES = 20;
export const MAX_FILTER_VALUE_LENGTH = 100;

const SOURCE_LABELS: Record<Schemas["ModelSearchSource"], string> = {
  pdb_archive: "PDB archive",
  dynamic_pdb_deposit: "Dynamic PDB deposit",
};

const DATA_LEVEL_LABELS: Record<Schemas["ArtifactLevel"], string> = {
  L0: "L0 raw data",
  L1: "L1 processed data (reflections, maps)",
  L2: "L2 models",
  L3: "L3 evaluations",
};

const METRIC_LABELS = new Map<string, string>(
  METRIC_FIELDS.map((field) => [field.key, field.label]),
);

function metricLabel(key: Schemas["MetricKey"]): string {
  const label = METRIC_LABELS.get(key);
  if (label === undefined) {
    throw new Error(`no label for metric ${key}`);
  }
  return label;
}

function choices<Value extends string>(
  values: readonly Value[],
  label: (value: Value) => string,
): FilterChoice[] {
  return values.map((value) => ({ value, label: label(value) }));
}

function metricRange(key: Schemas["MetricKey"]): RangeFilter {
  const minParameter = `${key}_min` as const satisfies ModelSearchParameter;
  const maxParameter = `${key}_max` as const satisfies ModelSearchParameter;
  return { kind: "range", id: key, group: "scores", label: metricLabel(key), minParameter, maxParameter };
}

/** Archive entries keep the archive's own text for the method. */
const ARCHIVE_METHOD_SUGGESTIONS = ["ELECTRON CRYSTALLOGRAPHY", "NEUTRON DIFFRACTION"];

/** Every filter of the panel, in the order the panel shows them and the URL lists them. */
export const MODEL_SEARCH_FILTERS: readonly ModelSearchFilter[] = [
  {
    kind: "text_list",
    id: "method",
    parameter: "method",
    group: "entry",
    label: "Method",
    help: "As the entry states it, ignoring case.",
    placeholder: "X-ray crystallography",
    suggestions: [...structureMethodValues, ...ARCHIVE_METHOD_SUGGESTIONS],
  },
  {
    kind: "range",
    id: "resolution",
    minParameter: "resolution_min",
    maxParameter: "resolution_max",
    group: "entry",
    label: "Resolution",
    unit: "Å",
    help: "Entries without a resolution never match.",
  },
  {
    kind: "text_list",
    id: "organism",
    parameter: "organism",
    group: "entry",
    label: "Source organism",
    help: "Scientific name or NCBI taxonomy ID.",
    placeholder: "Homo sapiens or 9606",
  },
  {
    kind: "text_list",
    id: "uniprot",
    parameter: "uniprot",
    group: "entry",
    label: "UniProt accession",
    placeholder: "P69905",
  },
  {
    kind: "text_list",
    id: "ligand_or_cofactor",
    parameter: "ligand_or_cofactor",
    group: "entry",
    label: "In Ligands or Cofactors",
    help: "Chemical component ID in the entry's curated lists. Entries whose lists are unknown never match.",
    placeholder: "ADP",
  },
  {
    kind: "choice_list",
    id: "model_type",
    parameter: "model_type",
    group: "model",
    label: "Model type",
    choices: choices(structureModelTypeValues, (value) => value),
  },
  {
    kind: "choice_list",
    id: "purpose",
    parameter: "purpose",
    group: "model",
    label: "Purpose",
    choices: choices(modelPurposeValues, (value) => value),
  },
  {
    kind: "choice_list",
    id: "source",
    parameter: "source",
    group: "model",
    label: "Source",
    help: "PDB archive: taken unchanged from a PDB deposition, re-refinements included.",
    choices: choices(modelSearchSourceValues, (value) => SOURCE_LABELS[value]),
  },
  {
    kind: "choice_list",
    id: "data_level",
    parameter: "data_level",
    group: "model",
    label: "Data level",
    help: "Models with at least one attached file of the level.",
    choices: choices(artifactLevelValues, (value) => DATA_LEVEL_LABELS[value]),
  },
  {
    kind: "text_list",
    id: "compound",
    parameter: "compound",
    group: "model",
    label: "Compound in the coordinate file",
    help: "Chemical component ID read from the model's file.",
    placeholder: "ADP",
  },
  ...metricKeyValues.map(metricRange),
  {
    kind: "choice_list",
    id: "metric_missing",
    parameter: "metric_missing",
    group: "scores",
    label: "No value for",
    help: "Models missing at least one of these scores. A range never matches a missing score.",
    choices: choices(metricKeyValues, metricLabel),
  },
  {
    kind: "yes_no",
    id: "flagged",
    parameter: "flagged",
    group: "review",
    label: "Review flags",
    help: "Shown to reviewers and admins only.",
    reviewersOnly: true,
    anyLabel: "Any",
    yesLabel: "Only flagged records",
    noLabel: "Only records without flags",
  },
];

/** The parameters a filter sets, in URL order. */
export function filterParameters(filter: ModelSearchFilter): ModelSearchParameter[] {
  switch (filter.kind) {
    case "text_list":
    case "choice_list":
    case "yes_no":
      return [filter.parameter];
    case "range":
      return [filter.minParameter, filter.maxParameter];
    default:
      return unknownFilterKind(filter);
  }
}

function unknownFilterKind(filter: never): never {
  throw new Error(`unknown model search filter kind: ${(filter as { kind?: unknown }).kind}`);
}

/** Every filter parameter, in the one order the website writes them. */
export const MODEL_SEARCH_PARAMETERS: readonly ModelSearchParameter[] =
  MODEL_SEARCH_FILTERS.flatMap(filterParameters);

const KNOWN_PARAMETERS = new Set<string>(MODEL_SEARCH_PARAMETERS);

/**
 * The filters the panel offers this reader. The flagged filter is for
 * reviewers and admins (researcher answer 7), whose sign-in carries the
 * review_flags.read permission; the API checks the role on every request.
 */
export function offeredFilters(canFilterReviewFlags: boolean): readonly ModelSearchFilter[] {
  return canFilterReviewFlags
    ? MODEL_SEARCH_FILTERS
    : MODEL_SEARCH_FILTERS.filter((filter) => !filter.reviewersOnly);
}

const REVIEWER_ONLY_PARAMETERS = new Set<string>(
  MODEL_SEARCH_FILTERS.filter((filter) => filter.reviewersOnly).flatMap(filterParameters),
);

/** Whether a search uses a filter that only reviewers and admins may use. */
export function usesReviewerFilters(criteria: ModelSearchCriteria): boolean {
  return criteria.some(([parameter]) => REVIEWER_ONLY_PARAMETERS.has(parameter));
}

/** The same search without the filters only reviewers and admins may use. */
export function withoutReviewerFilters(criteria: ModelSearchCriteria): ModelSearchCriteria {
  return criteria.filter(([parameter]) => !REVIEWER_ONLY_PARAMETERS.has(parameter));
}

/** One filter value as the URL holds it: [parameter, value]. */
export type ModelSearchCriterion = readonly [ModelSearchParameter, string];

/** A search: its filter values, in the website's parameter order. */
export type ModelSearchCriteria = readonly ModelSearchCriterion[];

type SearchParamValue = string | string[] | undefined;

/** URLSearchParams, or the record a Next.js page receives as searchParams. */
export type SearchParamsInput = URLSearchParams | Record<string, SearchParamValue>;

function valuesOf(input: SearchParamsInput, parameter: string): string[] {
  if (input instanceof URLSearchParams) {
    return input.getAll(parameter);
  }
  const value = input[parameter];
  if (value === undefined) {
    return [];
  }
  return Array.isArray(value) ? value : [value];
}

/**
 * The search a URL asks for. Values are kept exactly as given, repeats
 * included, so the API sees what the URL says; parameters the website does not
 * know (query, utm_source, typos) are left out.
 */
export function readModelSearch(input: SearchParamsInput): ModelSearchCriteria {
  return MODEL_SEARCH_PARAMETERS.flatMap((parameter) =>
    valuesOf(input, parameter).map((value): ModelSearchCriterion => [parameter, value]),
  );
}

export function isModelSearchParameter(name: string): name is ModelSearchParameter {
  return KNOWN_PARAMETERS.has(name);
}

/**
 * The query string of a search, without "?": the same for the website's URL
 * and the API's, in the website's parameter order.
 */
export function modelSearchQuery(criteria: ModelSearchCriteria): string {
  const params = new URLSearchParams();
  for (const [parameter, value] of criteria) {
    params.append(parameter, value);
  }
  return params.toString();
}

/** /browse with the search, or plain /browse for none. */
export function browseSearchHref(criteria: ModelSearchCriteria): string {
  const query = modelSearchQuery(criteria);
  return query ? `/browse?${query}` : "/browse";
}

/** The same search as a GET /v1/models URL, for scripts. */
export function modelSearchApiUrl(apiBaseUrl: string, criteria: ModelSearchCriteria): string {
  const query = modelSearchQuery(criteria);
  return `${apiBaseUrl.replace(/\/+$/, "")}/v1/models${query ? `?${query}` : ""}`;
}

/** How many filters each group has set, for the panel's group headings. */
export function activeFilterCount(criteria: ModelSearchCriteria, group?: FilterGroupId): number {
  const set = new Set(criteria.map(([parameter]) => parameter));
  return MODEL_SEARCH_FILTERS.filter(
    (filter) =>
      (group === undefined || filter.group === group) &&
      filterParameters(filter).some((parameter) => set.has(parameter)),
  ).length;
}

/**
 * What the panel holds: each parameter's values. Lists hold one string per
 * value; a range end and a yes/no hold one string, empty when unset.
 */
export type PanelValues = Partial<Record<ModelSearchParameter, string[]>>;

export function panelValuesFromCriteria(criteria: ModelSearchCriteria): PanelValues {
  const values: PanelValues = {};
  for (const [parameter, value] of criteria) {
    (values[parameter] ??= []).push(value);
  }
  return values;
}

/**
 * The search the panel describes, empty values left out. Only the given
 * filters' parameters count, so a filter the panel does not offer this reader
 * (one only reviewers may use, kept from a shared URL) is dropped on Apply.
 */
export function criteriaFromPanel(
  values: PanelValues,
  filters: readonly ModelSearchFilter[] = MODEL_SEARCH_FILTERS,
): ModelSearchCriteria {
  return filters.flatMap(filterParameters).flatMap((parameter) =>
    (values[parameter] ?? [])
      .map((value) => value.trim())
      .filter((value) => value !== "")
      .map((value): ModelSearchCriterion => [parameter, value]),
  );
}

/** Something in the panel the API would refuse, on the parameter to show it at. */
export type PanelProblem = { parameter: ModelSearchParameter; message: string };

// A decimal number as both JavaScript and Go's strconv.ParseFloat read it:
// no hex, no "Infinity", no digit separators.
const DECIMAL_NUMBER = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/;
const DIGITS_ONLY = /^\d+$/;

/**
 * The panel's own checks, the ones GET /v1/models answers 400 for, so most
 * mistakes are shown at the field instead of after a round trip.
 */
export function panelProblems(values: PanelValues): PanelProblem[] {
  const problems: PanelProblem[] = [];
  for (const filter of MODEL_SEARCH_FILTERS) {
    problems.push(...filterProblems(filter, values));
  }
  return problems;
}

function filterProblems(filter: ModelSearchFilter, values: PanelValues): PanelProblem[] {
  switch (filter.kind) {
    case "text_list":
      return textListProblems(filter, values[filter.parameter] ?? []);
    case "choice_list":
      return listLengthProblems(filter.parameter, values[filter.parameter] ?? []);
    case "range":
      return rangeProblems(filter, values);
    case "yes_no":
      return [];
    default:
      return unknownFilterKind(filter);
  }
}

function listLengthProblems(parameter: ModelSearchParameter, list: string[]): PanelProblem[] {
  return list.length > MAX_FILTER_VALUES
    ? [{ parameter, message: `At most ${MAX_FILTER_VALUES} values.` }]
    : [];
}

function textListProblems(filter: TextListFilter, list: string[]): PanelProblem[] {
  const problems = listLengthProblems(filter.parameter, list);
  const values = list.map((value) => value.trim());
  if (values.some((value) => value.length > MAX_FILTER_VALUE_LENGTH)) {
    problems.push({
      parameter: filter.parameter,
      message: `A value is longer than ${MAX_FILTER_VALUE_LENGTH} characters.`,
    });
  }
  if (filter.parameter === "organism" && values.some((value) => DIGITS_ONLY.test(value) && Number(value) === 0)) {
    problems.push({ parameter: filter.parameter, message: "NCBI taxonomy IDs start at 1." });
  }
  return problems;
}

function rangeProblems(filter: RangeFilter, values: PanelValues): PanelProblem[] {
  const problems: PanelProblem[] = [];
  const ends = [filter.minParameter, filter.maxParameter].map((parameter) => {
    const text = (values[parameter]?.[0] ?? "").trim();
    if (text === "") {
      return null;
    }
    if (!DECIMAL_NUMBER.test(text)) {
      problems.push({ parameter, message: "Enter a number." });
      return null;
    }
    return Number(text);
  });
  const [minimum, maximum] = ends;
  if (minimum !== null && maximum !== null && minimum > maximum) {
    problems.push({ parameter: filter.minParameter, message: "The minimum is above the maximum." });
  }
  return problems;
}

/** The total of a search as the count line says it: "1 model", "1,234 models", "10,000+ models". */
export function formatModelTotal(total: Schemas["ModelSearchTotal"]): string {
  const count = total.count.toLocaleString("en-US");
  if (!total.exact) {
    return `${count}+ models`;
  }
  return total.count === 1 ? "1 model" : `${count} models`;
}

/** The count line above the results: "No models match these filters.", "1 model matches these filters". */
export function modelTotalLine(total: Schemas["ModelSearchTotal"]): string {
  if (total.count === 0) {
    return "No models match these filters.";
  }
  const verb = total.exact && total.count === 1 ? "matches" : "match";
  return `${formatModelTotal(total)} ${verb} these filters`;
}

/** What a failed search tells the reader: a heading and what to do. */
export type SearchFailure = {
  status: number | null;
  /** The API's own explanation, for a 400 or a 403. */
  detail?: string | null;
  retryAfterSeconds?: number | null;
};

export type SearchFailureMessage = { title: string; body: string };

function waitPhrase(seconds: number | null | undefined): string {
  if (seconds == null || seconds <= 0) {
    return "in a few seconds";
  }
  return seconds === 1 ? "in 1 second" : `in ${seconds} seconds`;
}

export function searchFailureMessage(failure: SearchFailure): SearchFailureMessage {
  if (failure.status === 400) {
    return {
      title: "This search could not run",
      body: failure.detail?.trim() || "The API refused one of the filters.",
    };
  }
  if (failure.status === 403) {
    // Today only the flagged filter, which is for reviewers and admins.
    return {
      title: "This search needs a reviewer or admin",
      body:
        failure.detail?.trim() ||
        "One of the filters is for reviewers and admins. Sign in with such an account, or search without it.",
    };
  }
  if (failure.status === 429) {
    // The website searches as one caller for every visitor, so this is the
    // site's shared budget, not something this reader did.
    return {
      title: "Too many searches right now",
      body:
        `Too many searches are running on Dynamic PDB at the moment. Try again ${waitPhrase(failure.retryAfterSeconds)}. ` +
        "To read many results from a script, use the API link and follow links.next.",
    };
  }
  if (failure.status === 504) {
    return {
      title: "The search took too long",
      body:
        "Add filters that narrow it, for example a model type or a resolution range, " +
        `then try again ${waitPhrase(failure.retryAfterSeconds)}.`,
    };
  }
  return { title: "Search failed", body: "Something went wrong on our side. Try again." };
}

/**
 * The panel's values with the text still being typed into each list added as
 * one more value, as if its Add button had been pressed. Apply uses it, so a
 * value typed but not added is not lost.
 */
export function withDraftValues(
  values: PanelValues,
  drafts: Readonly<Record<string, string>>,
): PanelValues {
  const merged: PanelValues = { ...values };
  for (const filter of MODEL_SEARCH_FILTERS) {
    if (filter.kind !== "text_list") {
      continue;
    }
    const draft = (drafts[filter.id] ?? "").trim();
    if (draft === "") {
      continue;
    }
    const list = merged[filter.parameter] ?? [];
    const known = list.some((value) => value.toLowerCase() === draft.toLowerCase());
    merged[filter.parameter] = known ? list : [...list, draft];
  }
  return merged;
}
