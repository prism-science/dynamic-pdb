package artifact

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/cif"
)

func Test_should_extract_field_from_artifact_payload(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {
			Filename: "model.cif",
			Contents: []byte("_refine.ls_R_factor_R_free 0.243\n"),
		},
	})

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}

func extractMMCIFField(t *testing.T, contents []byte, field string) (any, bool) {
	t.Helper()
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {Filename: "model.cif", Contents: contents},
	})
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: field}},
	)
	require.NoError(t, err)
	return value, ok
}

func readLibraryFixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "lib", "cif", "testdata", name))
	require.NoError(t, err)
	return contents
}

func Test_should_keep_heme_in_cofactors_not_ligands_when_extracting_from_mmcif(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "4HHB.cif")

	// when
	cofactors, cofactorsFound := extractMMCIFField(t, contents, "cofactors")
	ligands, ligandsFound := extractMMCIFField(t, contents, "ligands")

	// then
	require.True(t, cofactorsFound)
	assert.Equal(t, []string{"HEM"}, cofactors)
	if ligandsFound {
		assert.NotContains(t, ligands, "HEM")
	}
}

func Test_should_keep_catalytic_metal_in_cofactors_when_extracting_from_mmcif(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "3CLN.cif")

	// when
	cofactors, cofactorsFound := extractMMCIFField(t, contents, "cofactors")
	_, ligandsFound := extractMMCIFField(t, contents, "ligands")

	// then
	require.True(t, cofactorsFound)
	assert.Equal(t, []string{"CA"}, cofactors)
	assert.False(t, ligandsFound)
}

func Test_should_report_empty_cofactors_when_mmcif_has_no_cofactors(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "1AKE.cif")

	// when
	cofactors, found := extractMMCIFField(t, contents, "cofactors")

	// then
	require.True(t, found)
	assert.Equal(t, []string{}, cofactors)
}

func Test_should_omit_atom_counts_when_read_limit_cuts_exactly_on_a_row_boundary(t *testing.T) {
	// given
	header := "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n_atom_site.pdbx_PDB_model_num\n"
	row := "ATOM C CA ALA A 1 1\n"
	remainder := (structureReadLimit - len(header)) % len(row)
	if remainder < 2 {
		remainder += len(row)
	}
	padding := "#" + strings.Repeat(" ", remainder-2) + "\n"
	body := padding + header
	rows := (structureReadLimit - len(body)) / len(row)
	text := body + strings.Repeat(row, rows)
	require.Equal(t, structureReadLimit, len(text))
	contents := []byte(text + row + row)
	untruncated, err := cif.MMCIFFields(text)
	require.NoError(t, err)

	// when
	_, atomCountFound := extractMMCIFField(t, contents, "atom_count")
	_, residuesFound := extractMMCIFField(t, contents, "modeled_residues")
	_, altlocFound := extractMMCIFField(t, contents, "altloc_fraction")

	// then
	assert.Contains(t, untruncated, "atom_count")
	assert.False(t, atomCountFound)
	assert.False(t, residuesFound)
	assert.False(t, altlocFound)
}

func extractorFor(artifact extractors.Artifact, cache *FieldCache) FieldExtractor {
	return NewCachedFieldExtractor(map[string]extractors.Artifact{"coordinates": artifact}, cache)
}

func extractField(t *testing.T, extractor FieldExtractor, field string) (any, bool) {
	t.Helper()
	value, ok, err := extractor.Extract(context.Background(), "4HHB",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{
			MMCIF: &manifest.ExtractRule{Field: field},
			PDB:   &manifest.ExtractRule{Field: field},
		},
	)
	require.NoError(t, err)
	return value, ok
}

func Test_should_parse_artifact_once_when_many_fields_are_read(t *testing.T) {
	// given
	path := filepath.Join(t.TempDir(), "model.cif")
	require.NoError(t, os.WriteFile(path, readLibraryFixture(t, "4HHB.cif"), 0o600))
	cache := NewFieldCache()
	extractor := extractorFor(extractors.Artifact{Filename: "model.cif", LocalPath: path}, cache)
	fields := []string{"atom_count", "modeled_residues", "unique_protein_chains", "altloc_fraction", "ligands", "cofactors", "program.name", "program.version", "compound_review", "_refine.ls_d_res_high"}

	// when
	for _, field := range fields {
		extractField(t, extractor, field)
	}
	require.NoError(t, os.Remove(path))
	count, found := extractField(t, extractor, "atom_count")

	// then
	require.True(t, found, "the file was deleted: every field must come from the first parse")
	assert.Equal(t, 420, count)
}

