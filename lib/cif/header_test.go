package cif

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_header_values_when_archive_file_states_them(t *testing.T) {
	// when
	header := extractFixture(t, "193L.cif").Header

	// then
	assert.Equal(t, Header{
		EntryID:    "193L",
		Authors:    []string{"Vaney, M.C.", "Maignan, S.", "Ries-Kautt, M.", "Ducruix, A."},
		RWork:      new(0.184),
		RFree:      new(0.226),
		Resolution: new(1.33),
		Method:     "X-RAY DIFFRACTION",
		SpaceGroup: "P 43 21 2",
	}, header)
}

func Test_should_leave_a_header_value_out_when_the_file_gives_none(t *testing.T) {
	// when: 184D has no R-free ("?")
	header := extractFixture(t, "184D.cif").Header

	// then
	assert.Nil(t, header.RFree)
	require.NotNil(t, header.RWork)
	assert.InDelta(t, 0.214, *header.RWork, 1e-9)
}

func Test_should_read_header_values_the_way_the_preview_does(t *testing.T) {
	cases := map[string]struct {
		text string
		want Header
	}{
		"block name names the PDB ID": {
			text: "data_1abc\n_reflns.d_resolution_high 2.10\n_space_group.name_H-M_alt 'P 1'\n_audit_author.name 'Doe, J.'\n",
			want: Header{EntryID: "1ABC", Authors: []string{"Doe, J."}, Resolution: new(2.1), SpaceGroup: "P 1"},
		},
		"refinement program's own block name": {
			text: "data_phenix\n_refine.ls_R_factor_R_work 0.19\n",
			want: Header{Authors: []string{}, RWork: new(0.19)},
		},
		"looped categories name no single value": {
			text: "data_model\nloop_\n_refine.pdbx_refine_id\n_refine.ls_R_factor_R_work\n'X-RAY DIFFRACTION' 0.19\n'NEUTRON DIFFRACTION' 0.21\n" +
				"loop_\n_exptl.method\n'X-RAY DIFFRACTION'\n'NEUTRON DIFFRACTION'\n",
			want: Header{Authors: []string{}},
		},
		"text where a number belongs": {
			text: "data_model\n_entry.id 0ABC\n_refine.ls_d_res_high high\n",
			want: Header{Authors: []string{}},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, result, err := ExtractText(testCase.text, false)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.Header)
		})
	}
}
