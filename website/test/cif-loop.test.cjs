require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { cifLoop, loopValue } = require("../src/lib/parse/cif.ts");

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
