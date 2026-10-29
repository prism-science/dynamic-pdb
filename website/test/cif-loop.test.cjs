require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { cifCategory, cifLoop, firstDataBlock, loopValue } = require("../src/lib/parse/cif.ts");

test("should_read_a_row_wrapped_over_several_lines_when_values_are_split_across_lines", () => {
  // given
  const text = ["data_X", "loop_", "_t.a", "_t.b", "_t.c", "1 2", "3", "4 5 6", "#"].join("\n");

  // when
  const loop = cifLoop(text, "_t");

  // then
  assert.deepEqual(loop.columns, ["a", "b", "c"]);
  assert.deepEqual(loop.rows, [
    ["1", "2", "3"],
    ["4", "5", "6"],
  ]);
});

test("should_read_a_text_block_as_one_value_when_a_row_contains_one", () => {
  // given
  const text = ["loop_", "_t.id", "_t.name", "_t.comp", "1", ";first line", "second line", ";", "1UD", "2 'x' GOL"].join("\n");

  // when
  const loop = cifLoop(text, "_t");

  // then
  assert.equal(loop.rows.length, 2);
  assert.equal(loopValue(loop, loop.rows[0], "name"), "first line\nsecond line");
  assert.equal(loopValue(loop, loop.rows[0], "comp"), "1UD");
  assert.equal(loopValue(loop, loop.rows[1], "comp"), "GOL");
});

test("should_keep_spaces_inside_quotes_when_values_are_quoted", () => {
  // given
  const text = ["loop_", "_t.a", "_t.b", "'two words' \"it's here\"", "?  .", "#"].join("\n");

  // when
  const loop = cifLoop(text, "_t");

  // then
  assert.deepEqual(loop.rows, [
    ["two words", "it's here"],
    ["?", "."],
  ]);
});

test("should_drop_trailing_values_when_they_do_not_fill_a_row", () => {
  // given
  const text = ["loop_", "_t.a", "_t.b", "1 2", "3 4", "5", "_u.x", "9"].join("\n");

  // when
  const loop = cifLoop(text, "_t");

  // then
  assert.deepEqual(loop.rows, [
    ["1", "2"],
    ["3", "4"],
  ]);
});

test("should_find_the_requested_loop_when_other_loops_come_first", () => {
  // given
  const text = ["loop_", "_a.x", "1", "2", "loop_", "_b.y", "_b.z", "3", "4", "loop_", "_c.w", "5"].join("\n");

  // when
  const loop = cifLoop(text, "_b");

  // then
  assert.deepEqual(loop.rows, [["3", "4"]]);
  assert.equal(cifLoop(text, "_missing"), null);
});

test("should_finish_quickly_when_a_4_MiB_loop_is_wrapped_one_value_per_line", () => {
  // given
  const header = "loop_\n_t.a\n_t.b\n_t.c\n_t.d\n";
  const body = "value\n".repeat(Math.ceil((4 << 20) / 6));
  const text = `${header}${body}`;

  // when
  const started = performance.now();
  const loop = cifLoop(text, "_t");
  const elapsed = performance.now() - started;

  // then
  assert.equal(loop.rows.length, Math.floor(Math.ceil((4 << 20) / 6) / 4));
  assert.ok(elapsed < 2000, `took ${elapsed} ms`);
});

test("should_finish_quickly_when_a_4_MiB_text_has_many_unterminated_quotes_and_loop_headers", () => {
  // given
  const text = "loop_\n_x.a\n'unterminated quote\n".repeat((4 << 20) / 32);

  // when
  const started = performance.now();
  const loop = cifLoop(text, "_t");
  const elapsed = performance.now() - started;

  // then
  assert.equal(loop, null);
  assert.ok(elapsed < 2000, `took ${elapsed} ms`);
});

test("should_read_pair_items_when_their_tags_are_indented_and_followed_by_comments", () => {
  // given: lib/cif reads tokens, so layout and comments do not matter.
  const text = [
    "data_X",
    "  _f.comp_id GOL # the glycerol",
    "\t_f.feature_type 'SUBJECT OF INVESTIGATION'",
    "_f.asym_id ?",
    "#",
  ].join("\n");

  // when
  const category = cifCategory(text, "_f");

  // then
  assert.deepEqual(category.columns, ["comp_id", "feature_type", "asym_id"]);
  assert.equal(loopValue(category, category.rows[0], "comp_id"), "GOL");
  assert.equal(loopValue(category, category.rows[0], "feature_type"), "SUBJECT OF INVESTIGATION");
  assert.equal(loopValue(category, category.rows[0], "asym_id"), null);
});

test("should_take_only_the_next_token_as_an_item_value_when_a_line_holds_more", () => {
  // given
  const text = ["data_X", "_f.comp_id GOL _f.feature_type OTHER", "#"].join("\n");

  // when
  const category = cifCategory(text, "_f");

  // then
  assert.deepEqual(category.columns, ["comp_id", "feature_type"]);
  assert.deepEqual(category.rows, [["GOL", "OTHER"]]);
});

test("should_not_read_loop_tags_as_items_when_a_loop_names_another_category", () => {
  // given
  const text = ["data_X", "loop_", "_g.id", "_g.name", "1 a", "2 b", "_f.comp_id GOL", "#"].join("\n");

  // when
  const category = cifCategory(text, "_f");

  // then
  assert.deepEqual(category, { columns: ["comp_id"], rows: [["GOL"]] });
});

test("should_end_the_first_data_block_at_the_second_data_header", () => {
  // given: a data_ value in quotes or a text block is not a header.
  const first = ["data_FIRST", "_x.a 'data_quoted'", ";", "data_in_block", ";", "_x.b 1", ""].join("\n");
  const text = first + ["data_SECOND", "_f.comp_id ATP", "#"].join("\n");

  // when
  const block = firstDataBlock(text);

  // then
  assert.equal(block, first);
  assert.equal(cifCategory(block, "_f"), null);
});

