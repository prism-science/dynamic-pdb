package cif

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every parse of a four mebibyte file must finish inside hostileTimeBudget, and
// four times the input must not cost much more than eight times the time.
const (
	hostileTimeBudget = 2 * time.Second
	// hostileTimeFloor keeps the scaling check from judging timings that are
	// mostly noise.
	hostileTimeFloor    = 50 * time.Millisecond
	hostileScalingRatio = 8
	hostileSmallBytes   = 1 << 20
)

const loopPrefix = "data_X\nloop_\n_entity.id\n"

func repeated(prefix string, unit string) func(size int) string {
	return func(size int) string { return prefix + repeatToSize(unit, size) }
}

func hostileTimeCases() map[string]func(size int) string {
	return map[string]func(size int) string{
		"one line of single-quoted tokens":          repeated(loopPrefix, "'a' "),
		"one line of double-quoted tokens":          repeated(loopPrefix, "\"a\" "),
		"one line of mixed quoted tokens":           repeated(loopPrefix, "'a' \"b\" "),
		"quoted tokens followed by a tab":           repeated(loopPrefix, "'a'\t"),
		"quoted tokens on many lines":               repeated(loopPrefix, "'a' 'b' 'c'\n"),
		"quotes closed only at the end of the line": func(size int) string { return loopPrefix + "'" + repeatToSize("a'b", size) + "'\n" },
		"single quote never closed on a long line":  func(size int) string { return loopPrefix + "'" + repeatToSize("a'b ", size) + "\n" },
		"single quote never closed on many lines":   repeated(loopPrefix, "'abc def\n"),
		"double quote never closed on many lines":   repeated(loopPrefix, "\"abc def\n"),
		"quote run with no text":                    repeated(loopPrefix, "''''"),
		"quote then space repeated":                 repeated(loopPrefix, "' "),
		"quotes inside unquoted tokens":             repeated(loopPrefix, "a'b a\"b "),
		"prime-like names in an atom_site loop":     repeated("data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_atom_id\n_atom_site.label_comp_id\n", "HETATM O5' G\nHETATM \"O5'\" G\n"),
		"prime-like names on one line":              repeated(loopPrefix, "O5' C5' O3' "),
		"quoted prime-like compound names":          repeated(loopPrefix, "\"GUANOSINE-5'-DIPHOSPHATE\" "),
		"single-quoted names with embedded quotes":  repeated(loopPrefix, "'a 'b' c' "),
		"quoted tokens in CRLF lines":               repeated(loopPrefix, "'a' 'b'\r\n"),
		"carriage returns without line feeds":       repeated(loopPrefix, "a\r"),
		"text blocks, one per line":                 repeated(loopPrefix, ";x\n;\n"),
		"text block with many lines":                func(size int) string { return loopPrefix + ";\n" + repeatToSize("abc 'def\n", size) + ";\n" },
		"text block never closed":                   func(size int) string { return loopPrefix + ";\n" + repeatToSize("abc 'def\n", size) },
		"text block start repeated inside a block":  repeated(loopPrefix, ";\n;\n"),
		"semicolon lines that do not start a block": repeated(loopPrefix, " ;a\n"),
		"text block in a CRLF file":                 repeated(loopPrefix, ";x\r\n;\r\n"),
		"one very long line of tokens":              func(size int) string { return loopPrefix + repeatToSize("a ", size) + "\n" },
		"one very long unquoted token":              func(size int) string { return loopPrefix + strings.Repeat("a", size) + "\n" },
		"many tiny tokens in a loop":                repeated(loopPrefix, "a "),
		"many empty lines":                          repeated(loopPrefix, "\n"),
		"many comment lines":                        repeated(loopPrefix, "# c\n"),
		"many comments after tokens":                repeated(loopPrefix, "a # c\n"),
		"many data_ lines":                          repeated("", "data_X\n"),
		"many loop_ lines":                          repeated("data_X\n", "loop_\n"),
		"many loop_ with a tag":                     repeated("data_X\n", "loop_\n_a.b\n"),
		"many tag lines":                            repeated("data_X\n", "_a.b 1\n"),
		"many distinct tag lines":                   func(size int) string { return "data_X\n" + repeatToSizeCounted("_a.t%d 'x' \n", size) },
		"many distinct loops":                       func(size int) string { return "data_X\n" + repeatToSizeCounted("loop_\n_c%d.a\n'x'\n", size) },
		"many quoted values for scalar tags":        repeated("data_X\n", "_a.b 'x y'\n"),
		"quoted value for the wanted scalar":        repeated("data_X\n", "_refine.ls_R_factor_R_free 'x y'\n"),
		"mixed quotes, blocks, comments and tokens": repeated(loopPrefix, "'a' \"b\" c'd e\"f ;g\n;\n# h\nO5' '\n;x\n;\n_t.v 'w' "),
		"mixed lines with quotes and CRLF":          repeated("data_X\r\nloop_\r\n_entity.id\r\n", "'a b' c'd \"e\" ;\r\n;x\r\n;\r\n"),
		"metalc rows with quoted values":            repeated("data_X\nloop_\n_struct_conn.id\n_struct_conn.conn_type_id\n_struct_conn.ptnr1_label_comp_id\n", "'a' metalc \"ZN\" "),
		"atom_site rows with quoted values":         repeated("data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_atom_id\n_atom_site.label_comp_id\n", "ATOM 'O5\\'' \"G\" "),
		"depositor flags and many compound instances": func(size int) string {
			return "data_X\nloop_\n_pdbx_entity_instance_feature.comp_id\n_pdbx_entity_instance_feature.feature_type\n" +
				repeatToSizeCounted("Z%d OTHER\n", size/4) + "#\n" + hostileHetatmLoop(size*3/4)
		},
		"located depositor flags on one compound": func(size int) string {
			return "data_X\nloop_\n_pdbx_entity_instance_feature.comp_id\n_pdbx_entity_instance_feature.feature_type\n" +
				"_pdbx_entity_instance_feature.asym_id\n_pdbx_entity_instance_feature.auth_seq_num\n" +
				repeatToSizeCounted("ATP OTHER B %d\n", size/4) + "#\n" + hostileHetatmLoop(size*3/4)
		},
		"distinct modified residues": func(size int) string {
			return "data_X\nloop_\n_pdbx_struct_mod_residue.label_comp_id\n" + repeatToSizeCounted("M%d\n", size)
		},
		"distinct glycan members": func(size int) string {
			return "data_X\n_entity.id 1\n_entity.type branched\n#\nloop_\n_pdbx_entity_branch_list.entity_id\n_pdbx_entity_branch_list.comp_id\n" +
				repeatToSizeCounted("1 S%d\n", size)
		},
	}
}

