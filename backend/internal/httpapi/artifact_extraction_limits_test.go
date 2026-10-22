package httpapi

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/validation"
)

func Test_should_refuse_extra_request_when_every_extraction_slot_is_taken(t *testing.T) {
	// given
	limits := newExtractionLimits(2, 100, time.Minute)
	releaseFirst, firstOK := limits.tryAcquire()
	_, secondOK := limits.tryAcquire()

	// when
	_, thirdOK := limits.tryAcquire()
	releaseFirst()
	releaseFirst()
	_, afterReleaseOK := limits.tryAcquire()
	_, fullAgainOK := limits.tryAcquire()

	// then
	assert.True(t, firstOK)
	assert.True(t, secondOK)
	assert.False(t, thirdOK)
	assert.True(t, afterReleaseOK, "releasing twice must free exactly one slot")
	assert.False(t, fullAgainOK)
}

func Test_should_limit_each_user_separately_when_requests_exceed_the_window_budget(t *testing.T) {
	// given
	limits := newExtractionLimits(2, 3, time.Minute)
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	var results []bool

	// when
	for range 4 {
		results = append(results, limits.userLimited(httptest.NewRecorder(), request, "alice"))
	}
	recorder := httptest.NewRecorder()
	limitedAgain := limits.userLimited(recorder, request, "alice")
	bobLimited := limits.userLimited(httptest.NewRecorder(), request, "bob")

	// then
	assert.Equal(t, []bool{false, false, false, true}, results)
	assert.True(t, limitedAgain)
	assert.Equal(t, "60", recorder.Header().Get("Retry-After"))
	assert.False(t, bobLimited)
}

func Test_should_not_limit_when_extraction_limits_are_not_installed(t *testing.T) {
	// given
	var limits *extractionLimits
	request := httptest.NewRequest(http.MethodPost, "/", nil)

	// when
	userLimited := limits.userLimited(httptest.NewRecorder(), request, "alice")
	release, acquired := limits.tryAcquire()

	// then
	assert.False(t, userLimited)
	assert.True(t, acquired)
	release()
}

func Test_should_install_default_extraction_limits_when_server_is_created(t *testing.T) {
	// when
	server := NewServer(nil, nil, auth.Config{}, nil, nil, nil, "", validation.ModeLogOnly)

	// then
	require.NotNil(t, server.extractionLimits)
	assert.Equal(t, defaultExtractionMaxInFlight, cap(server.extractionLimits.slots))
}

func syntheticArtifactMMCIF(targetBytes int) []byte {
	header := "data_SYNTH\n#\n_entry.id SYNTH\n#\n_software.name PROLSQ\n#\n" +
		"loop_\n_atom_site.group_PDB\n_atom_site.id\n_atom_site.type_symbol\n_atom_site.label_atom_id\n" +
		"_atom_site.label_alt_id\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.label_seq_id\n" +
		"_atom_site.Cartn_x\n_atom_site.Cartn_y\n_atom_site.Cartn_z\n_atom_site.occupancy\n_atom_site.auth_seq_id\n" +
		"_atom_site.auth_asym_id\n_atom_site.pdbx_PDB_model_num\n"
	var builder strings.Builder
	builder.Grow(targetBytes + 1024)
	builder.WriteString(header)
	names := []string{"N", "CA", "C", "O", "CB", "CG", "CD", "NE"}
	for atom := 0; builder.Len() < targetBytes; atom++ {
		residue := atom/len(names) + 1
		builder.WriteString("ATOM " + strconv.Itoa(atom+1) + " C " + names[atom%len(names)] + " . ARG A " +
			strconv.Itoa(residue) + " 20.123 30.456 40.789 1.00 " + strconv.Itoa(residue) + " A 1\n")
	}
	builder.WriteString("HETATM 99999 FE FE . HEM A 2 1.000 2.000 3.000 1.00 500 A 1\n#\n")
	return []byte(builder.String())
}

func Test_should_stay_under_allocation_budget_when_extracting_a_four_mebibyte_head(t *testing.T) {
	// given
	data := syntheticArtifactMMCIF(artifactExtractionReadLimit)
	var before, after runtime.MemStats

	// when
	runtime.GC()
	runtime.ReadMemStats(&before)
	attributes, err := buildArtifactExtraction(data, int64(len(data))+1024, testArtifactSource)
	runtime.ReadMemStats(&after)

	// then
	require.NoError(t, err)
	assert.True(t, attributes.Truncated)
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(16<<20), "bytes allocated extracting a 4 MiB head (was about 227 MiB with Parse and Extract)")
}

func Benchmark_extract_artifact_head(b *testing.B) {
	data := syntheticArtifactMMCIF(artifactExtractionReadLimit)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := buildArtifactExtraction(data, int64(len(data))+1024, testArtifactSource); err != nil {
			b.Fatal(err)
		}
	}
}
