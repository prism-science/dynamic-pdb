package cif

import (
	"fmt"
	"strings"
)

// MMCIFFields flattens an mmCIF file into the field map the upload manifest
// reads: every scalar item under its tag and a normalized key, the refinement
// program, and the derived counts, ligands and cofactors.
func MMCIFFields(text string) (map[string]any, error) {
	return mmcifFields(Parse(text))
}

// MMCIFFieldsTruncated is MMCIFFields for the head of a larger file: the cut-off
// tail is not trusted, so counts that need the whole atom_site loop are omitted.
func MMCIFFieldsTruncated(text string) (map[string]any, error) {
	return mmcifFields(ParseTruncated(text))
}

func mmcifFields(document *Document) (map[string]any, error) {
	result, err := Extract(document)
	if err != nil {
		return nil, fmt.Errorf("extract mmCIF values: %w", err)
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
	addCompoundFields(fields, result.Ligands, result.Cofactors, result.Atoms != nil)
	return fields, nil
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

// ScalarValue returns one mmCIF item without extracting anything else.
func ScalarValue(text string, tag string) (string, bool) {
	return Parse(text).Scalar(tag)
}

// addCompoundFields leaves a field out when it is unknown. Cofactors are an
// empty list, not omitted, when the coordinates were read in full and held none.
func addCompoundFields(fields map[string]any, ligands []Compound, cofactors []Compound, coordinatesRead bool) {
	if len(ligands) > 0 {
		addField(fields, "ligands", compoundIDList(ligands))
	}
	if len(cofactors) > 0 || coordinatesRead {
		addField(fields, "cofactors", compoundIDList(cofactors))
	}
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

func normalizedFieldKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("(", " ", ")", " ", ":", " ").Replace(key)
	return strings.Join(strings.Fields(key), " ")
}