// hostileHetatmLoop is about size bytes of ATP instances, each at its own residue number.
func hostileHetatmLoop(size int) string {
	return "loop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n" +
		repeatToSizeCounted("HETATM ATP A %d\n", size)
}

func hostilePDBTimeCases() map[string]func(size int) string {
	return map[string]func(size int) string{
		"PDB with one very long line":     func(size int) string { return "HETATM" + strings.Repeat("a", size) + "\n" },
		"PDB with tiny lines":             repeated("", "ATOM\n"),
		"PDB with prime-like atom names":  repeated("", "HETATM    1  O5'  G A   1       1.000   1.000   1.000  1.00 10.00           O\n"),
		"PDB with CRLF lines":             repeated("", "HETATM    1  O5'  G A   1\r\n"),
		"PDB with many REMARK lines":      repeated("", "REMARK   3   PROGRAM     : PHENIX\n"),
		"PDB with distinct REMARK labels": func(size int) string { return repeatToSizeCounted("REMARK   3   L%d : v\n", size) },
		"PDB with HET records":            repeated("", "HET    LIG  A   1      12\n"),
		"PDB with quotes and no newlines": func(size int) string { return repeatToSize("'a' ", size) },
		"PDB with one SEQRES line of distinct names": func(size int) string {
			return "SEQRES   1 A 9999  " + repeatToSizeCounted("N%d ", size) + "\n"
		},
		"PDB with distinct SEQRES names": hostileSeqresLines,
		"PDB with distinct MODRES residues": func(size int) string {
			var builder strings.Builder
			for counter := 0; builder.Len() < size; counter++ {
				fmt.Fprintf(&builder, "MODRES 1ABC %3s A %4d  ALA\n", base36Name(counter), counter%10000)
			}
			return builder.String()
		},
	}
}

// base36Name is a residue name of up to three characters, distinct for the
// first 46,656 counters.
func base36Name(counter int) string {
	return strings.ToUpper(strconv.FormatInt(int64(counter%46656), 36))
}

