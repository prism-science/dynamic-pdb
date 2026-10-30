package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"dynamic-pdb/lib/cif"
)

// The refresh checks the reference lists of lib/cif against the chemical
// component dictionary (CCD) and regenerates the two files made by rule,
// metal_ions.json and ccd_types.json. A reviewed ID is never rewritten: a
// missing, obsolete or replaced ID, or a reviewed metal the rule would drop, is
// a problem, and a run with a problem writes nothing.

const (
	cofactorsFile = "cofactors.json"
	metalIonsFile = "metal_ions.json"
	solventsFile  = "solvents.json"
	covalentFile  = "covalent_cofactors.json"
	ccdTypesFile  = "ccd_types.json"
)

// handEditedFiles are reviewed by people; the refresh only updates their
// ccd_checked date.
var handEditedFiles = []string{cofactorsFile, solventsFile, covalentFile}

// waterAliases are water IDs on the solvent list that older programs write.
// They are not CCD IDs.
var waterAliases = map[string]bool{"WAT": true, "H2O": true}

var (
	ccdIDPattern = regexp.MustCompile(`^[A-Z0-9]{1,5}$`)
	// birdIDPattern matches BIRD molecule definitions (PRD_000001), which the
	// search API returns next to CCD IDs. A file names the CCD components of a
	// BIRD molecule, never the PRD ID, so they are left out of ccd_types.json.
	birdIDPattern     = regexp.MustCompile(`^PRD_[0-9]+$`)
	ccdCheckedPattern = regexp.MustCompile(`"ccd_checked": "[0-9]{4}-[0-9]{2}-[0-9]{2}"`)
)

// referenceFiles is the directory holding the reference JSON files.
type referenceFiles interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
}

type listHeader struct {
	Version     string `json:"version"`
	Approved    bool   `json:"approved"`
	CCDChecked  string `json:"ccd_checked"`
	Description string `json:"description"`
}

type cofactorsData struct {
	listHeader
	Cofactors []struct {
		ID string `json:"id"`
	} `json:"cofactors"`
	MetalElements    []string `json:"metal_elements"`
	NonmetalElements []string `json:"nonmetal_elements"`
}

type metalIonsData struct {
	listHeader
	MetalIons []string `json:"metal_ions"`
}

type solventsData struct {
	listHeader
	Solvents            []string `json:"solvents"`
	Ambiguous           []string `json:"ambiguous"`
	KeepWhenMetalLinked []string `json:"keep_when_metal_linked"`
}

type covalentData struct {
	listHeader
	CovalentCofactors []struct {
		Residue  string `json:"residue"`
		Cofactor string `json:"cofactor"`
	} `json:"covalent_cofactors"`
}

type ccdTypesData struct {
	listHeader
	Peptide    []string `json:"peptide"`
	Nucleotide []string `json:"nucleotide"`
	Saccharide []string `json:"saccharide"`
}

type referenceLists struct {
	raw       map[string][]byte
	cofactors cofactorsData
	metalIons metalIonsData
	solvents  solventsData
	covalent  covalentData
	ccdTypes  ccdTypesData
}

func loadLists(files referenceFiles) (referenceLists, error) {
	lists := referenceLists{raw: map[string][]byte{}}
	targets := []struct {
		name   string
		target any
		header *listHeader
	}{
		{cofactorsFile, &lists.cofactors, &lists.cofactors.listHeader},
		{metalIonsFile, &lists.metalIons, &lists.metalIons.listHeader},
		{solventsFile, &lists.solvents, &lists.solvents.listHeader},
		{covalentFile, &lists.covalent, &lists.covalent.listHeader},
		{ccdTypesFile, &lists.ccdTypes, &lists.ccdTypes.listHeader},
	}
	for _, file := range targets {
		data, err := files.ReadFile(file.name)
		if err != nil {
			return referenceLists{}, fmt.Errorf("read %s: %w", file.name, err)
		}
		if err := json.Unmarshal(data, file.target); err != nil {
			return referenceLists{}, fmt.Errorf("decode %s: %w", file.name, err)
		}
		if strings.TrimSpace(file.header.Version) == "" {
			return referenceLists{}, fmt.Errorf("%s has no version", file.name)
		}
		lists.raw[file.name] = data
	}
	return lists, nil
}

// listGroup is one list of reviewed IDs that must all be released non-polymers.
type listGroup struct {
	file, group string
	ids         []string
}

