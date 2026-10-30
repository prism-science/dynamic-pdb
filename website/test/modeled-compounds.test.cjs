require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { compoundTags, modeledCompoundsView } = require("../src/lib/modeled-compounds.ts");

function copied(ccdId, chain, residue, overrides = {}) {
  return {
    ccd_id: ccdId,
    label_asym_id: null,
    auth_asym_id: chain,
    auth_seq_id: residue,
    pdb_ins_code: null,
    entity_id: null,
    source: "nonpoly_scheme",
    needs_review: false,
    review_reason: null,
    ...overrides,
  };
}

function done(copies, overrides = {}) {
  return {
    status: "done",
    parser_version: "1.3.0",
    complete: true,
    warnings: [],
    last_error: null,
    read_at: "2026-10-30T12:00:00Z",
    copies,
    ...overrides,
  };
}

const ENTRY_LISTS = { ligands: ["ATP", "ZN"], cofactors: ["HEM", "ZN"] };

test("should_group_copies_by_compound_and_tag_them_from_the_entry_lists_when_the_file_was_read", () => {
  // given
  const compounds = done([
    copied("ATP", "A", "401"),
    copied("GOL", "A", "301"),
    copied("GOL", "B", "301"),
    copied("HEM", "A", "142"),
    copied("QQQ", "A", "501"),
    copied("ZN", "A", "201", { pdb_ins_code: "A" }),
  ]);

  // when
  const view = modeledCompoundsView(compounds, ENTRY_LISTS);

  // then
  assert.equal(view.state, "read");
  assert.equal(view.complete, true);
  assert.deepEqual(
    view.groups.map((group) => [group.ccdId, group.copies.map((item) => item.position), group.tags]),
    [
      ["ATP", ["A 401"], ["Ligand"]],
      ["GOL", ["A 301", "B 301"], ["Buffer"]],
      ["HEM", ["A 142"], ["Cofactor"]],
      ["QQQ", ["A 501"], []],
      ["ZN", ["A 201A"], ["Ligand", "Cofactor"]],
    ],
  );
});

test("should_tag_nothing_when_the_entry_lists_are_not_known", () => {
  // when
  const view = modeledCompoundsView(done([copied("GOL", "A", "301")]), null);

  // then
  assert.deepEqual(view.groups[0].tags, []);
});

test("should_tag_a_buffer_the_entry_keeps_as_a_ligand_when_its_lists_name_it", () => {
  // when: a depositor flagged a glycerol in a pocket as the subject.
  const tags = compoundTags("GOL", { ligands: ["GOL"], cofactors: [] });

  // then
  assert.deepEqual(tags, ["Ligand"]);
});

test("should_keep_each_review_reason_once_when_several_copies_need_review", () => {
  // given
  const reason = "the file does not say which residues belong to its polymer chains: confirm this residue is not part of one";
  const compounds = done([
    copied("MSE", "A", "1", { needs_review: true, review_reason: reason, source: "pdb_hetatm" }),
    copied("MSE", "A", "38", { needs_review: true, review_reason: reason, source: "pdb_hetatm" }),
  ]);

  // when
  const view = modeledCompoundsView(compounds, ENTRY_LISTS);

  // then
  assert.deepEqual(view.groups[0].reviewReasons, [reason]);
  assert.equal(view.groups[0].copies[1].reviewReason, reason);
});

test("should_say_the_list_is_partial_when_a_limit_stopped_it", () => {
  // when
  const view = modeledCompoundsView(
    done([copied("SO4", "A", "1")], { complete: false, warnings: ["more than 100000 compound copies: the rest were not listed"] }),
    ENTRY_LISTS,
  );

  // then
  assert.equal(view.complete, false);
  assert.deepEqual(view.warnings, ["more than 100000 compound copies: the rest were not listed"]);
});

test("should_keep_the_typed_lists_when_the_file_is_not_ours_or_the_record_is_missing", () => {
  // then
  assert.deepEqual(modeledCompoundsView(undefined, ENTRY_LISTS), { state: "typed" });
  assert.deepEqual(modeledCompoundsView(done([], { status: "not_stored" }), ENTRY_LISTS), { state: "typed" });
});

test("should_say_the_file_is_being_read_or_why_it_failed_when_it_has_no_copies_yet", () => {
  // then
  assert.deepEqual(modeledCompoundsView(done([], { status: "pending" }), ENTRY_LISTS), { state: "reading", lastError: null });
  assert.deepEqual(
    modeledCompoundsView(done([], { status: "pending", last_error: "open the file: connection reset" }), ENTRY_LISTS),
    { state: "reading", lastError: "open the file: connection reset" },
  );
  assert.deepEqual(
    modeledCompoundsView(done([], { status: "failed", last_error: 'unknown model file format "ccp4"' }), ENTRY_LISTS),
    { state: "failed", reason: 'unknown model file format "ccp4"' },
  );
});

test("should_fail_loudly_when_the_status_is_unknown", () => {
  // then
  assert.throws(() => modeledCompoundsView(done([], { status: "guessing" }), ENTRY_LISTS), /unknown modeled compounds status/);
});
