package cif

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textTokens(text string) ([]token, bool) {
	tokens := newTokenizer(context.Background(), text, false)
	read := []token{}
	for {
		current, ok := tokens.next()
		if !ok {
			return read, tokens.separatorFollows()
		}
		read = append(read, current)
	}
}

func streamedTokens(t *testing.T, text string) ([]token, bool) {
	t.Helper()
	tokens := newLineTokenizer(context.Background(), strings.NewReader(text))
	read := []token{}
	for {
		current, ok := tokens.next()
		if !ok {
			require.NoError(t, tokens.readFailure())
			return read, tokens.separatorFollows()
		}
		read = append(read, current)
	}
}

func Test_should_yield_the_tokens_of_the_text_tokenizer_when_fixtures_are_streamed(t *testing.T) {
	// given
	names, err := filepath.Glob(filepath.Join("testdata", "*.cif"))
	require.NoError(t, err)
	synthetic, err := filepath.Glob(filepath.Join("testdata", "parity", "*.cif"))
	require.NoError(t, err)
	names = append(names, synthetic...)
	require.NotEmpty(t, names)

	for _, name := range names {
		text := readFixture(t, strings.TrimPrefix(name, "testdata"+string(filepath.Separator)))

		// when
		streamed, streamedSeparator := streamedTokens(t, text)

		// then
		expected, expectedSeparator := textTokens(text)
		assert.Equal(t, expected, streamed, name)
		assert.Equal(t, expectedSeparator, streamedSeparator, name)
	}
}

func Test_should_yield_the_tokens_of_the_text_tokenizer_when_lines_are_unusual(t *testing.T) {
	// given
	cases := map[string]string{
		"crlf line endings":             "data_X\r\nloop_\r\n_a.b\r\n_a.c\r\n1 'x y'\r\n;text\r\nfield\r\n;\r\n#\r\n",
		"no newline at the end":         "data_X\n_a.b value",
		"comment after a value":         "data_X\n_a.b value # not a separator\n_a.c 'quoted # kept'\n",
		"separator after the last row":  "data_X\nloop_\n_a.b\n1\n2\n#\n\n",
		"text field closed mid-line":    "data_X\n_a.b\n;first\nsecond\n; _a.c 3 # tail\n",
		"text field left open":          "data_X\n_a.b\n;first\nsecond\n",
		"text field open without a end": "data_X\n_a.b\n;first",
		"embedded quotes":               "data_X\n_a.b 'a b'c d'\n_a.c \"it's\"\n_a.d 'open\n",
		"tabs and empty lines":          "data_X\n\n\t_a.b\t1\n\n#comment\n",
		"line longer than the buffer":   "data_X\n_a.b " + strings.Repeat("x", 3*streamBufferBytes) + "\n_a.c 1\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			streamed, streamedSeparator := streamedTokens(t, text)

			// then
			expected, expectedSeparator := textTokens(text)
			assert.Equal(t, expected, streamed)
			assert.Equal(t, expectedSeparator, streamedSeparator)
		})
	}
}

func Test_should_fail_when_a_streamed_line_is_longer_than_the_limit(t *testing.T) {
	// given
	text := "data_X\n_a.b " + strings.Repeat("x", maxScanLineBytes+1) + "\n"

	// when
	_, err := parseStream(context.Background(), strings.NewReader(text), parseOptions{})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line is longer than")
}

func Test_should_fail_when_a_streamed_text_field_is_longer_than_the_limit(t *testing.T) {
	// given: many short lines inside one text field.
	line := strings.Repeat("y", 1000) + "\n"
	text := "data_X\n_a.b\n;\n" + strings.Repeat(line, maxScanLineBytes/len(line)+2) + ";\n"

	// when
	_, err := parseStream(context.Background(), strings.NewReader(text), parseOptions{})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text field is longer than")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("disk on fire")
}

func Test_should_fail_when_the_stream_cannot_be_read(t *testing.T) {
	// given: a stream that fails after its first line.
	source := io.MultiReader(strings.NewReader("data_X\n_a.b 1\n"), failingReader{})

	// when
	_, err := parseStream(context.Background(), source, parseOptions{})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk on fire")
}

func Test_should_fail_when_the_context_is_done_before_the_stream_ends(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// when
	_, err := parseStream(ctx, strings.NewReader("data_X\n_a.b 1\n"), parseOptions{})

	// then
	require.ErrorIs(t, err, context.Canceled)
}

func Test_should_keep_only_the_read_categories_and_stop_at_the_value_budget_when_a_file_is_streamed(t *testing.T) {
	// given: an unrelated item, and an entity table larger than a 40-byte budget.
	text := "data_X\n_unused.item " + strings.Repeat("u", 1000) + "\n#\n" +
		"loop_\n_entity.id\n_entity.type\n1 polymer\n2 non-polymer\n3 " + strings.Repeat("w", 50) + "\n#\n"
	options := parseOptions{onlyTables: map[string]struct{}{"_entity": {}}, keptValueBudget: 40}

	// when
	document, err := parseStream(context.Background(), strings.NewReader(text), options)

	// then
	require.NoError(t, err)
	assert.Empty(t, document.Scalars(), "an unrelated item is not kept")
	assert.True(t, document.valueBudgetHit)
	entities := document.Table("_entity")
	require.NotNil(t, entities)
	assert.False(t, entities.Complete)
	assert.Len(t, entities.Rows, 2)
}