func Test_should_not_confuse_artifacts_when_two_files_share_a_name_and_length(t *testing.T) {
	// given
	cache := NewFieldCache()
	first := []byte("_refine.ls_R_factor_R_free 0.111\n")
	second := []byte("_refine.ls_R_factor_R_free 0.222\n")
	require.Equal(t, len(first), len(second))

	// when
	firstValue, _ := extractField(t, extractorFor(extractors.Artifact{Filename: "model.cif", Contents: first}, cache), "_refine.ls_R_factor_R_free")
	secondValue, _ := extractField(t, extractorFor(extractors.Artifact{Filename: "model.cif", Contents: second}, cache), "_refine.ls_R_factor_R_free")

	// then
	assert.Equal(t, "0.111", firstValue)
	assert.Equal(t, "0.222", secondValue)
}

func Test_should_share_one_parse_when_fields_are_read_concurrently(t *testing.T) {
	// given
	cache := NewFieldCache()
	artifact := extractors.Artifact{Filename: "model.cif", Contents: readLibraryFixture(t, "4HHB.cif")}
	var group sync.WaitGroup
	results := make([]any, 16)

	// when
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], _ = extractField(t, extractorFor(artifact, cache), "atom_count")
		}()
	}
	group.Wait()

	// then
	for _, result := range results {
		assert.Equal(t, 420, result)
	}
	assert.Len(t, cache.entries, 1)
}

func Test_should_expose_review_flags_when_pdb_metal_has_no_connection_rows(t *testing.T) {
	// given
	contents := []byte("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM    2 ZN    ZN B 101      16.104  13.207   9.447  1.00 20.00          ZN\n")
	extractor := extractorFor(extractors.Artifact{Filename: "model.pdb", Contents: contents}, NewFieldCache())

	// when
	cofactors, cofactorsFound := extractField(t, extractor, "cofactors")
	review, reviewFound := extractField(t, extractor, "compound_review")

	// then
	require.True(t, cofactorsFound)
	assert.Equal(t, []string{"ZN"}, cofactors)
	require.True(t, reviewFound)
	assert.Equal(t, []cif.ReviewItem{{
		List:   cif.ListCofactors,
		ID:     "ZN",
		Reason: "metal ion kept for review: the file has no metalc connection rows",
	}}, review)
}

func Test_should_keep_sulfate_and_flag_it_when_reference_lists_are_draft(t *testing.T) {
	// given
	contents := []byte("data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 SO4\n3 HOH\n")
	extractor := extractorFor(extractors.Artifact{Filename: "model.cif", Contents: contents}, NewFieldCache())

	// when
	ligands, ligandsFound := extractField(t, extractor, "ligands")
	review, _ := extractField(t, extractor, "compound_review")

	// then
	require.True(t, ligandsFound)
	assert.Equal(t, []string{"SO4"}, ligands)
	require.Len(t, review, 1)
}

func Test_should_omit_pdb_counts_when_artifact_is_longer_than_read_limit(t *testing.T) {
	// given
	line := "ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n"
	contents := []byte(strings.Repeat(line, structureReadLimit/len(line)+10) + "END\n")
	extractor := extractorFor(extractors.Artifact{Filename: "model.pdb", Contents: contents}, NewFieldCache())

	// when
	_, atomCountFound := extractField(t, extractor, "atom_count")

	// then
	assert.False(t, atomCountFound)
}

