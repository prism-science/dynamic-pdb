package cif

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_sort_known_compounds_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// when / then
	assert.Equal(t, kindSolvent, reference.kind("hoh"))
	assert.Equal(t, kindSolvent, reference.kind("GOL"))
	assert.Equal(t, kindMetalIon, reference.kind("ZN"))
	assert.Equal(t, kindCofactor, reference.kind("hem"))
	for _, id := range []string{"HEM", "HEC", "NAD", "NAP", "NDP", "FAD", "FMN", "PLP", "SAM", "COA", "TPP"} {
		assert.Equal(t, kindCofactor, reference.kind(id), id)
	}
	assert.Equal(t, kindLigand, reference.kind("ATP"))
}

func Test_should_carry_version_and_pending_approval_when_reference_is_draft(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// then
	assert.NotEmpty(t, reference.Version)
	assert.False(t, reference.Approved)
}

func Test_should_not_list_a_compound_twice_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// when
	seen := map[string]string{}
	for id := range reference.cofactors {
		seen[id] += "cofactor "
	}
	for id := range reference.metals {
		seen[id] += "metal "
	}
	for id := range reference.solvents {
		seen[id] += "solvent "
	}

	// then
	for id, lists := range seen {
		assert.Len(t, strings.Fields(lists), 1, "%s is in several lists: %s", id, lists)
	}
}

func Test_should_reject_reference_without_version_when_parsing(t *testing.T) {
	// when
	_, err := parseReference([]byte(`{"cofactors": []}`), []byte(`{"version": "1", "solvents": []}`))

	// then
	require.Error(t, err)
}

func Test_should_reject_solvent_list_without_version_when_parsing(t *testing.T) {
	// when
	_, err := parseReference([]byte(`{"version": "1", "cofactors": []}`), []byte(`{"solvents": []}`))

	// then
	require.Error(t, err)
}

func Test_should_report_both_list_versions_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// then
	assert.NotEmpty(t, reference.CofactorVersion)
	assert.NotEmpty(t, reference.SolventVersion)
	assert.Contains(t, reference.Version, reference.CofactorVersion)
	assert.Contains(t, reference.Version, reference.SolventVersion)
}
