package cif

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hostileFileBytes = 4 << 20

// Budgets for one parse of a hostile four mebibyte file. They are the numbers
// the extract endpoint's two-slot limit relies on (see
// backend/internal/httpapi/artifact_extraction_limits.go).
const (
	hostileLiveHeapBudget = 48 << 20
	hostileAllocBudget    = 128 << 20
)

type memoryUse struct {
	liveHeap   uint64
	totalAlloc uint64
}

// measureMemory runs work and reports the heap still reachable from its result
// after a collection, and everything it allocated along the way. work returns
// the value to keep alive while the live heap is read.
func measureMemory(work func() any) memoryUse {
	var before, after runtime.MemStats
	readMemStats(&before)
	kept := work()
	readMemStats(&after)
	runtime.KeepAlive(kept)
	live := uint64(0)
	if after.HeapAlloc > before.HeapAlloc {
		live = after.HeapAlloc - before.HeapAlloc
	}
	return memoryUse{liveHeap: live, totalAlloc: after.TotalAlloc - before.TotalAlloc}
}

func repeatToSize(unit string, size int) string {
	return strings.Repeat(unit, size/len(unit))
}

func hostileLoop(category string, columns int, body string) string {
	var builder strings.Builder
	builder.WriteString("data_X\nloop_\n")
	for column := range columns {
		fmt.Fprintf(&builder, "%s.c%d\n", category, column)
	}
	builder.WriteString(body)
	return builder.String()
}

