package cif

import (
	"context"
	"fmt"
	"strings"
)

// FieldCompoundReview is the field that lists the compounds the parser flagged
// for review, with the list each one was placed in and why.
const FieldCompoundReview = "compound_review"

// Extraction is one structure file flattened for the upload manifest.
type Extraction struct {
	// Fields holds every scalar item under its tag and a normalized key, the
	// refinement program, the derived counts, ligands, cofactors and, when any
	// compound needs review, FieldCompoundReview.
	Fields map[string]any
	// Warnings explain what was left out or relies on draft data.
	Warnings []string
	// Review lists the flagged compounds, the same data as FieldCompoundReview.
	Review []ReviewItem
}

// ReviewItem is one compound flagged for review.
type ReviewItem struct {
	List   string `json:"list"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Names of the compound lists a ReviewItem refers to.
const (
	ListLigands   = "ligands"
	ListCofactors = "cofactors"
)

// MMCIFFields flattens an mmCIF file into the field map the upload manifest
// reads. See MMCIFExtraction.
func MMCIFFields(text string) (map[string]any, error) {
	extraction, err := MMCIFExtraction(text, false)
	if err != nil {
		return nil, err
	}
	return extraction.Fields, nil
}

// MMCIFFieldsTruncated is MMCIFFields for the head of a larger file: the cut-off
// tail is not trusted, so counts that need the whole atom_site loop are omitted.
func MMCIFFieldsTruncated(text string) (map[string]any, error) {
	extraction, err := MMCIFExtraction(text, true)
	if err != nil {
		return nil, err
	}
	return extraction.Fields, nil
}

// MMCIFExtraction reads an mmCIF text in one pass. Set truncated when text is
// only the head of a larger file.
// The read stops after TimeBudget, see ExtractText.
func MMCIFExtraction(text string, truncated bool) (Extraction, error) {
	ctx, cancel := withTimeBudget(context.Background())
	defer cancel()
	return MMCIFExtractionContext(ctx, text, truncated)
}

// MMCIFExtractionContext is MMCIFExtraction bounded by ctx only.
func MMCIFExtractionContext(ctx context.Context, text string, truncated bool) (Extraction, error) {
	reference, err := DefaultReference()
	if err != nil {
		return Extraction{}, fmt.Errorf("load reference list: %w", err)
	}
	return mmcifExtraction(ctx, text, truncated, reference)
}

func mmcifExtraction(ctx context.Context, text string, truncated bool, reference *Reference) (Extraction, error) {
	document, result, err := extractText(ctx, text, truncated, reference)
	if err != nil {
		return Extraction{}, fmt.Errorf("extract mmCIF fields: %w", err)
	}
	fields := map[string]any{}
	for tag, value := range document.Scalars() {
		addField(fields, tag, value)
	}
	addField(fields, "program.name", result.Program.Name)
	addField(fields, "program.version", result.Program.Version)
	if result.Atoms != nil {
		addPositiveInt(fields, "atom_count", result.Atoms.AtomCount)
		addPositiveInt(fields, "modeled_residues", result.Atoms.ModeledResidues)
		addPositiveInt(fields, "unique_protein_chains", result.Atoms.ProteinChains)
		if result.Atoms.ModeledResidues > 0 {
			addField(fields, "altloc_fraction", result.Atoms.AltlocFraction)
		}
	}
	review := addCompoundFields(fields, result.Ligands, result.Cofactors, result.Atoms != nil)
	return Extraction{Fields: fields, Warnings: result.Warnings, Review: review}, nil
}

// FieldValue looks a field up by its exact key, then by its normalized key.
func FieldValue(fields map[string]any, field string) (any, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, false
	}
	if value, ok := fields[field]; ok {
		return value, true
	}
	value, ok := fields[normalizedFieldKey(field)]
	return value, ok
}

// addCompoundFields leaves a field out when it is unknown. Cofactors are an
// empty list, not omitted, when the coordinates were read in full and held none.
// It returns the compounds flagged for review.
func addCompoundFields(fields map[string]any, ligands []Compound, cofactors []Compound, coordinatesRead bool) []ReviewItem {
	if len(ligands) > 0 {
		addField(fields, ListLigands, compoundIDList(ligands))
	}
	if len(cofactors) > 0 || coordinatesRead {
		addField(fields, ListCofactors, compoundIDList(cofactors))
	}
	review := append(reviewItems(ListLigands, ligands), reviewItems(ListCofactors, cofactors)...)
	if len(review) > 0 {
		fields[FieldCompoundReview] = review
	}
	return review
}

func reviewItems(list string, compounds []Compound) []ReviewItem {
	var items []ReviewItem
	for _, compound := range compounds {
		if compound.NeedsReview {
			items = append(items, ReviewItem{List: list, ID: compound.ID, Reason: compound.ReviewReason})
		}
	}
	return items
}

func compoundIDList(compounds []Compound) []string {
	ids := make([]string, 0, len(compounds))
	for _, compound := range compounds {
		ids = append(ids, compound.ID)
	}
	return ids
}

func addField(fields map[string]any, key string, value any) {
	key = strings.TrimSpace(key)
	if key == "" || value == nil {
		return
	}
	if text, ok := value.(string); ok && text == "" {
		return
	}
	fields[key] = value
	if normalized := normalizedFieldKey(key); normalized != "" {
		fields[normalized] = value
	}
}

func addPositiveInt(fields map[string]any, key string, value int) {
	if value > 0 {
		addField(fields, key, value)
	}
}

var fieldKeySeparators = strings.NewReplacer("(", " ", ")", " ", ":", " ")

func normalizedFieldKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = fieldKeySeparators.Replace(key)
	return strings.Join(strings.Fields(key), " ")
}
