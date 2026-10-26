package cif

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_scalar_items_when_values_are_quoted_or_bare(t *testing.T) {
	// given
	text := "data_X\n_struct.title 'It''s a \"title\"'\n_cell.length_a 10.5\n_exptl.method \"X-RAY DIFFRACTION\"\n_a.b ?\n_a.c .\n"

	// when
	document := Parse(text)

	// then
	title, ok := document.Scalar("_struct.title")
	require.True(t, ok)
	assert.Equal(t, "It''s a \"title\"", title)
	length, ok := document.Scalar("_CELL.length_a")
	require.True(t, ok)
	assert.Equal(t, "10.5", length)
	method, ok := document.Scalar("_exptl.method")
	require.True(t, ok)
	assert.Equal(t, "X-RAY DIFFRACTION", method)
	_, ok = document.Scalar("_a.b")
	assert.False(t, ok)
	_, ok = document.Scalar("_a.c")
	assert.False(t, ok)
}

func Test_should_keep_embedded_quote_when_quote_is_not_followed_by_whitespace(t *testing.T) {
	// given
	text := "data_X\nloop_\n_chem_comp.id\n_chem_comp.name\nGDP \"GUANOSINE-5'-DIPHOSPHATE\"\nABC 'a b'c d'\n"

	// when
	document := Parse(text)

	// then
	table := document.Table("_chem_comp")
	require.NotNil(t, table)
	require.Len(t, table.Rows, 2)
	assert.Equal(t, "GUANOSINE-5'-DIPHOSPHATE", table.Value(table.Rows[0], "name"))
	assert.Equal(t, "a b'c d", table.Value(table.Rows[1], "name"))
}

func Test_should_read_text_block_when_value_starts_with_semicolon(t *testing.T) {
	// given
	text := "data_X\n_struct.title\n;First line\nsecond line 'quoted'\n;\n_cell.length_a 1.0\nloop_\n_a.id\n_a.text\n1\n;multi\nline\n;\n2 plain\n"

	// when
	document := Parse(text)

	// then
	title, ok := document.Scalar("_struct.title")
	require.True(t, ok)
	assert.Equal(t, "First line\nsecond line 'quoted'", title)
	length, ok := document.Scalar("_cell.length_a")
	require.True(t, ok)
	assert.Equal(t, "1.0", length)
	table := document.Table("_a")
	require.NotNil(t, table)
	require.Len(t, table.Rows, 2)
	assert.Equal(t, "multi\nline", table.Value(table.Rows[0], "text"))
	assert.Equal(t, "plain", table.Value(table.Rows[1], "text"))
}

func Test_should_ignore_comments_when_outside_quotes(t *testing.T) {
	// given
	text := "data_X\n# header\n_a.b 1 # trailing\n_a.c 'x # y'\nloop_\n_l.id\n_l.v\n1 a # comment\n# whole line\n2 b\n"

	// when
	document := Parse(text)

	// then
	value, ok := document.Scalar("_a.b")
	require.True(t, ok)
	assert.Equal(t, "1", value)
	value, ok = document.Scalar("_a.c")
	require.True(t, ok)
	assert.Equal(t, "x # y", value)
	table := document.Table("_l")
	require.NotNil(t, table)
	assert.Len(t, table.Rows, 2)
}

func Test_should_read_rows_spanning_lines_when_loop_values_wrap(t *testing.T) {
	// given
	text := "data_X\nloop_\n_l.a\n_l.b\n_l.c\n1 2\n3\n4 5 6\n"

	// when
	document := Parse(text)

	// then
	table := document.Table("_l")
	require.NotNil(t, table)
	require.Len(t, table.Rows, 2)
	assert.Equal(t, []string{"1", "2", "3"}, table.Rows[0])
	assert.Equal(t, []string{"4", "5", "6"}, table.Rows[1])
}

func Test_should_expose_single_row_category_as_table_when_items_are_not_looped(t *testing.T) {
	// given
	text := "data_X\n_entity_poly.entity_id 1\n_entity_poly.type 'polypeptide(L)'\n"

	// when
	document := Parse(text)

	// then
	table := document.Table("_entity_poly")
	require.NotNil(t, table)
	require.Len(t, table.Rows, 1)
	assert.Equal(t, "polypeptide(L)", table.Value(table.Rows[0], "type"))
}

func Test_should_stop_at_second_data_block_when_file_has_several(t *testing.T) {
	// given
	text := "data_A\n_a.b 1\ndata_B\n_a.b 2\n"

	// when
	document := Parse(text)

	// then
	value, ok := document.Scalar("_a.b")
	require.True(t, ok)
	assert.Equal(t, "1", value)
}

func Test_should_drop_partial_row_and_mark_incomplete_when_input_is_truncated(t *testing.T) {
	// given
	text := "data_X\nloop_\n_l.a\n_l.b\n1 2\n3 4\n5"

	// when
	document := ParseTruncated(text)

	// then
	table := document.Table("_l")
	require.NotNil(t, table)
	assert.Len(t, table.Rows, 2)
	assert.False(t, table.Complete)
}

func Test_should_mark_loop_complete_when_it_ends_before_truncation_point(t *testing.T) {
	// given
	text := "data_X\nloop_\n_l.a\n1\n2\n#\n_z.a 1\n"

	// when
	document := ParseTruncated(text)

	// then
	table := document.Table("_l")
	require.NotNil(t, table)
	assert.True(t, table.Complete)
}

func Test_should_mark_loop_complete_when_truncated_text_continues_with_separator_comments(t *testing.T) {
	// given
	text := "data_X\nloop_\n_l.a\n1\n2\n#\n# padding padding\n"

	// when
	document := ParseTruncated(text)

	// then
	table := document.Table("_l")
	require.NotNil(t, table)
	assert.True(t, table.Complete)
}
