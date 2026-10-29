package cif

import (
	"fmt"
	"strings"
)

// CCDCategory is how a chemical component is sorted before the reference
// lists, by its CCD type. The peptide, nucleotide and saccharide categories
// name the arrays of ccd_types.json.
type CCDCategory string

const (
	CCDPeptide    CCDCategory = "peptide"
	CCDNucleotide CCDCategory = "nucleotide"
	CCDSaccharide CCDCategory = "saccharide"
	// CCDNonPolymer is sorted by the reference lists. Its IDs are not written
	// to ccd_types.json.
	CCDNonPolymer CCDCategory = "non-polymer"
)

// CCDTypeCategory maps a chem_comp.type value of the PDBx/mmCIF dictionary to
// its category, ignoring case. An unknown type is an error: the refresh tool
// stops on it, and the parser warns and flags the compound for review.
func CCDTypeCategory(ccdType string) (CCDCategory, error) {
	switch strings.ToLower(strings.TrimSpace(ccdType)) {
	case "d-beta-peptide, c-gamma linking",
		"d-gamma-peptide, c-delta linking",
		"d-peptide cooh carboxy terminus",
		"d-peptide nh3 amino terminus",
		"d-peptide linking",
		"l-beta-peptide, c-gamma linking",
		"l-gamma-peptide, c-delta linking",
		"l-peptide cooh carboxy terminus",
		"l-peptide nh3 amino terminus",
		"l-peptide linking",
		"peptide linking",
		"peptide-like":
		return CCDPeptide, nil
	case "dna oh 3 prime terminus",
		"dna oh 5 prime terminus",
		"dna linking",
		"l-dna linking",
		"l-rna linking",
		"rna oh 3 prime terminus",
		"rna oh 5 prime terminus",
		"rna linking":
		return CCDNucleotide, nil
	case "d-saccharide",
		"d-saccharide, alpha linking",
		"d-saccharide, beta linking",
		"l-saccharide",
		"l-saccharide, alpha linking",
		"l-saccharide, beta linking",
		"saccharide":
		return CCDSaccharide, nil
	case "non-polymer", "other":
		return CCDNonPolymer, nil
	default:
		return "", fmt.Errorf("unknown CCD type %q", ccdType)
	}
}
