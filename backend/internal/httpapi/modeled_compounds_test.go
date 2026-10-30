package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_hide_the_old_copies_when_a_file_is_being_read_again(t *testing.T) {
	// given: a set sent back to pending still holds the copies of its last read.
	set := domainmodels.ModeledCompoundSet{
		Status:   domainmodels.ModeledCompoundStatusPending,
		Warnings: []string{},
		Copies:   []domainmodels.ModeledCompoundCopy{{CCDID: "SO4", Source: domainmodels.ModeledCompoundSourcePDBHetatm}},
	}

	// when
	compounds, err := modeledCompoundsFromModel(set)

	// then
	require.NoError(t, err)
	assert.Equal(t, Pending, compounds.Status)
	assert.Empty(t, compounds.Copies)
}

func Test_should_fail_when_a_set_has_an_unknown_status_or_source(t *testing.T) {
	cases := map[string]domainmodels.ModeledCompoundSet{
		"status": {Status: "lost"},
		"source": {
			Status: domainmodels.ModeledCompoundStatusDone,
			Copies: []domainmodels.ModeledCompoundCopy{{CCDID: "ZN", Source: "guessed"}},
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := modeledCompoundsFromModel(set)

			// then
			require.Error(t, err)
		})
	}
}