func (lists referenceLists) nonPolymerGroups() []listGroup {
	cofactorIDs := make([]string, 0, len(lists.cofactors.Cofactors))
	for _, cofactor := range lists.cofactors.Cofactors {
		cofactorIDs = append(cofactorIDs, cofactor.ID)
	}
	solventIDs := []string{}
	for _, id := range lists.solvents.Solvents {
		if !waterAliases[id] {
			solventIDs = append(solventIDs, id)
		}
	}
	return []listGroup{
		{cofactorsFile, "cofactors", cofactorIDs},
		{metalIonsFile, "metal_ions", lists.metalIons.MetalIons},
		{solventsFile, "solvents", solventIDs},
		{solventsFile, "ambiguous", lists.solvents.Ambiguous},
	}
}

func (lists referenceLists) dictionaryIDs() []string {
	ids := []string{}
	for _, group := range lists.nonPolymerGroups() {
		ids = append(ids, group.ids...)
	}
	for _, pair := range lists.covalent.CovalentCofactors {
		ids = append(ids, pair.Residue)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// releaseProblem says why a reviewed ID can no longer be used as it is, or ""
// when it is a released record nothing replaced.
func releaseProblem(id string, record chemComp, found bool) string {
	switch {
	case !found:
		return fmt.Sprintf("%s is not in the CCD", id)
	case record.ReplacedBy != "":
		return fmt.Sprintf("%s is replaced by %s in the CCD: researchers must approve the replacement", id, record.ReplacedBy)
	case record.ReleaseStatus != "REL":
		return fmt.Sprintf("%s has release status %q in the CCD, not REL", id, record.ReleaseStatus)
	default:
		return ""
	}
}

func (lists referenceLists) checkListed(records map[string]chemComp) []string {
	problems := []string{}
	for _, group := range lists.nonPolymerGroups() {
		for _, id := range group.ids {
			record, found := records[id]
			if problem := releaseProblem(id, record, found); problem != "" {
				problems = append(problems, fmt.Sprintf("%s %s: %s", group.file, group.group, problem))
				continue
			}
			if record.Type != "non-polymer" {
				problems = append(problems, fmt.Sprintf("%s %s: %s has CCD type %q, not non-polymer", group.file, group.group, id, record.Type))
			}
		}
	}
	cofactors := map[string]bool{}
	for _, cofactor := range lists.cofactors.Cofactors {
		cofactors[cofactor.ID] = true
	}
	for _, pair := range lists.covalent.CovalentCofactors {
		record, found := records[pair.Residue]
		if problem := releaseProblem(pair.Residue, record, found); problem != "" {
			problems = append(problems, fmt.Sprintf("%s: %s", covalentFile, problem))
		} else if category, err := categoryOfType(record.Type); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s: %v", covalentFile, pair.Residue, err))
		} else if category != cif.CCDPeptide {
			problems = append(problems, fmt.Sprintf("%s: %s has CCD type %q, not a peptide-linking residue", covalentFile, pair.Residue, record.Type))
		}
		if !cofactors[pair.Cofactor] {
			problems = append(problems, fmt.Sprintf("%s: %s maps to %s, which is not on the cofactor list", covalentFile, pair.Residue, pair.Cofactor))
		}
	}
	dropped := map[string]bool{}
	for _, id := range lists.solvents.Solvents {
		dropped[id] = true
	}
	for _, id := range lists.solvents.KeepWhenMetalLinked {
		if !dropped[id] {
			problems = append(problems, fmt.Sprintf("%s keep_when_metal_linked: %s is not on the solvents list", solventsFile, id))
		}
	}
	return problems
}

// generateMetals applies the metal rule (researchers' decision of 2026-10-28):
// every released single-atom non-polymer whose element is a metal. A reviewed
// metal the rule would drop is a problem.
func generateMetals(ctx context.Context, ccd dictionary, lists referenceLists) ([]string, []string, []string, error) {
	candidates, err := ccd.singleHeavyAtomNonPolymers(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find single-atom candidates: %w", err)
	}
	slices.Sort(candidates)
	records, err := ccd.chemComps(ctx, candidates)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read single-heavy-atom records: %w", err)
	}
	metalElements := upperSet(lists.cofactors.MetalElements)
	nonmetalElements := upperSet(lists.cofactors.NonmetalElements)
	metals := []string{}
	notes := []string{}
	excluded := map[string]string{}
	for _, id := range candidates {
		record, found := records[id]
		if problem := releaseProblem(id, record, found); problem != "" {
			excluded[id] = problem
			continue
		}
		if record.AtomCount != 1 {
			excluded[id] = fmt.Sprintf("%s has %d atoms", id, record.AtomCount)
			continue
		}
		element := strings.ToUpper(strings.TrimSpace(record.Formula))
		switch {
		case metalElements[element]:
			metals = append(metals, id)
		case nonmetalElements[element]:
			excluded[id] = fmt.Sprintf("%s is element %s, not a metal", id, record.Formula)
		default:
			excluded[id] = fmt.Sprintf("%s has element %q, which is in neither metal_elements nor nonmetal_elements", id, record.Formula)
			notes = append(notes, fmt.Sprintf("%s: single-atom %s has element %q, in neither element list: left off metal_ions", cofactorsFile, id, record.Formula))
		}
	}
	generated := upperSet(metals)
	problems := []string{}
	for _, id := range lists.metalIons.MetalIons {
		if generated[id] {
			continue
		}
		why := excluded[id]
		if why == "" {
			why = id + " is not a single-heavy-atom non-polymer in the CCD"
		}
		problems = append(problems, fmt.Sprintf("%s: the metal rule would drop reviewed metal %s (%s)", metalIonsFile, id, why))
	}
	reviewed := upperSet(lists.metalIons.MetalIons)
	for _, id := range metals {
		if !reviewed[id] {
			notes = append(notes, fmt.Sprintf("%s: the metal rule adds %s", metalIonsFile, id))
		}
	}
	return metals, notes, problems, nil
}

