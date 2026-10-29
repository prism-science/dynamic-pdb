package cif

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed cofactors.json
var cofactorsJSON []byte

//go:embed metal_ions.json
var metalIonsJSON []byte

//go:embed solvents.json
var solventsJSON []byte

//go:embed covalent_cofactors.json
var covalentCofactorsJSON []byte

//go:embed ccd_types.json
var ccdTypesJSON []byte

type compoundKind int

const (
	kindLigand compoundKind = iota
	kindCofactor
	kindMetalIon
	kindSolvent
	// kindDraftSolvent is a compound on the solvent list while the lists are
	// not approved: it is kept as a ligand and flagged, never dropped.
	kindDraftSolvent
	// kindAmbiguous is often from the buffer but often a real ligand (sulfate,
	// phosphate, citrate, halides): kept as a ligand and flagged.
	kindAmbiguous
	// kindSolventUnlessMetalLinked is dropped like a solvent unless a metalc
	// connection joins the instance to a metal ion (imidazole on a His-tag metal).
	kindSolventUnlessMetalLinked
)

// elementClass sorts an element symbol for the single-atom rule.
type elementClass int

const (
	elementUnknown elementClass = iota
	elementMetal
	elementNonmetal
)

// Reference sorts chemical component IDs. Cofactors and the element classes
// live in cofactors.json, metal ions in metal_ions.json, solvents and the
// ambiguous compounds in solvents.json, covalently bound cofactors in
// covalent_cofactors.json, and the CCD categories in ccd_types.json. Each file
// is versioned and carries its own approval flag.
type Reference struct {
	// Version combines every file version so a stored value names every list it used.
	Version                 string
	CofactorVersion         string
	MetalIonVersion         string
	SolventVersion          string
	CovalentCofactorVersion string
	CCDTypeVersion          string
	// Approved is true only when researchers approved every file.
	Approved            bool
	cofactors           map[string]string
	metals              map[string]struct{}
	solvents            map[string]struct{}
	ambiguous           map[string]struct{}
	keepWhenMetalLinked map[string]struct{}
	// covalentCofactors maps a modified residue to the cofactor bound in it.
	covalentCofactors map[string]string
	ccdCategories     map[string]CCDCategory
	metalElements     map[string]struct{}
	nonmetalElements  map[string]struct{}
}

// referenceFiles is the raw reference data: the embedded files, or test data.
type referenceFiles struct {
	cofactors         []byte
	metalIons         []byte
	solvents          []byte
	covalentCofactors []byte
	ccdTypes          []byte
}

func embeddedReferenceFiles() referenceFiles {
	return referenceFiles{
		cofactors:         cofactorsJSON,
		metalIons:         metalIonsJSON,
		solvents:          solventsJSON,
		covalentCofactors: covalentCofactorsJSON,
		ccdTypes:          ccdTypesJSON,
	}
}

type listHeader struct {
	Version  string `json:"version"`
	Approved bool   `json:"approved"`
}

