package cif

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expiringContext reports context.DeadlineExceeded once Err has been called
// more than allowed times, so a test decides how far the parse gets.
type expiringContext struct {
	context.Context
	allowed int
	calls   int
}

func (c *expiringContext) Err() error {
	c.calls++
	if c.calls > c.allowed {
		return context.DeadlineExceeded
	}
	return nil
}

const timeBudgetWarning = "parser limit: " + timeBudgetNote

func archiveWithAtoms(atomRows int) string {
	return "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_seq_id\n_atom_site.auth_asym_id\n" +
		strings.Repeat("ATOM 1 A\n", atomRows) +
		"#\n"
}

func Test_should_warn_and_withhold_counts_when_the_context_is_done_before_extraction(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// when
	_, result, err := ExtractTextContext(ctx, archiveWithAtoms(10), false)

	// then
	require.NoError(t, err)
	assert.Contains(t, result.Warnings, timeBudgetWarning)
	assert.Nil(t, result.Atoms)
	assert.True(t, result.AtomsCutOff)
}

func Test_should_warn_and_withhold_counts_when_the_budget_ends_inside_the_coordinates(t *testing.T) {
	// given
	ctx := &expiringContext{Context: context.Background(), allowed: 2}
	text := archiveWithAtoms(10 * checkInterval)

	// when
	_, result, err := ExtractTextContext(ctx, text, false)

	// then
	require.NoError(t, err)
	assert.Contains(t, result.Warnings, timeBudgetWarning)
	assert.Nil(t, result.Atoms)
	assert.True(t, result.AtomsCutOff)
	assert.Less(t, ctx.calls, 10, "the parse should stop reading once the budget is gone")
}

func Test_should_not_apply_a_loop_the_budget_ended_at_a_row_boundary(t *testing.T) {
	// given: a one-column loop of modified residues, so the budget can only end
	// it between two rows.
	ctx := &expiringContext{Context: context.Background(), allowed: 2}
	text := "data_X\nloop_\n_pdbx_struct_mod_residue.label_comp_id\n" + strings.Repeat("LLP\n", 10*checkInterval) + "#\n"

	// when
	_, result, err := ExtractTextContext(ctx, text, false)

	// then
	require.NoError(t, err)
	assert.Contains(t, result.Warnings, timeBudgetWarning)
	assert.Empty(t, result.Cofactors, "the rows read before the budget ended may not be all of them")
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "_pdbx_struct_mod_residue (covalently bound cofactors)")
}

func Test_should_not_warn_when_the_file_is_read_within_the_budget(t *testing.T) {
	// given
	text := archiveWithAtoms(10 * checkInterval)

	// when
	_, result, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.NotContains(t, result.Warnings, timeBudgetWarning)
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 10*checkInterval, result.Atoms.AtomCount)
}

func Test_should_report_time_budget_in_extraction_when_the_context_is_done(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// when
	mmcif, mmcifErr := MMCIFExtractionContext(ctx, archiveWithAtoms(10), false)
	pdb, pdbErr := PDBExtractionContext(ctx, "ATOM      1  N   ALA A   1       1.000   1.000   1.000  1.00 10.00           N\nEND\n", false)

	// then
	require.NoError(t, mmcifErr)
	require.NoError(t, pdbErr)
	assert.Contains(t, mmcif.Warnings, timeBudgetWarning)
	assert.Contains(t, pdb.Warnings, timeBudgetWarning)
	assert.NotContains(t, mmcif.Fields, "atom_count")
	assert.NotContains(t, pdb.Fields, "atom_count")
}

func Test_should_return_an_error_when_the_scalar_scan_runs_out_of_time(t *testing.T) {
	// given
	ctx := &expiringContext{Context: context.Background(), allowed: 1}
	text := "data_X\n" + strings.Repeat("_a.b 'x'\n", 4*checkInterval)

	// when
	_, ok, err := ScalarValueFromReaderContext(ctx, strings.NewReader(text), hostileScalarTag)

	// then
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, ok)
}

// slowReader hands out a few bytes per call and pauses before each.
type slowReader struct {
	text  string
	pause time.Duration
}

func (r *slowReader) Read(buffer []byte) (int, error) {
	if r.text == "" {
		return 0, io.EOF
	}
	time.Sleep(r.pause)
	count := copy(buffer[:min(len(buffer), 64)], r.text)
	r.text = r.text[count:]
	return count, nil
}

func Test_should_stop_a_slow_scalar_scan_when_the_deadline_passes(t *testing.T) {
	// given
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	source := &slowReader{text: "data_X\n" + strings.Repeat("_a.b 'x'\n", 1_000_000), pause: time.Millisecond}

	// when
	start := time.Now()
	_, _, err := ScalarValueFromReaderContext(ctx, source, hostileScalarTag)

	// then
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)
}