// generateCategories lists the IDs of every CCD type that is not sorted by the
// reference lists, by category. When the known types no longer cover every ID
// with a type, the CCD has a new type and the run stops.
func generateCategories(ctx context.Context, ccd dictionary) (map[cif.CCDCategory][]string, []string, error) {
	total, err := ccd.countWithType(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("count CCD IDs with a type: %w", err)
	}
	byCategory := map[cif.CCDCategory][]string{}
	firstType := map[string]string{}
	problems := []string{}
	covered := 0
	for _, ccdType := range cif.KnownCCDTypes {
		category, err := categoryOfType(ccdType)
		if err != nil {
			return nil, nil, fmt.Errorf("map CCD type %q: %w", ccdType, err)
		}
		if category == cif.CCDNonPolymer {
			count, err := ccd.countOfType(ctx, ccdType)
			if err != nil {
				return nil, nil, fmt.Errorf("count CCD IDs of type %q: %w", ccdType, err)
			}
			covered += count
			continue
		}
		ids, err := ccd.idsOfType(ctx, ccdType)
		if err != nil {
			return nil, nil, fmt.Errorf("list CCD IDs of type %q: %w", ccdType, err)
		}
		covered += len(ids)
		for _, id := range ids {
			switch {
			case birdIDPattern.MatchString(id):
				continue
			case !ccdIDPattern.MatchString(id):
				problems = append(problems, fmt.Sprintf("%s: CCD type %q lists %q, which is not a CCD ID", ccdTypesFile, ccdType, id))
			case firstType[id] != "":
				problems = append(problems, fmt.Sprintf("%s: %s has two CCD types, %q and %q", ccdTypesFile, id, firstType[id], ccdType))
			default:
				firstType[id] = ccdType
				byCategory[category] = append(byCategory[category], id)
			}
		}
	}
	if covered != total {
		problems = append(problems, fmt.Sprintf("%s: %d CCD IDs have a type but the known types cover %d: the CCD has a new type; add it to cif.CCDTypeCategory and cif.KnownCCDTypes", ccdTypesFile, total, covered))
	}
	for category := range byCategory {
		slices.Sort(byCategory[category])
	}
	return byCategory, problems, nil
}

func upperSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[strings.ToUpper(value)] = true
	}
	return set
}

// refreshResult is what one run found. A run with problems writes nothing.
type refreshResult struct {
	Problems []string
	Notes    []string
	// Drift names each generated file whose content the dictionary changes.
	Drift []string
	// Written names the files a run that is not a check rewrote.
	Written []string
}

func (r refreshResult) clean() bool {
	return len(r.Problems) == 0 && len(r.Drift) == 0
}