type cofactorFile struct {
	listHeader
	Cofactors []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"cofactors"`
	MetalElements    []string `json:"metal_elements"`
	NonmetalElements []string `json:"nonmetal_elements"`
}

type metalIonFile struct {
	listHeader
	MetalIons []string `json:"metal_ions"`
}

type solventFile struct {
	listHeader
	Solvents            []string `json:"solvents"`
	Ambiguous           []string `json:"ambiguous"`
	KeepWhenMetalLinked []string `json:"keep_when_metal_linked"`
}

type covalentCofactorFile struct {
	listHeader
	CovalentCofactors []struct {
		Residue  string `json:"residue"`
		Cofactor string `json:"cofactor"`
	} `json:"covalent_cofactors"`
}

type ccdTypeFile struct {
	listHeader
	Peptide    []string `json:"peptide"`
	Nucleotide []string `json:"nucleotide"`
	Saccharide []string `json:"saccharide"`
}

var loadDefaultReference = sync.OnceValues(func() (*Reference, error) {
	return parseReference(embeddedReferenceFiles())
})

// DefaultReference returns the reference lists embedded in this package.
func DefaultReference() (*Reference, error) {
	reference, err := loadDefaultReference()
	if err != nil {
		return nil, fmt.Errorf("load embedded reference lists: %w", err)
	}
	return reference, nil
}

func decodeList(name string, data []byte, file any, header *listHeader) error {
	if err := json.Unmarshal(data, file); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if strings.TrimSpace(header.Version) == "" {
		return fmt.Errorf("%s has no version", name)
	}
	return nil
}

func parseReference(files referenceFiles) (*Reference, error) {
	var cofactorList cofactorFile
	if err := decodeList("cofactor list", files.cofactors, &cofactorList, &cofactorList.listHeader); err != nil {
		return nil, err
	}
	var metalList metalIonFile
	if err := decodeList("metal ion list", files.metalIons, &metalList, &metalList.listHeader); err != nil {
		return nil, err
	}
	var solventList solventFile
	if err := decodeList("solvent list", files.solvents, &solventList, &solventList.listHeader); err != nil {
		return nil, err
	}
	var covalentList covalentCofactorFile
	if err := decodeList("covalent cofactor map", files.covalentCofactors, &covalentList, &covalentList.listHeader); err != nil {
		return nil, err
	}
	var ccdTypes ccdTypeFile
	if err := decodeList("CCD type table", files.ccdTypes, &ccdTypes, &ccdTypes.listHeader); err != nil {
		return nil, err
	}

	reference := &Reference{
		Version: "cofactors:" + cofactorList.Version + "+metals:" + metalList.Version +
			"+solvents:" + solventList.Version + "+covalent:" + covalentList.Version +
			"+ccd-types:" + ccdTypes.Version,
		CofactorVersion:         cofactorList.Version,
		MetalIonVersion:         metalList.Version,
		SolventVersion:          solventList.Version,
		CovalentCofactorVersion: covalentList.Version,
		CCDTypeVersion:          ccdTypes.Version,
		Approved: cofactorList.Approved && metalList.Approved && solventList.Approved &&
			covalentList.Approved && ccdTypes.Approved,
		cofactors:           map[string]string{},
		metals:              idSet(metalList.MetalIons),
		solvents:            idSet(solventList.Solvents),
		ambiguous:           idSet(solventList.Ambiguous),
		keepWhenMetalLinked: idSet(solventList.KeepWhenMetalLinked),
		covalentCofactors:   map[string]string{},
		ccdCategories:       map[string]CCDCategory{},
		metalElements:       idSet(cofactorList.MetalElements),
		nonmetalElements:    idSet(cofactorList.NonmetalElements),
	}
	for _, cofactor := range cofactorList.Cofactors {
		reference.cofactors[strings.ToUpper(cofactor.ID)] = cofactor.Name
	}
	for id := range reference.keepWhenMetalLinked {
		if _, onSolventList := reference.solvents[id]; !onSolventList {
			return nil, fmt.Errorf("solvent list: %s is kept when metal-linked but is not on the solvent list", id)
		}
	}
	for _, pair := range covalentList.CovalentCofactors {
		residue, cofactor := strings.ToUpper(pair.Residue), strings.ToUpper(pair.Cofactor)
		if _, isCofactor := reference.cofactors[cofactor]; !isCofactor {
			return nil, fmt.Errorf("covalent cofactor map: %s maps to %s, which is not on the cofactor list", residue, cofactor)
		}
		reference.covalentCofactors[residue] = cofactor
	}
	for _, group := range []struct {
		category CCDCategory
		ids      []string
	}{{CCDPeptide, ccdTypes.Peptide}, {CCDNucleotide, ccdTypes.Nucleotide}, {CCDSaccharide, ccdTypes.Saccharide}} {
		for _, id := range group.ids {
			reference.ccdCategories[strings.ToUpper(id)] = group.category
		}
	}
	return reference, nil
}

func idSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[strings.ToUpper(strings.TrimSpace(id))] = struct{}{}
	}
	return set
}

// DraftWarning is reported with every extraction made while the reference lists
// are not approved.
const DraftWarning = "reference lists are draft and not yet approved by researchers: only water is dropped from ligands; " +
	"buffers, salts and cryoprotectants on the draft solvent list are kept as ligands and flagged for review"

// draftSolventReason explains the flag on a compound kept only because the
// solvent list is a draft.
const draftSolventReason = "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped"

// ambiguousReason explains the flag on a compound that is often from the
// buffer but often a real ligand.
const ambiguousReason = "often from the buffer: confirm it is part of the sample"

// kind sorts a compound. Water is always dropped. Other solvents are dropped
// only once the reference lists are approved; until then they are kept.
func (r *Reference) kind(compoundID string) compoundKind {
	id := strings.ToUpper(compoundID)
	if isWater(id) {
		return kindSolvent
	}
	if _, ok := r.solvents[id]; ok {
		_, keepWhenMetalLinked := r.keepWhenMetalLinked[id]
		switch {
		case !r.Approved:
			return kindDraftSolvent
		case keepWhenMetalLinked:
			return kindSolventUnlessMetalLinked
		default:
			return kindSolvent
		}
	}
	if _, ok := r.ambiguous[id]; ok {
		return kindAmbiguous
	}
	if _, ok := r.metals[id]; ok {
		return kindMetalIon
	}
	if _, ok := r.cofactors[id]; ok {
		return kindCofactor
	}
	return kindLigand
}

// elementClass says whether an element symbol is a metal for the single-atom
// rule. A symbol on neither list (empty, or X for an unknown atom) is unknown.
func (r *Reference) elementClass(symbol string) elementClass {
	element := strings.ToUpper(strings.TrimSpace(symbol))
	if _, ok := r.metalElements[element]; ok {
		return elementMetal
	}
	if _, ok := r.nonmetalElements[element]; ok {
		return elementNonmetal
	}
	return elementUnknown
}

// covalentCofactor returns the cofactor bound in a modified residue, such as
// PLP in LLP.
func (r *Reference) covalentCofactor(residue string) (string, bool) {
	cofactor, ok := r.covalentCofactors[strings.ToUpper(residue)]
	return cofactor, ok
}

// ccdCategoryOf returns the category of a CCD ID whose type is a peptide,
// nucleotide or saccharide type; false for every other ID.
func (r *Reference) ccdCategoryOf(compoundID string) (CCDCategory, bool) {
	category, ok := r.ccdCategories[strings.ToUpper(compoundID)]
	return category, ok
}
