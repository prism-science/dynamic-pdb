require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const ts = require("typescript");

const {
  activeFilterCount,
  browseSearchHref,
  criteriaFromPanel,
  filterParameters,
  formatModelTotal,
  MAX_FILTER_VALUES,
  MODEL_LIST_PAGING_PARAMETERS,
  MODEL_SEARCH_FILTERS,
  MODEL_SEARCH_PARAMETERS,
  modelSearchApiUrl,
  modelSearchQuery,
  panelProblems,
  panelValuesFromCriteria,
  readModelSearch,
  reviewFlagLines,
  searchFailureMessage,
  withDraftValues,
} = require("../src/lib/model-search.ts");

/** The query parameters of GET /v1/models, read from the generated API types. */
function apiModelListParameters() {
  const file = path.join(__dirname, "../src/lib/api/schema.gen.ts");
  const source = ts.createSourceFile(file, fs.readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true);
  const operations = source.statements.find(
    (node) => ts.isInterfaceDeclaration(node) && node.name.text === "operations",
  );
  const member = (members, name) => {
    const found = members.find((item) => item.name && item.name.getText(source) === name);
    assert.ok(found, `schema.gen.ts has no ${name}`);
    return found;
  };
  const operation = member(operations.members, "listModelsAcrossEntries");
  const parameters = member(operation.type.members, "parameters");
  const query = member(parameters.type.members, "query");
  return query.type.members.map((item) => item.name.getText(source));
}

test("should offer every filter parameter of GET /v1/models, and no other", () => {
  const apiFilters = apiModelListParameters().filter(
    (name) => !MODEL_LIST_PAGING_PARAMETERS.includes(name),
  );

  assert.deepEqual([...MODEL_SEARCH_PARAMETERS].sort(), [...apiFilters].sort());
  assert.equal(new Set(MODEL_SEARCH_PARAMETERS).size, MODEL_SEARCH_PARAMETERS.length);
});

test("should give every filter a unique id and a group the panel shows", () => {
  const ids = MODEL_SEARCH_FILTERS.map((filter) => filter.id);
  assert.equal(new Set(ids).size, ids.length);
  for (const filter of MODEL_SEARCH_FILTERS) {
    assert.ok(["entry", "model", "scores", "review"].includes(filter.group), filter.id);
    assert.ok(filterParameters(filter).length > 0, filter.id);
  }
});

test("should read the search from a URL verbatim, repeats kept, in the website's order", () => {
  const url = new URLSearchParams(
    "r_free_max=0.20&query=kinase&model_type=Ensemble&utm_source=mail&model_type=Multiconformer&organism=Homo+sapiens&deleted=e1",
  );

  const criteria = readModelSearch(url);

  assert.deepEqual(criteria, [
    ["organism", "Homo sapiens"],
    ["model_type", "Ensemble"],
    ["model_type", "Multiconformer"],
    ["r_free_max", "0.20"],
  ]);
});

test("should read the same search from the record a Next.js page receives", () => {
  const criteria = readModelSearch({
    query: "kinase",
    model_type: ["Ensemble", "Deposited"],
    resolution_max: "2",
    flagged: "true",
    unknown: "x",
  });

  assert.deepEqual(criteria, [
    ["resolution_max", "2"],
    ["model_type", "Ensemble"],
    ["model_type", "Deposited"],
    ["flagged", "true"],
  ]);
});

test("should forward a value the API will refuse unchanged, so the API explains it", () => {
  const criteria = readModelSearch(new URLSearchParams("resolution_max=abc&model_type=Foo&compound="));

  assert.deepEqual(criteria, [
    ["resolution_max", "abc"],
    ["model_type", "Foo"],
    ["compound", ""],
  ]);
});

test("should write one query string for the website's URL and the API's", () => {
  const criteria = readModelSearch(
    new URLSearchParams("ligand_or_cofactor=ADP&model_type=Ensemble&r_free_max=0.2&flagged=false"),
  );

  const query = modelSearchQuery(criteria);

  assert.equal(query, "ligand_or_cofactor=ADP&model_type=Ensemble&r_free_max=0.2&flagged=false");
  assert.equal(browseSearchHref(criteria), `/browse?${query}`);
  assert.equal(
    modelSearchApiUrl("https://dynamicpdb.com/api/", criteria),
    `https://dynamicpdb.com/api/v1/models?${query}`,
  );
  // Reading the written URL back gives the same search: a shared URL restores it.
  assert.deepEqual(readModelSearch(new URLSearchParams(query)), criteria);
});

test("should link to plain /browse and /v1/models for an empty search", () => {
  assert.equal(browseSearchHref([]), "/browse");
  assert.equal(modelSearchApiUrl("https://dynamicpdb.com/api", []), "https://dynamicpdb.com/api/v1/models");
});

test("should count the filters set, per group", () => {
  const criteria = readModelSearch(
    new URLSearchParams("resolution_min=1&resolution_max=2&model_type=Ensemble&model_type=Deposited&rmsf_max=1&flagged=true"),
  );

  assert.equal(activeFilterCount(criteria), 4);
  assert.equal(activeFilterCount(criteria, "entry"), 1);
  assert.equal(activeFilterCount(criteria, "model"), 1);
  assert.equal(activeFilterCount(criteria, "scores"), 1);
  assert.equal(activeFilterCount(criteria, "review"), 1);
});