// refresh checks every list against the dictionary. With check set it writes
// nothing and reports drift; otherwise it rewrites the generated files and the
// ccd_checked date of every file, unless there is a problem.
func refresh(ctx context.Context, ccd dictionary, files referenceFiles, today string, check bool) (refreshResult, error) {
	lists, err := loadLists(files)
	if err != nil {
		return refreshResult{}, fmt.Errorf("load the reference lists: %w", err)
	}
	records, err := ccd.chemComps(ctx, lists.dictionaryIDs())
	if err != nil {
		return refreshResult{}, fmt.Errorf("read listed records: %w", err)
	}
	result := refreshResult{Problems: lists.checkListed(records)}

	metals, notes, problems, err := generateMetals(ctx, ccd, lists)
	if err != nil {
		return refreshResult{}, fmt.Errorf("apply the metal rule: %w", err)
	}
	result.Notes = append(result.Notes, notes...)
	result.Problems = append(result.Problems, problems...)

	categories, problems, err := generateCategories(ctx, ccd)
	if err != nil {
		return refreshResult{}, fmt.Errorf("build the CCD category table: %w", err)
	}
	result.Problems = append(result.Problems, problems...)

	generatedGroups := map[string][]namedIDs{
		metalIonsFile: {{"metal_ions", metals}},
		ccdTypesFile: {
			{string(cif.CCDPeptide), categories[cif.CCDPeptide]},
			{string(cif.CCDNucleotide), categories[cif.CCDNucleotide]},
			{string(cif.CCDSaccharide), categories[cif.CCDSaccharide]},
		},
	}
	generatedFiles := []string{metalIonsFile, ccdTypesFile}
	for _, name := range generatedFiles {
		// Compare with the file's own date, so a check on a later day is clean.
		header, err := lists.header(name)
		if err != nil {
			return refreshResult{}, fmt.Errorf("compare %s with the dictionary: %w", name, err)
		}
		if !bytes.Equal(lists.raw[name], formatGenerated(header, header.CCDChecked, generatedGroups[name])) {
			result.Drift = append(result.Drift, name)
		}
	}
	if check || len(result.Problems) > 0 {
		return result, nil
	}

	updates := map[string][]byte{}
	for _, name := range generatedFiles {
		header, err := lists.header(name)
		if err != nil {
			return refreshResult{}, fmt.Errorf("regenerate %s: %w", name, err)
		}
		updates[name] = formatGenerated(header, today, generatedGroups[name])
	}
	for _, name := range handEditedFiles {
		current := lists.raw[name]
		if matches := ccdCheckedPattern.FindAllIndex(current, -1); len(matches) != 1 {
			return refreshResult{}, fmt.Errorf("%s needs exactly one \"ccd_checked\": \"YYYY-MM-DD\" line, found %d", name, len(matches))
		}
		updates[name] = ccdCheckedPattern.ReplaceAll(current, []byte(`"ccd_checked": "`+today+`"`))
	}
	for _, name := range []string{cofactorsFile, metalIonsFile, solventsFile, covalentFile, ccdTypesFile} {
		if bytes.Equal(updates[name], lists.raw[name]) {
			continue
		}
		if err := files.WriteFile(name, updates[name], 0o644); err != nil {
			return refreshResult{}, fmt.Errorf("write %s: %w", name, err)
		}
		result.Written = append(result.Written, name)
	}
	for _, name := range result.Drift {
		result.Notes = append(result.Notes, fmt.Sprintf("%s changed: bump its version, and the reference version it feeds, before the PR", name))
	}
	return result, nil
}

func (lists referenceLists) header(name string) (listHeader, error) {
	switch name {
	case cofactorsFile:
		return lists.cofactors.listHeader, nil
	case metalIonsFile:
		return lists.metalIons.listHeader, nil
	case solventsFile:
		return lists.solvents.listHeader, nil
	case covalentFile:
		return lists.covalent.listHeader, nil
	case ccdTypesFile:
		return lists.ccdTypes.listHeader, nil
	default:
		return listHeader{}, fmt.Errorf("unknown reference file: %s", name)
	}
}

// namedIDs is one array of a generated file.
type namedIDs struct {
	name string
	ids  []string
}

const idsPerLine = 16

// formatGenerated writes a generated file in a fixed layout, so a refresh that
// finds nothing new leaves the file byte for byte the same.
func formatGenerated(header listHeader, date string, groups []namedIDs) []byte {
	var out bytes.Buffer
	out.WriteString("{\n")
	fmt.Fprintf(&out, "  \"version\": %s,\n", quote(header.Version))
	fmt.Fprintf(&out, "  \"approved\": %t,\n", header.Approved)
	fmt.Fprintf(&out, "  \"ccd_checked\": %s,\n", quote(date))
	fmt.Fprintf(&out, "  \"description\": %s", quote(header.Description))
	for _, group := range groups {
		fmt.Fprintf(&out, ",\n  %s: [", quote(group.name))
		for index, id := range group.ids {
			if index%idsPerLine == 0 {
				out.WriteString("\n    ")
			} else {
				out.WriteString(" ")
			}
			out.WriteString(quote(id))
			if index < len(group.ids)-1 {
				out.WriteString(",")
			}
		}
		if len(group.ids) > 0 {
			out.WriteString("\n  ")
		}
		out.WriteString("]")
	}
	out.WriteString("\n}\n")
	return out.Bytes()
}

func quote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode %q: %v", value, err))
	}
	return string(encoded)
}
