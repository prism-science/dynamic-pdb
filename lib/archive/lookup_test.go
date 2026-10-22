package archive

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/lib/rcsb"
)

type fakeRCSB struct {
	entry         rcsb.EntryDetails
	entryErr      error
	polymers      map[string]rcsb.PolymerEntityDetails
	polymerErr    error
	requestedPDBs []string
}

func (fake *fakeRCSB) GetEntryDetails(_ context.Context, pdbID string) (rcsb.EntryDetails, error) {
	fake.requestedPDBs = append(fake.requestedPDBs, pdbID)
	return fake.entry, fake.entryErr
}

func (fake *fakeRCSB) GetPolymerEntityDetails(
	_ context.Context,
	_ string,
	entityID string,
) (rcsb.PolymerEntityDetails, error) {
	return fake.polymers[entityID], fake.polymerErr
}

type fakeSIFTS struct {
	release *string
	err     error
}

func (fake fakeSIFTS) GetUniProtRelease(context.Context, string) (*string, error) {
	return fake.release, fake.err
}

func entryFixtureSource(t *testing.T) *fakeRCSB {
	t.Helper()
	return &fakeRCSB{
		entry: readFixture[rcsb.EntryDetails](t, "entry_4HHB.json"),
		polymers: map[string]rcsb.PolymerEntityDetails{
			"1": readFixture[rcsb.PolymerEntityDetails](t, "polymer_entity_4HHB_1.json"),
			"2": {},
		},
	}
}

func Test_should_look_up_entry_model_and_polymer_entities_when_pdb_entry_exists(t *testing.T) {
	// given
	source := entryFixtureSource(t)
	lookup := NewLookup(source, fakeSIFTS{release: new("2026.03")})

	// when
	result, err := lookup.Get(context.Background(), "4hhb")

	// then
	require.NoError(t, err)
	assert.Equal(t, "4HHB", result.PDBID)
	assert.Equal(t, []string{"4HHB"}, source.requestedPDBs)
	require.NotNil(t, result.Entry.Title)
	require.Len(t, result.PolymerEntities, 2)
	require.Len(t, result.PolymerEntities[0].UniProtMappings, 1)
	assert.Equal(t, new("2026_03"), result.PolymerEntities[0].UniProtMappings[0].UniProtRelease)
	assert.Empty(t, result.Warnings)
}

func Test_should_return_not_found_when_rcsb_has_no_entry(t *testing.T) {
	// given
	lookup := NewLookup(&fakeRCSB{entryErr: rcsb.ErrNotFound}, fakeSIFTS{})

	// when
	_, err := lookup.Get(context.Background(), "9ZZZ")

	// then
	require.Error(t, err)
	assert.ErrorIs(t, err, rcsb.ErrNotFound)
}

func Test_should_warn_instead_of_failing_when_uniprot_release_is_unavailable(t *testing.T) {
	// given
	lookup := NewLookup(entryFixtureSource(t), fakeSIFTS{err: errors.New("sifts down")})

	// when
	result, err := lookup.Get(context.Background(), "4HHB")

	// then
	require.NoError(t, err)
	assert.Nil(t, result.PolymerEntities[0].UniProtMappings[0].UniProtRelease)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "uniprot_release", result.Warnings[0].Field)
	assert.Contains(t, result.Warnings[0].Detail, "sifts down")
}

func Test_should_fail_when_polymer_entity_cannot_be_read(t *testing.T) {
	// given
	source := entryFixtureSource(t)
	source.polymerErr = errors.New("boom")
	lookup := NewLookup(source, fakeSIFTS{})

	// when
	_, err := lookup.Get(context.Background(), "4HHB")

	// then
	require.ErrorContains(t, err, "boom")
}
