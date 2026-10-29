package main

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryFiles is a reference directory held in memory.
type memoryFiles map[string][]byte

func (files memoryFiles) ReadFile(name string) ([]byte, error) {
	data, ok := files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

func (files memoryFiles) WriteFile(name string, data []byte, _ fs.FileMode) error {
	files[name] = data
	return nil
}

// fakeDictionary answers like the RCSB APIs from fixed records.
type fakeDictionary struct {
	records     map[string]chemComp
	singleHeavy []string
	// extraTyped is added to the count of IDs with a type, as if the CCD had a new type.
	extraTyped int
}

func (d fakeDictionary) countWithType(context.Context) (int, error) {
	count := d.extraTyped
	for _, record := range d.records {
		if record.Type != "" {
			count++
		}
	}
	return count, nil
}

func (d fakeDictionary) countOfType(ctx context.Context, ccdType string) (int, error) {
	ids, err := d.idsOfType(ctx, ccdType)
	return len(ids), err
}

func (d fakeDictionary) idsOfType(_ context.Context, ccdType string) ([]string, error) {
	ids := []string{}
	for id, record := range d.records {
		if record.Type == ccdType {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (d fakeDictionary) singleHeavyAtomNonPolymers(context.Context) ([]string, error) {
	return d.singleHeavy, nil
}

func (d fakeDictionary) chemComps(_ context.Context, ids []string) (map[string]chemComp, error) {
	found := map[string]chemComp{}
	for _, id := range ids {
		if record, ok := d.records[id]; ok {
			found[id] = record
		}
	}
	return found, nil
}

func nonPolymer(id string, formula string, atoms int) chemComp {
	return chemComp{ID: id, Type: "non-polymer", Formula: formula, ReleaseStatus: "REL", AtomCount: atoms}
}

// testDictionary holds every ID the test files list, plus two metals the rule
// adds (RE, and HO3 next to the reviewed HO), a halide and UNX.
func testDictionary() fakeDictionary {
	records := map[string]chemComp{
		"HEM": nonPolymer("HEM", "C34 H32 Fe N4 O4", 75),
		"PLP": nonPolymer("PLP", "C8 H10 N O6 P", 24),
		"ZN":  nonPolymer("ZN", "Zn", 1),
		"HO":  nonPolymer("HO", "Ho", 1),
		"HO3": nonPolymer("HO3", "Ho", 1),
		"RE":  nonPolymer("RE", "Re", 1),
		"CL":  nonPolymer("CL", "Cl", 1),
		"UNX": nonPolymer("UNX", "X", 1),
		"NH4": nonPolymer("NH4", "H4 N", 5),
		"HOH": nonPolymer("HOH", "H2 O", 3),
		"GOL": nonPolymer("GOL", "C3 H8 O3", 14),
		"IMD": nonPolymer("IMD", "C3 H5 N2", 9),
		"SO4": nonPolymer("SO4", "O4 S", 5),
		"LLP": {ID: "LLP", Type: "L-peptide linking", ReleaseStatus: "REL", AtomCount: 47},
		"MSE": {ID: "MSE", Type: "L-peptide linking", ReleaseStatus: "REL", AtomCount: 20},
		"NAG": {ID: "NAG", Type: "D-saccharide, beta linking", ReleaseStatus: "REL", AtomCount: 30},
		"DA":  {ID: "DA", Type: "DNA linking", ReleaseStatus: "REL", AtomCount: 33},
	}
	return fakeDictionary{
		records:     records,
		singleHeavy: []string{"ZN", "HO", "HO3", "RE", "CL", "UNX", "NH4", "HOH"},
	}
}

func header(date string) string {
	return fmt.Sprintf(`"version": "2026.10.0",
  "approved": true,
  "ccd_checked": %q,
  "description": "test"`, date)
}

func testFiles(metals string) memoryFiles {
	return memoryFiles{
		cofactorsFile: []byte(`{
  ` + header("2026-10-01") + `,
  "cofactors": [{"id": "HEM", "name": "heme"}, {"id": "PLP", "name": "PLP"}],
  "metal_elements": ["Zn", "Ho", "Re"],
  "nonmetal_elements": ["H", "N", "O", "Cl"]
}
`),
		metalIonsFile: []byte(`{
  ` + header("2026-10-01") + `,
  "metal_ions": [` + metals + `]
}
`),
		solventsFile: []byte(`{
  ` + header("2026-10-01") + `,
  "solvents": ["HOH", "WAT", "GOL", "IMD"],
  "ambiguous": ["SO4"],
  "keep_when_metal_linked": ["IMD"]
}
`),
		covalentFile: []byte(`{
  ` + header("2026-10-01") + `,
  "covalent_cofactors": [{"residue": "LLP", "cofactor": "PLP"}]
}
`),
		ccdTypesFile: []byte(`{
  ` + header("2026-10-01") + `,
  "peptide": [],
  "nucleotide": [],
  "saccharide": []
}
`),
	}
}

func Test_should_write_generated_files_and_dates_when_lists_match_the_dictionary(t *testing.T) {
	// given
	files := testFiles(`"HO", "ZN"`)

	// when
	result, err := refresh(context.Background(), testDictionary(), files, "2026-10-29", false)

	// then
	require.NoError(t, err)
	assert.Empty(t, result.Problems)
	assert.Equal(t, []string{metalIonsFile, ccdTypesFile}, result.Drift)
	assert.Contains(t, result.Notes, "metal_ions.json: the metal rule adds HO3")
	assert.Contains(t, result.Notes, "metal_ions.json: the metal rule adds RE")
	assert.Contains(t, string(files[metalIonsFile]), `"metal_ions": [
    "HO", "HO3", "RE", "ZN"
  ]`)
	assert.Contains(t, string(files[ccdTypesFile]), `"peptide": [
    "LLP", "MSE"
  ],
  "nucleotide": [
    "DA"
  ],
  "saccharide": [
    "NAG"
  ]`)
	for _, name := range []string{cofactorsFile, metalIonsFile, solventsFile, covalentFile, ccdTypesFile} {
		assert.Contains(t, string(files[name]), `"ccd_checked": "2026-10-29"`, name)
	}
	assert.ElementsMatch(t, []string{cofactorsFile, metalIonsFile, solventsFile, covalentFile, ccdTypesFile}, result.Written)
}

func Test_should_be_clean_when_checking_files_it_wrote_on_another_day(t *testing.T) {
	// given
	files := testFiles(`"HO", "ZN"`)
	_, err := refresh(context.Background(), testDictionary(), files, "2026-10-29", false)
	require.NoError(t, err)
	written := map[string]string{}
	for name, data := range files {
		written[name] = string(data)
	}

	// when
	result, err := refresh(context.Background(), testDictionary(), files, "2026-11-15", true)

	// then
	require.NoError(t, err)
	assert.True(t, result.clean(), "problems %v, drift %v", result.Problems, result.Drift)
	assert.Empty(t, result.Written)
	for name, data := range files {
		assert.Equal(t, written[name], string(data), "%s must not change on --check", name)
	}
}

func Test_should_report_drift_without_writing_when_checking(t *testing.T) {
	// given
	files := testFiles(`"HO", "ZN"`)
	before := string(files[metalIonsFile])

	// when
	result, err := refresh(context.Background(), testDictionary(), files, "2026-10-29", true)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{metalIonsFile, ccdTypesFile}, result.Drift)
	assert.Empty(t, result.Written)
	assert.Equal(t, before, string(files[metalIonsFile]))
}

func Test_should_stop_without_writing_when_the_metal_rule_would_drop_a_reviewed_metal(t *testing.T) {
	// given: OS is reviewed but the dictionary has no such single atom.
	files := testFiles(`"HO", "OS", "ZN"`)
	before := string(files[metalIonsFile])

	// when
	result, err := refresh(context.Background(), testDictionary(), files, "2026-10-29", false)

	// then
	require.NoError(t, err)
	assert.Contains(t, result.Problems, "metal_ions.json metal_ions: OS is not in the CCD")
	assert.Contains(t, result.Problems, "metal_ions.json: the metal rule would drop reviewed metal OS (OS is not a single-heavy-atom non-polymer in the CCD)")
	assert.Empty(t, result.Written)
	assert.Equal(t, before, string(files[metalIonsFile]))
}

func Test_should_report_a_replaced_id_and_never_rewrite_it(t *testing.T) {
	// given
	dictionary := testDictionary()
	replaced := dictionary.records["HEM"]
	replaced.ReleaseStatus, replaced.ReplacedBy = "OBS", "HEX"
	dictionary.records["HEM"] = replaced
	files := testFiles(`"HO", "ZN"`)
	before := string(files[cofactorsFile])

	// when
	result, err := refresh(context.Background(), dictionary, files, "2026-10-29", false)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"cofactors.json cofactors: HEM is replaced by HEX in the CCD: researchers must approve the replacement"}, result.Problems)
	assert.Empty(t, result.Written)
	assert.Equal(t, before, string(files[cofactorsFile]))
}

func Test_should_report_a_listed_id_that_is_not_a_non_polymer(t *testing.T) {
	// given
	files := testFiles(`"HO", "ZN"`)
	files[solventsFile] = []byte(strings.Replace(string(files[solventsFile]), `"GOL"`, `"GOL", "MSE"`, 1))

	// when
	result, err := refresh(context.Background(), testDictionary(), files, "2026-10-29", false)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{`solvents.json solvents: MSE has CCD type "L-peptide linking", not non-polymer`}, result.Problems)
}

