package cif

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mutationHeader = "data_X\n" +
	"loop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer C54T\n" +
	"_struct_ref.id 1\n_struct_ref.entity_id 1\n" +
	"_struct_ref_seq.align_id 1\n_struct_ref_seq.ref_id 1\n" +
	"loop_\n_struct_ref_seq_dif.align_id\n_struct_ref_seq_dif.mon_id\n_struct_ref_seq_dif.pdbx_seq_db_name\n" +
	"_struct_ref_seq_dif.pdbx_seq_db_accession_code\n_struct_ref_seq_dif.db_mon_id\n" +
	"_struct_ref_seq_dif.pdbx_seq_db_seq_num\n_struct_ref_seq_dif.details\n" +
	"1 THR UNP P11111 CYS 54 'engineered mutation'\n" +
	"#\n"

const atomSiteStart = "loop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_comp_id\n" +
	"_atom_site.auth_asym_id\n_atom_site.auth_seq_id\n" +
	"ATOM N ALA A 1\nATOM C ALA A 2\n"

func Test_should_report_mutations_complete_when_whole_file_was_read(t *testing.T) {
	// given
	text := mutationHeader + atomSiteStart + "#\n"

	// when
	document, result, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.True(t, document.MutationsComplete())
	require.Len(t, result.Mutations, 1)
	assert.Equal(t, []string{"C54T"}, result.Mutations[0].Mutations)
}

func Test_should_report_mutations_complete_when_cut_off_head_reached_coordinates(t *testing.T) {
	// given
	text := mutationHeader + atomSiteStart

	// when
	document, _, err := ExtractText(text, true)

	// then
	require.NoError(t, err)
	assert.True(t, document.MutationsComplete())
}

func Test_should_report_mutations_incomplete_when_cut_off_head_ended_before_coordinates(t *testing.T) {
	// given
	text := mutationHeader

	// when
	document, _, err := ExtractText(text, true)

	// then
	require.NoError(t, err)
	assert.False(t, document.MutationsComplete())
}

func Test_should_report_mutations_incomplete_when_time_budget_ended_the_read(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// when
	document, _, err := ExtractTextContext(ctx, mutationHeader+atomSiteStart+"#\n", false)

	// then
	require.NoError(t, err)
	assert.False(t, document.MutationsComplete())
}

func Test_should_report_mutations_incomplete_when_parser_limit_skipped_rows(t *testing.T) {
	// given
	var wideLoop strings.Builder
	wideLoop.WriteString("loop_\n")
	for column := 0; column <= maxLoopColumns; column++ {
		fmt.Fprintf(&wideLoop, "_struct_ref_seq_dif.column_%d\n", column)
	}
	text := mutationHeader + wideLoop.String() + atomSiteStart + "#\n"

	// when
	document, _, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.False(t, document.MutationsComplete())
}

func Test_should_report_mutations_incomplete_when_there_is_no_document(t *testing.T) {
	// given
	var document *Document

	// when
	complete := document.MutationsComplete()

	// then
	assert.False(t, complete)
}
