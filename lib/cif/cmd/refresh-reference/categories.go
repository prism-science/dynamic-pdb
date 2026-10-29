package main

import (
	"fmt"

	"dynamic-pdb/lib/cif"
)

// knownCCDTypes are the chem_comp.type values of the PDBx/mmCIF dictionary.
// On 2026-10-29 they covered all 51,512 CCD IDs with a type.
var knownCCDTypes = []string{
	"D-beta-peptide, C-gamma linking",
	"D-gamma-peptide, C-delta linking",
	"D-peptide COOH carboxy terminus",
	"D-peptide NH3 amino terminus",
	"D-peptide linking",
	"L-beta-peptide, C-gamma linking",
	"L-gamma-peptide, C-delta linking",
	"L-peptide COOH carboxy terminus",
	"L-peptide NH3 amino terminus",
	"L-peptide linking",
	"peptide linking",
	"peptide-like",
	"DNA OH 3 prime terminus",
	"DNA OH 5 prime terminus",
	"DNA linking",
	"L-DNA linking",
	"L-RNA linking",
	"RNA OH 3 prime terminus",
	"RNA OH 5 prime terminus",
	"RNA linking",
	"D-saccharide",
	"D-saccharide, alpha linking",
	"D-saccharide, beta linking",
	"L-saccharide",
	"L-saccharide, alpha linking",
	"L-saccharide, beta linking",
	"saccharide",
	"non-polymer",
	"other",
}

// categoryOfType maps a CCD type to its category with the parsers' own switch
// (cif.CCDTypeCategory). A type it does not know stops the refresh: a person
// decides where a new type goes.
func categoryOfType(ccdType string) (cif.CCDCategory, error) {
	category, err := cif.CCDTypeCategory(ccdType)
	if err != nil {
		return "", fmt.Errorf("%w: add it to cif.CCDTypeCategory and knownCCDTypes", err)
	}
	return category, nil
}