func Test_should_not_allocate_per_field_when_fields_come_from_cache(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "4HHB.cif")
	extractor := extractorFor(extractors.Artifact{Filename: "model.cif", Contents: contents}, NewFieldCache())
	fields := []string{"atom_count", "modeled_residues", "unique_protein_chains", "altloc_fraction", "ligands", "cofactors", "program.name", "program.version", "compound_review", "_refine.ls_d_res_high"}
	var before, after runtime.MemStats

	// when
	extractField(t, extractor, "atom_count")
	runtime.GC()
	runtime.ReadMemStats(&before)
	for _, field := range fields {
		extractField(t, extractor, field)
	}
	runtime.ReadMemStats(&after)

	// then
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<10), "ten cached field reads must not parse again")
}

func Test_should_fail_loudly_when_artifact_format_is_neither_pdb_nor_mmcif(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {Filename: "model.mmtf", Format: "mmtf", Contents: []byte("binary")},
	})

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), `format "mmtf"`)
}

func Test_should_report_not_found_when_artifact_has_no_format(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {Filename: "model", Contents: []byte("data")},
	})

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_should_stay_under_allocation_budget_when_a_four_mebibyte_artifact_is_hostile(t *testing.T) {
	const allocationBudget = 128 << 20
	hostile := map[string]string{
		"struct_conn a a a":  "data_X\nloop_\n_struct_conn.id\n" + strings.Repeat("a ", structureReadLimit/2),
		"one enormous token": "data_X\nloop_\n_entity.id\n" + strings.Repeat("v", structureReadLimit) + "\n",
		"ragged loop":        "data_X\nloop_\n_struct_ref_seq_dif.a\n_struct_ref_seq_dif.b\n_struct_ref_seq_dif.c\n" + strings.Repeat("xx yy ", structureReadLimit/6),
	}
	for name, text := range hostile {
		t.Run(name, func(t *testing.T) {
			// given
			extractor := NewFieldExtractor(map[string]extractors.Artifact{
				"coordinates": {Filename: "model.cif", Contents: []byte(text)},
			})
			var before, after runtime.MemStats

			// when
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, _, err := extractor.Extract(context.Background(), "5AMF",
				manifest.Source{Artifact: "coordinates"},
				manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "ligands"}},
			)
			runtime.ReadMemStats(&after)

			// then
			require.NoError(t, err)
			assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(allocationBudget))
		})
	}
}

type recordingHandler struct {
	mutex   sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.records = append(h.records, record)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func Test_should_log_draft_warning_once_when_several_artifacts_are_read(t *testing.T) {
	// given
	handler := &recordingHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cache := NewFieldCache()
	artifacts := map[string]extractors.Artifact{
		"first":  {Filename: "first.cif", Contents: readLibraryFixture(t, "3CLN.cif")},
		"second": {Filename: "second.cif", Contents: readLibraryFixture(t, "4HHB.cif")},
		"third":  {Filename: "third.cif", Contents: readLibraryFixture(t, "193L.cif")},
	}
	extractor := NewCachedFieldExtractor(artifacts, cache)

	// when
	for name := range artifacts {
		_, _, err := extractor.Extract(context.Background(), "5AMF",
			manifest.Source{Artifact: name},
			manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "ligands"}},
		)
		require.NoError(t, err)
	}

	// then
	draftLines := 0
	for _, record := range handler.records {
		record.Attrs(func(attribute slog.Attr) bool {
			if attribute.Key == "warning" && attribute.Value.String() == cif.DraftWarning {
				draftLines++
			}
			return true
		})
	}
	assert.Equal(t, 1, draftLines)
}

func Test_should_finish_quickly_when_a_four_mebibyte_artifact_is_one_line_of_quoted_tokens(t *testing.T) {
	for name, unit := range map[string]string{"single quotes": "'a' ", "double quotes": "\"a\" "} {
		t.Run(name, func(t *testing.T) {
			// given
			text := "data_X\nloop_\n_entity.id\n" + strings.Repeat(unit, structureReadLimit/len(unit))
			extractor := NewFieldExtractor(map[string]extractors.Artifact{
				"coordinates": {Filename: "model.cif", Contents: []byte(text)},
			})

			// when
			start := time.Now()
			_, _, err := extractor.Extract(context.Background(), "5AMF",
				manifest.Source{Artifact: "coordinates"},
				manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "ligands"}},
			)
			elapsed := time.Since(start)

			// then
			require.NoError(t, err)
			assert.Less(t, elapsed, 2*time.Second)
		})
	}
}
