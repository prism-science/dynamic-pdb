package main

import (
	"fmt"

	"dynamic-pdb/lib/cif"
)

// categoryOfType maps a CCD type to its category with the parsers' own switch
// (cif.CCDTypeCategory). A type it does not know stops the refresh: a person
// decides where a new type goes.
func categoryOfType(ccdType string) (cif.CCDCategory, error) {
	category, err := cif.CCDTypeCategory(ccdType)
	if err != nil {
		return "", fmt.Errorf("sort the CCD type (a new type goes in cif.CCDTypeCategory and cif.KnownCCDTypes): %w", err)
	}
	return category, nil
}