test("should turn the panel back into the search, empty fields left out", () => {
  const values = panelValuesFromCriteria(
    readModelSearch(new URLSearchParams("model_type=Ensemble&organism=9606&r_free_max=0.2")),
  );
  values.resolution_min = ["  "];
  values.r_work_min = [" 0.1 "];
  values.flagged = [""];

  assert.deepEqual(criteriaFromPanel(values), [
    ["organism", "9606"],
    ["model_type", "Ensemble"],
    ["r_work_min", "0.1"],
    ["r_free_max", "0.2"],
  ]);
});

test("should add text typed but not added to its list when applying", () => {
  const values = { organism: ["Homo sapiens"], compound: ["ADP"] };

  const merged = withDraftValues(values, { organism: " Mus musculus ", compound: "adp", uniprot: "" });

  assert.deepEqual(merged.organism, ["Homo sapiens", "Mus musculus"]);
  assert.deepEqual(merged.compound, ["ADP"]);
  assert.equal(merged.uniprot, undefined);
  assert.deepEqual(values.organism, ["Homo sapiens"]);
});

test("should accept a panel the API would accept", () => {
  assert.deepEqual(
    panelProblems({
      resolution_min: ["1"],
      resolution_max: ["1"],
      r_free_max: ["2.5e-1"],
      clashscore_min: ["-.5"],
      organism: ["9606"],
    }),
    [],
  );
});

test("should name the field of each value the API would refuse", () => {
  const tooMany = Array.from({ length: MAX_FILTER_VALUES + 1 }, (_, index) => `P${index}`);

  const problems = panelProblems({
    resolution_min: ["3"],
    resolution_max: ["2"],
    r_free_max: ["0x10"],
    r_work_min: ["Infinity"],
    uniprot: tooMany,
    compound: ["A".repeat(101)],
    organism: ["000"],
    model_type: Array.from({ length: MAX_FILTER_VALUES + 1 }, () => "Ensemble"),
  });

  assert.deepEqual(problems, [
    { parameter: "resolution_min", message: "The minimum is above the maximum." },
    { parameter: "organism", message: "NCBI taxonomy IDs start at 1." },
    { parameter: "uniprot", message: "At most 20 values." },
    { parameter: "model_type", message: "At most 20 values." },
    { parameter: "compound", message: "A value is longer than 100 characters." },
    { parameter: "r_work_min", message: "Enter a number." },
    { parameter: "r_free_max", message: "Enter a number." },
  ]);
});

test("should say the total as the count line does, with 10,000+ above the cap", () => {
  assert.equal(formatModelTotal({ count: 0, exact: true }), "0 models");
  assert.equal(formatModelTotal({ count: 1, exact: true }), "1 model");
  assert.equal(formatModelTotal({ count: 1234, exact: true }), "1,234 models");
  assert.equal(formatModelTotal({ count: 10000, exact: false }), "10,000+ models");
});

test("should name known review issues and keep an unknown one's code", () => {
  const lines = reviewFlagLines([
    { object_type: "polymer_entity", object_id: "1", issue: "mutations_need_review", flagged_at: "x" },
    { object_type: "polymer_entity", object_id: "2", issue: "mutations_need_review", flagged_at: "x" },
    { object_type: "entry", object_id: "e", issue: "ligands_cofactors_rederive_from_archive", flagged_at: "x" },
    { object_type: "model_revision", object_id: "r", issue: "sync_validation_failed", flagged_at: "x" },
    { object_type: "chain", object_id: "c", issue: "new_issue_code", flagged_at: "x" },
  ]);

  assert.deepEqual(lines, [
    "Mutations need review (2 polymer entities)",
    "Ligands and Cofactors need review (entry)",
    "The latest PDB update failed validation (model version)",
    "new_issue_code (chain)",
  ]);
});

test("should explain each failed search with what to do next", () => {
  assert.deepEqual(
    searchFailureMessage({ status: 400, detail: "invalid model search parameter r_free_min: 0.3 is greater than r_free_max 0.2" }),
    {
      title: "This search could not run",
      body: "invalid model search parameter r_free_min: 0.3 is greater than r_free_max 0.2",
    },
  );
  assert.match(searchFailureMessage({ status: 429, retryAfterSeconds: 7 }).body, /Try again in 7 seconds\./);
  assert.match(searchFailureMessage({ status: 429, retryAfterSeconds: 1 }).body, /in 1 second\./);
  assert.match(searchFailureMessage({ status: 429 }).body, /links\.next/);
  const timeout = searchFailureMessage({ status: 504, retryAfterSeconds: 10 });
  assert.equal(timeout.title, "The search took too long");
  assert.match(timeout.body, /for example a model type or a resolution range, then try again in 10 seconds\./);
  assert.match(searchFailureMessage({ status: 504 }).body, /in a few seconds/);
  assert.equal(searchFailureMessage({ status: 500 }).title, "Search failed");
  assert.equal(searchFailureMessage({ status: null }).title, "Search failed");
});