func hostileFiles() map[string]string {
	files := map[string]string{}
	for _, category := range []string{"_struct_conn", "_struct_ref_seq_dif", "_chem_comp", "_entity", "_pdbx_entity_nonpoly", "_struct_ref", "_struct_ref_seq", "_software"} {
		files["a a a loop of "+category] = "data_X\nloop_\n" + category + ".id\n" + repeatToSize("a ", hostileFileBytes)
		files["wide rows of "+category] = hostileLoop(category, 40, repeatToSize("x ", hostileFileBytes))
		files["ragged loop of "+category] = hostileLoop(category, 7, repeatToSize("xx yy zz ", hostileFileBytes))
	}
	files["metalc rows"] = "data_X\nloop_\n_struct_conn.id\n_struct_conn.conn_type_id\n_struct_conn.ptnr1_label_asym_id\n_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr1_auth_seq_id\n" +
		"_struct_conn.ptnr2_label_asym_id\n_struct_conn.ptnr2_label_comp_id\n_struct_conn.ptnr2_auth_seq_id\n" +
		repeatToSizeCounted("c%d metalc A ZN %d B HIS %d\n", hostileFileBytes)
	files["loop with a million columns"] = "data_X\nloop_\n" + repeatToSize("_x.a\n", hostileFileBytes)
	files["one enormous value"] = "data_X\n_entity.id " + strings.Repeat("v", hostileFileBytes) + "\n"
	files["one enormous tag"] = "data_X\n_entity." + strings.Repeat("t", hostileFileBytes) + " 1\n"
	files["one enormous loop token"] = "data_X\nloop_\n_entity.id\n" + strings.Repeat("v", hostileFileBytes) + "\n"
	files["one enormous quoted token"] = "data_X\nloop_\n_entity.id\n'" + strings.Repeat("v", hostileFileBytes) + "'\n"
	files["one enormous text block"] = "data_X\n_entity.id\n;" + strings.Repeat("v", hostileFileBytes) + "\n;\n"
	files["distinct scalar items"] = "data_X\n" + repeatToSizeCounted("_a.t%d %d\n", hostileFileBytes)
	files["repeated scalar item"] = "data_X\n" + repeatToSize("_a.same 1\n", hostileFileBytes)
	files["distinct data blocks"] = repeatToSizeCounted("data_B%d\n_a.b 1\n", hostileFileBytes)
	files["atom_site distinct residues with altlocs"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_seq_id\n_atom_site.label_alt_id\n_atom_site.pdbx_PDB_model_num\n" +
		repeatToSizeCounted("ATOM %d A 1\n", hostileFileBytes)
	files["atom_site distinct alt ids"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_seq_id\n_atom_site.label_alt_id\n" +
		repeatToSizeCounted("ATOM 1 a%d\n", hostileFileBytes)
	files["atom_site distinct compound ids"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.auth_seq_id\n" +
		repeatToSizeCounted("HETATM L%d %d\n", hostileFileBytes)
	files["atom_site distinct chains"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_asym_id\n_atom_site.auth_seq_id\n" +
		repeatToSizeCounted("ATOM c%d %d\n", hostileFileBytes)
	files["atom_site distinct hetero instances"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.auth_seq_id\n_atom_site.label_atom_id\n" +
		repeatToSizeCounted("HETATM LIG %d a%d\n", hostileFileBytes)
	files["atom_site one hetero instance with distinct atom names"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.auth_seq_id\n_atom_site.label_atom_id\n" +
		repeatToSizeCounted("HETATM LIG 1 a%d\n", hostileFileBytes)
	files["atom_site ragged"] = "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_seq_id\n_atom_site.label_alt_id\n" + repeatToSize("ATOM 1 A 2 ", hostileFileBytes)
	return files
}

// repeatToSizeCounted repeats a template that takes counters, each use with
// fresh numbers, until the text is about size bytes.
func repeatToSizeCounted(template string, size int) string {
	var builder strings.Builder
	builder.Grow(size + 64)
	verbs := strings.Count(template, "%d")
	for counter := 0; builder.Len() < size; counter++ {
		arguments := make([]any, verbs)
		for index := range arguments {
			arguments[index] = counter + index
		}
		fmt.Fprintf(&builder, template, arguments...)
	}
	return builder.String()
}

func Test_should_stay_under_memory_budget_when_parsing_hostile_files(t *testing.T) {
	for name, text := range hostileFiles() {
		t.Run("ExtractText "+name, func(t *testing.T) {
			// given
			file := text

			// when
			use := measureMemory(func() any {
				document, result, err := ExtractText(file, false)
				require.NoError(t, err)
				return []any{document, result}
			})

			// then
			t.Logf("MEASURE %s live=%dKiB alloc=%dKiB", strings.ReplaceAll(t.Name(), " ", "_"), use.liveHeap>>10, use.totalAlloc>>10)
			assert.Less(t, use.liveHeap, uint64(hostileLiveHeapBudget), "live heap after one extraction")
			assert.Less(t, use.totalAlloc, uint64(hostileAllocBudget), "total allocation of one extraction")
		})
		t.Run("Parse and Extract "+name, func(t *testing.T) {
			// given
			file := text

			// when
			use := measureMemory(func() any {
				document := Parse(file)
				result, err := Extract(document)
				require.NoError(t, err)
				return []any{document, result}
			})

			// then
			t.Logf("MEASURE %s live=%dKiB alloc=%dKiB", strings.ReplaceAll(t.Name(), " ", "_"), use.liveHeap>>10, use.totalAlloc>>10)
			assert.Less(t, use.liveHeap, uint64(hostileLiveHeapBudget), "live heap after Parse and Extract")
			assert.Less(t, use.totalAlloc, uint64(hostileAllocBudget), "total allocation of Parse and Extract")
		})
	}
}

func Test_should_stay_under_memory_budget_when_reading_hostile_pdb_files(t *testing.T) {
	atom := func(serial int, name string, compound string, chain string, sequence int, altID string) string {
		return fmt.Sprintf("ATOM  %5d %-4s%1s%3s %1s%4d    %8.3f%8.3f%8.3f  1.00  0.00           C\n",
			serial, name, altID, compound, chain, sequence, 1.0, 2.0, 3.0)
	}
	files := map[string]string{}
	var distinctResidues, distinctAltlocs, distinctHetero, hetRecords, distinctChains strings.Builder
	for counter := 0; distinctResidues.Len() < hostileFileBytes; counter++ {
		distinctResidues.WriteString(atom(counter%99999, "CA", "ALA", "A", counter%10000, ""))
		distinctAltlocs.WriteString(atom(counter%99999, "CA", "ALA", "A", counter, "A"))
		distinctHetero.WriteString(strings.Replace(atom(counter%99999, fmt.Sprintf("a%d", counter%999), "LIG", "A", counter, ""), "ATOM  ", "HETATM", 1))
		fmt.Fprintf(&hetRecords, "HET    L%02d  %c%4d  %5d\n", counter%100, 'A'+rune(counter%26), counter, counter)
		distinctChains.WriteString(atom(counter%99999, "CA", "ALA", string(rune(0x100+counter%60000)), counter, ""))
	}
	files["distinct residues"] = distinctResidues.String()
	files["distinct altlocs"] = distinctAltlocs.String()
	files["distinct hetero instances"] = distinctHetero.String()
	files["HET records"] = hetRecords.String()
	files["distinct chains"] = distinctChains.String()
	files["remark lines"] = repeatToSizeCounted("REMARK   3   PROGRAM %d : X\n", hostileFileBytes)
	files["one enormous line"] = "REMARK   3  PROGRAM : " + strings.Repeat("v", hostileFileBytes) + "\n"
	for name, text := range files {
		t.Run(name, func(t *testing.T) {
			// given
			file := text

			// when
			use := measureMemory(func() any {
				extraction, err := PDBExtraction(file, false)
				require.NoError(t, err)
				return extraction
			})

			// then
			t.Logf("MEASURE %s live=%dKiB alloc=%dKiB", strings.ReplaceAll(t.Name(), " ", "_"), use.liveHeap>>10, use.totalAlloc>>10)
			assert.Less(t, use.liveHeap, uint64(hostileLiveHeapBudget), "live heap after reading the PDB file")
			assert.Less(t, use.totalAlloc, uint64(hostileAllocBudget), "total allocation of reading the PDB file")
		})
	}
}

func Test_should_stay_under_memory_budget_when_scanning_a_scalar_in_a_hostile_stream(t *testing.T) {
	const size = 64 << 20
	// given
	source := io.MultiReader(strings.NewReader("data_X\n_entity.id "), &repeatReader{unit: "v", remaining: size})

	// when
	use := measureMemory(func() any {
		_, _, err := ScalarValueFromReader(source, "_refine.ls_R_factor_R_free")
		return err
	})

	// then
	assert.Less(t, use.totalAlloc, uint64(hostileAllocBudget), "total allocation of scanning a %d byte line", size)
}

type repeatReader struct {
	unit      string
	remaining int
}

func (r *repeatReader) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	count := min(len(buffer), r.remaining)
	for index := range count {
		buffer[index] = r.unit[0]
	}
	r.remaining -= count
	return count, nil
}

func Benchmark_hostile_struct_conn_loop(b *testing.B) {
	text := "data_X\nloop_\n_struct_conn.id\n" + repeatToSize("a ", hostileFileBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := ExtractText(text, false); err != nil {
			b.Fatal(err)
		}
	}
}