// hostileSeqresLines is about size bytes of SEQRES records whose 13 residue
// names are new to their chain.
func hostileSeqresLines(size int) string {
	var builder strings.Builder
	for counter := 0; builder.Len() < size; counter++ {
		names := make([]string, 13)
		for slot := range names {
			names[slot] = fmt.Sprintf("%3s", base36Name(counter*13+slot))
		}
		fmt.Fprintf(&builder, "SEQRES %3d %c 9999  %s\n", counter%1000, 'A'+rune(counter/3589%26), strings.Join(names, " "))
	}
	return builder.String()
}

const hostileScalarTag = "_refine.ls_R_factor_R_free"

type hostileEntryPoint struct {
	name string
	run  func(t *testing.T, text string)
}

func mmcifEntryPoints() []hostileEntryPoint {
	return []hostileEntryPoint{
		{"ExtractText", func(t *testing.T, text string) {
			_, _, err := ExtractText(text, false)
			require.NoError(t, err)
		}},
		{"ExtractText truncated", func(t *testing.T, text string) {
			_, _, err := ExtractText(text, true)
			require.NoError(t, err)
		}},
		{"Parse and Extract", func(t *testing.T, text string) {
			_, err := Extract(Parse(text))
			require.NoError(t, err)
		}},
		{"MMCIFExtraction truncated", func(t *testing.T, text string) {
			_, err := MMCIFExtraction(text, true)
			require.NoError(t, err)
		}},
		{"ScalarValue", func(_ *testing.T, text string) {
			ScalarValue(text, hostileScalarTag)
		}},
	}
}

func timed(work func()) time.Duration {
	start := time.Now()
	work()
	return time.Since(start)
}

func assertNearLinear(t *testing.T, build func(size int) string, run func(t *testing.T, text string)) {
	t.Helper()
	small := build(hostileSmallBytes)
	large := build(hostileFileBytes)

	smallTime := timed(func() { run(t, small) })
	largeTime := timed(func() { run(t, large) })

	t.Logf("1 MiB %v, 4 MiB %v", smallTime, largeTime)
	assert.Less(t, largeTime, hostileTimeBudget, "4 MiB took %v (1 MiB took %v)", largeTime, smallTime)
	assert.Less(t, largeTime, hostileScalingRatio*max(smallTime, hostileTimeFloor),
		"4x the input took %v against %v: not linear", largeTime, smallTime)
}

func Test_should_stay_linear_in_time_when_parsing_hostile_mmcif_files(t *testing.T) {
	for name, build := range hostileTimeCases() {
		for _, entry := range mmcifEntryPoints() {
			t.Run(entry.name+" "+name, func(t *testing.T) {
				// given
				// a hostile file builder and one entry point

				// when
				// then
				assertNearLinear(t, build, entry.run)
			})
		}
	}
}

func Test_should_stay_linear_in_time_when_parsing_hostile_pdb_files(t *testing.T) {
	for name, build := range hostilePDBTimeCases() {
		for _, truncated := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				// given
				// a hostile file builder

				// when
				// then
				assertNearLinear(t, build, func(t *testing.T, text string) {
					_, err := PDBExtraction(text, truncated)
					require.NoError(t, err)
				})
			})
		}
	}
}

func Test_should_read_each_quoted_token_when_many_share_a_line(t *testing.T) {
	// given
	text := loopPrefix + strings.Repeat("'a b' \"c\" ", 1000) + "\n"

	// when
	document := Parse(text)

	// then
	table := document.Table("_entity")
	require.NotNil(t, table)
	require.Len(t, table.Rows, 2000)
	assert.Equal(t, "a b", table.Rows[0][0])
	assert.Equal(t, "c", table.Rows[1][0])
}

func Test_should_close_quote_at_first_quote_before_whitespace_when_scanning_a_quoted_token(t *testing.T) {
	// given
	text := loopPrefix + "'a'b c' \"d\"e\" 'f\n'g' h\n"

	// when
	table := Parse(text).Table("_entity")

	// then
	require.NotNil(t, table)
	var values []string
	for _, row := range table.Rows {
		values = append(values, row[0])
	}
	assert.Equal(t, []string{"a'b c", "d\"e", "f", "g", "h"}, values)
}
