package cif

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Header holds what a structure file's header says about the deposit as a whole: the PDB ID
// it names, who made it, how it was measured and how well the model fits the data. The
// browser preview reads the same items in the same order (parseStructureFacts in
// website/src/lib/parse/structure.ts), so the preview and the server agree on a file.
type Header struct {
	// EntryID is the PDB ID in _entry.id, or else in the data_ block name, in capitals; empty
	// when the file names none (a refinement program often writes its own block name).
	EntryID string   `json:"entry_id,omitempty"`
	Authors []string `json:"authors"`
	RWork   *float64 `json:"r_work,omitempty"`
	RFree   *float64 `json:"r_free,omitempty"`
	// Resolution is _refine.ls_d_res_high, or else _reflns.d_resolution_high, in ångströms.
	Resolution *float64 `json:"resolution,omitempty"`
	// Method is _exptl.method as the file writes it, such as X-RAY DIFFRACTION.
	Method string `json:"method,omitempty"`
	// SpaceGroup is _symmetry.space_group_name_H-M, or else _space_group.name_H-M_alt.
	SpaceGroup string `json:"space_group,omitempty"`
}

// pdbIDPattern is a PDB ID as the preview accepts one from a file: a digit other than 0, then
// three letters or digits.
var pdbIDPattern = regexp.MustCompile(`^[1-9][A-Z0-9]{3}$`)

// readHeader reads the header items. Each is a non-looped item, as in the preview: a
// category written as a loop (two refinements, two methods) gives no value, because the file
// does not say which row is the deposit's.
func readHeader(document *Document) Header {
	header := Header{
		Authors:    headerAuthors(document),
		RWork:      headerNumber(document, "_refine.ls_R_factor_R_work"),
		RFree:      headerNumber(document, "_refine.ls_R_factor_R_free"),
		Resolution: headerNumber(document, "_refine.ls_d_res_high"),
		Method:     headerText(document, "_exptl.method"),
		SpaceGroup: headerText(document, "_symmetry.space_group_name_H-M"),
	}
	if header.Resolution == nil {
		header.Resolution = headerNumber(document, "_reflns.d_resolution_high")
	}
	if header.SpaceGroup == "" {
		header.SpaceGroup = headerText(document, "_space_group.name_H-M_alt")
	}
	if id := pdbID(headerText(document, "_entry.id")); id != "" {
		header.EntryID = id
	} else {
		header.EntryID = pdbID(document.blockName)
	}
	return header
}

func headerAuthors(document *Document) []string {
	authors := []string{}
	if table := document.Table("_audit_author"); table != nil {
		for _, row := range table.Rows {
			if name := table.Value(row, "name"); name != "" {
				authors = append(authors, name)
			}
		}
		return authors
	}
	if name := headerText(document, "_audit_author.name"); name != "" {
		authors = append(authors, name)
	}
	return authors
}

func headerText(document *Document, tag string) string {
	value, _ := document.Scalar(tag)
	return value
}

// headerNumber reads a finite number; anything else, such as "?" or text, gives none.
func headerNumber(document *Document, tag string) *float64 {
	raw, ok := document.Scalar(tag)
	if !ok {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func pdbID(raw string) string {
	id := strings.ToUpper(strings.TrimSpace(raw))
	if pdbIDPattern.MatchString(id) {
		return id
	}
	return ""
}
