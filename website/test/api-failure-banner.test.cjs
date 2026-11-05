require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  apiFailure,
  requestFailure,
  sectionForPointer,
} = require("../src/app/components/entry-form/apiErrors.ts");
const { ApiRequestError } = require("../src/lib/api/entries.ts");

const NO_REQUEST = () => {
  throw new Error("the request is rebuilt only to place errors");
};

function bannerFor(status, errors = []) {
  return apiFailure({ status, errors, detail: `Backend responded with ${status}` }, () => ({ entry: {} })).error;
}

test("should_say_what_to_do_next_for_each_status_without_a_status_code", (t) => {
  // given
  t.mock.method(console, "error", () => {});
  const cases = [
    [null, "Dynamic PDB did not answer. Nothing you entered is lost: try again in a minute."],
    [401, "Your sign-in has expired. Sign in again in another tab, then submit again. Nothing you entered is lost."],
    [403, "Your account may not make this change. Ask a Dynamic PDB admin for access."],
    [404, "This entry or model is no longer there. Reload the page to see what is stored now."],
    [413, "The request is too large for Dynamic PDB. Remove the largest file and try again."],
    [429, "Dynamic PDB is busy. Wait a minute, then submit again. Nothing you entered is lost."],
    [
      500,
      "Dynamic PDB could not save this right now. Nothing you entered is lost: try again in a minute, and tell the Dynamic PDB team if it keeps failing.",
    ],
    [
      504,
      "Dynamic PDB could not save this right now. Nothing you entered is lost: try again in a minute, and tell the Dynamic PDB team if it keeps failing.",
    ],
  ];

  for (const [status, expected] of cases) {
    // when
    const failure = apiFailure({ status, errors: [], detail: "Backend responded with something" }, NO_REQUEST);

    // then
    assert.deepEqual(failure, { error: expected, fieldErrors: [] }, String(status));
  }
});

test("should_log_and_say_plainly_when_the_status_is_not_one_the_forms_expect", (t) => {
  // given
  const logged = [];
  t.mock.method(console, "error", (message) => logged.push(message));

  // when
  const banner = bannerFor(418);

  // then
  assert.equal(
    banner,
    "Dynamic PDB could not save this. Nothing you entered is lost: try again, and tell the Dynamic PDB team if it keeps failing.",
  );
  assert.deepEqual(logged, ["unexpected API status 418: Backend responded with 418"]);
});

test("should_name_the_section_and_never_the_pointer_when_no_field_shows_an_error", () => {
  // given
  const errors = [
    { status: "422", detail: "is not a field of the entry", source: { pointer: "/entry/crystallography/crystals/0/colour" } },
    { status: "422", detail: "is not valid", source: { pointer: "/entry" } },
  ];

  // when
  const banner = bannerFor(422, errors);

  // then
  assert.equal(banner, "Dynamic PDB did not accept the request. Experiment: is not a field of the entry; is not valid.");
  assert.doesNotMatch(banner, /\/entry|422/);
});

test("should_mark_the_fields_and_add_the_rest_when_some_errors_have_a_field", () => {
  // given
  const errors = [
    { status: "422", detail: "is required", source: { pointer: "/entry/method" } },
    { status: "422", detail: "entry is locked" },
  ];

  // when
  const failure = apiFailure({ status: 422, errors, detail: "" }, () => ({ entry: {} }));

  // then
  assert.equal(failure.error, "Dynamic PDB did not accept some values. They are marked below. Also: entry is locked.");
  assert.deepEqual(failure.fieldErrors.map((message) => message.field), ["entry.method"]);
});

test("should_show_a_taken_pdb_id_beside_the_pdb_id_field", () => {
  // given: what the API answers when another active entry has the PDB ID.
  const errors = [
    {
      status: "409",
      code: "CONFLICT",
      detail: "another entry already has this PDB ID; add your models to that entry instead",
      source: { pointer: "/entry/external_refs/pdb" },
    },
  ];

  // when
  const failure = apiFailure({ status: 409, errors, detail: "" }, () => ({ entry: { external_refs: { pdb: "7L0B" } } }));

  // then
  assert.equal(failure.error, "Dynamic PDB did not accept some values. They are marked below.");
  assert.deepEqual(
    failure.fieldErrors.map(({ field, message }) => ({ field, message })),
    [{ field: "entry.pdb", message: "another entry already has this PDB ID; add your models to that entry instead" }],
  );
});

test("should_quote_the_conflict_and_say_to_reload_when_a_409_has_no_field", () => {
  // given
  const errors = [{ status: "409", code: "CONFLICT", detail: "revision conflicts with the current active state" }];

  // when
  const banner = bannerFor(409, errors);
  const withoutDetail = bannerFor(409);

  // then
  assert.equal(
    banner,
    "This conflicts with what Dynamic PDB has stored: revision conflicts with the current active state. Reload the page and try again.",
  );
  assert.equal(
    withoutDetail,
    "This conflicts with what Dynamic PDB has stored: the entry changed since you opened it. Reload the page and try again.",
  );
});

test("should_read_status_and_errors_from_an_api_error_and_nothing_else_from_other_errors", () => {
  // given
  const errors = [{ status: "422", detail: "is required", source: { pointer: "/entry/method" } }];
  const apiError = new ApiRequestError("Backend responded with 422: is required (/entry/method)", 422, errors);
  const unreachable = new ApiRequestError("Backend request failed: connect ECONNREFUSED");

  // when / then
  assert.deepEqual(requestFailure(apiError), {
    status: 422,
    errors,
    detail: "Backend responded with 422: is required (/entry/method)",
  });
  assert.deepEqual(requestFailure(unreachable), {
    status: null,
    errors: [],
    detail: "Backend request failed: connect ECONNREFUSED",
  });
  assert.deepEqual(requestFailure(new Error("boom")), { status: null, errors: [], detail: "boom" });
});

test("should_find_no_section_for_a_pointer_that_shares_only_the_top_level_object", () => {
  // given / when / then
  assert.equal(sectionForPointer("/entry/colour"), null);
  assert.equal(sectionForPointer("/model/runs/0/colour"), "Experiment", "runs are in the Experiment section");
});
