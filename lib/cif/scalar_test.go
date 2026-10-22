package cif

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_scalar_like_full_parse_when_file_is_a_real_archive_entry(t *testing.T) {
	for _, name := range []string{"193L.cif", "1AKE.cif", "3CLN.cif", "4HHB.cif", "6OIM.cif"} {
		// given
		contents, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		text := string(contents)
		document := Parse(text)
		tags := slices.Concat(document.scalarTags, []string{"_missing.tag", "_refine_ls_restr.type", "_atom_site.id", "_struct_conn.id"})

		for _, tag := range tags {
			// when
			wantValue, wantOK := document.Scalar(tag)
			gotValue, gotOK := ScalarValue(text, tag)

			// then
			assert.Equal(t, wantOK, gotOK, "%s %s", name, tag)
			assert.Equal(t, wantValue, gotValue, "%s %s", name, tag)
		}
	}
}

func Test_should_read_scalar_like_full_parse_when_syntax_is_unusual(t *testing.T) {
	cases := map[string]string{
		"quoted value":              "data_X\n_a.b 'hello world'\n_a.c \"it's\"\n",
		"value on next line":        "data_X\n_a.b\n  42\n",
		"text block value":          "data_X\n_a.b\n;line one\nline two\n;\n_a.c 7\n",
		"text block then same line": "data_X\n_a.b\n;text\n; _a.c 7\n",
		"loop is not a scalar":      "data_X\nloop_\n_a.b\n_a.c\n1 2\n3 4\n_a.d 5\n",
		"loop values with quotes":   "data_X\nloop_\n_l.x\n'data_inside' ;semi\n_l.y\n_a.b 1\n",
		"loop with text block":      "data_X\nloop_\n_l.x\n_l.y\n1\n;_a.b 99\n;\n_a.b 2\n",
		"duplicate wins last":       "data_X\n_a.b 1\n_a.b 2\n",
		"unknown mark":              "data_X\n_a.b ?\n_a.c .\n",
		"comment after value":       "data_X\n_a.b 5 # note\n# _a.b 6\n",
		"second block ignored":      "data_X\n_a.b 1\ndata_Y\n_a.b 2\n",
		"crlf line endings":         "data_X\r\n_a.b 1\r\n_a.c\r\n;text\r\n;\r\n",
		"mixed case":                "DATA_X\n_A.B 1\nLOOP_\n_L.X\n1\n",
		"loop without tags":         "data_X\nloop_\n_a.b 1\n",
		"tag without value at end":  "data_X\n_a.b",
		"tag then loop":             "data_X\n_a.b\nloop_\n_l.x\n1\n",
		"unterminated text block":   "data_X\n_a.b\n;never closed\nmore\n",
		"save frame ends loop":      "data_X\nloop_\n_l.x\n1\nsave_f\n_a.b 3\n",
		"stop ends loop":            "data_X\nloop_\n_l.x\n1\nstop_\n_a.b 3\n",
		"value before data header":  "_a.b 1\ndata_X\n_a.c 2\n",
		"hash inside token":         "data_X\n_a.b abc#def\n",
		"quote unterminated":        "data_X\n_a.b 'abc\n_a.c 4\n",
	}
	for name, text := range cases {
		for _, tag := range []string{"_a.b", "_a.c", "_a.d", "_l.x"} {
			// when
			wantValue, wantOK := Parse(text).Scalar(tag)
			gotValue, gotOK := ScalarValue(text, tag)

			// then
			assert.Equal(t, wantOK, gotOK, "%s %s", name, tag)
			assert.Equal(t, wantValue, gotValue, "%s %s", name, tag)
		}
	}
}

func Test_should_read_scalar_when_line_is_longer_than_read_buffer(t *testing.T) {
	// given
	text := "data_X\n_a.b '" + strings.Repeat("x", 200*1024) + "'\n_a.c 1\n"

	// when
	value, ok := ScalarValue(text, "_a.b")
	other, otherOK := ScalarValue(text, "_a.c")

	// then
	require.True(t, ok)
	assert.Equal(t, strings.Repeat("x", 200*1024), value)
	require.True(t, otherOK)
	assert.Equal(t, "1", other)
}

func Test_should_read_scalar_from_reader_when_source_is_a_stream(t *testing.T) {
	// given
	source := strings.NewReader(syntheticMMCIF(1 << 20))

	// when
	value, ok, err := ScalarValueFromReader(source, "_refine.ls_R_factor_R_free")

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}

func Test_should_allocate_little_when_reading_one_scalar_from_a_large_file(t *testing.T) {
	// given
	text := syntheticMMCIF(40 << 20)
	var before, after runtimeMemStats

	// when
	readMemStats(&before)
	value, ok := ScalarValue(text, "_refine.ls_R_factor_R_free")
	readMemStats(&after)

	// then
	require.True(t, ok)
	require.Equal(t, "0.243", value)
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(1<<20), "bytes allocated reading one scalar from a 40 MiB file")
}

func Test_should_stay_under_allocation_budget_when_extracting_a_four_mebibyte_head(t *testing.T) {
	// given
	text := syntheticMMCIF(4 << 20)
	var before, after runtimeMemStats

	// when
	readMemStats(&before)
	_, result, err := ExtractText(text, true)
	readMemStats(&after)

	// then
	require.NoError(t, err)
	require.Len(t, result.Cofactors, 1)
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(16<<20), "bytes allocated extracting 4 MiB")
}

func Test_should_extract_same_result_as_full_parse_when_text_is_read_in_one_pass(t *testing.T) {
	for _, name := range []string{"193L.cif", "1AKE.cif", "3CLN.cif", "4HHB.cif", "6OIM.cif"} {
		text := readFixture(t, name)
		for _, cut := range []int{len(text), len(text) * 9 / 10, len(text) / 2, len(text) / 3, 7000, 100} {
			for _, truncated := range []bool{false, true} {
				// given
				head := text[:cut]
				var document *Document
				if truncated {
					document = ParseTruncated(head)
				} else {
					document = Parse(head)
				}
				want, err := Extract(document)
				require.NoError(t, err)
				want.Connections = nil

				// when
				_, got, err := ExtractText(head, truncated)

				// then
				require.NoError(t, err)
				assert.Equal(t, want, got, "%s cut at %d truncated=%v", name, cut, truncated)
			}
		}
	}
}