func Test_should_stop_when_the_ccd_has_a_type_nobody_knows(t *testing.T) {
	// given
	dictionary := testDictionary()
	dictionary.extraTyped = 3

	// when
	result, err := refresh(context.Background(), dictionary, testFiles(`"HO", "ZN"`), "2026-10-29", false)

	// then
	require.NoError(t, err)
	require.Len(t, result.Problems, 1)
	assert.Contains(t, result.Problems[0], "the CCD has a new type")
	assert.Empty(t, result.Written)
}

func Test_should_reject_a_covalent_residue_of_unknown_type(t *testing.T) {
	// given
	dictionary := testDictionary()
	residue := dictionary.records["LLP"]
	residue.Type = "L-peptide linking, new kind"
	dictionary.records["LLP"] = residue

	// when
	result, err := refresh(context.Background(), dictionary, testFiles(`"HO", "ZN"`), "2026-10-29", false)

	// then
	require.NoError(t, err)
	assert.Contains(t, result.Problems, `covalent_cofactors.json: LLP: unknown CCD type "L-peptide linking, new kind": add it to knownCCDTypes and categoryOfType`)
}

func Test_should_map_every_known_ccd_type_and_reject_others(t *testing.T) {
	for _, ccdType := range knownCCDTypes {
		_, err := categoryOfType(ccdType)
		assert.NoError(t, err, ccdType)
	}
	_, err := categoryOfType("glycolipid")
	require.Error(t, err)
}
