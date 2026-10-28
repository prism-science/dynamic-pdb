package cif

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	detailsEngineeredMutation = "engineered mutation"
	sequenceDatabaseUniProt   = "UNP"
)

var oneLetterCodes = map[string]string{
	"ALA": "A", "ARG": "R", "ASN": "N", "ASP": "D", "CYS": "C",
	"GLN": "Q", "GLU": "E", "GLY": "G", "HIS": "H", "ILE": "I",
	"LEU": "L", "LYS": "K", "MET": "M", "PHE": "F", "PRO": "P",
	"SER": "S", "THR": "T", "TRP": "W", "TYR": "Y", "VAL": "V",
}

type mutationKey struct {
	entityID  string
	accession string
}

// readMutations pairs each entity's own pdbx_mutation text with the engineered
// mutations found in _struct_ref_seq_dif. Rows that cannot be mapped to an
// entity, a UniProt accession and numbered standard residues are listed as
// unmapped and flag the set for review instead of being guessed.
func readMutations(document *Document, differences []SequenceDifference) []MutationSet {
	originals := entityMutationText(document)
	alignEntities := alignEntityIDs(document)

	sets := map[mutationKey]*MutationSet{}
	orphanUnmapped := []string{}
	for _, difference := range differences {
		if !strings.EqualFold(difference.Details, detailsEngineeredMutation) {
			continue
		}
		entityID := alignEntities[difference.AlignID]
		mutation, reason := difference.uniProtMutation()
		if entityID == "" {
			orphanUnmapped = append(orphanUnmapped, describeDifference(difference, "no entity for align_id "+difference.AlignID))
			continue
		}
		if reason != "" {
			set := mutationSetFor(sets, mutationKey{entityID: entityID}, originals)
			set.Unmapped = append(set.Unmapped, describeDifference(difference, reason))
			set.NeedsReview = true
			set.ReviewReason = "engineered mutation rows could not be mapped to UniProt numbering"
			continue
		}
		key := mutationKey{entityID: entityID, accession: difference.SeqDBAccession}
		set := mutationSetFor(sets, key, originals)
		set.Mutations = append(set.Mutations, mutation)
	}

	result := make([]MutationSet, 0, len(sets)+len(originals))
	covered := map[string]bool{}
	for key, set := range sets {
		covered[key.entityID] = true
		result = append(result, *set)
	}
	for entityID, original := range originals {
		if covered[entityID] {
			continue
		}
		result = append(result, MutationSet{
			EntityID:     entityID,
			Mutations:    []string{},
			Original:     original,
			NeedsReview:  true,
			ReviewReason: "no engineered mutation rows in _struct_ref_seq_dif",
		})
	}
	if len(orphanUnmapped) > 0 {
		result = append(result, MutationSet{
			Mutations:    []string{},
			Unmapped:     orphanUnmapped,
			NeedsReview:  true,
			ReviewReason: "engineered mutation rows could not be attached to an entity",
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].EntityID != result[right].EntityID {
			return result[left].EntityID < result[right].EntityID
		}
		return result[left].Accession < result[right].Accession
	})
	return result
}

// MutationsComplete reports whether the parse read every row Result.Mutations is built from: no
// parser limit was hit and the time budget did not end the read, and a cut-off read went on into
// the coordinates, which archive files write after the mutation rows.
func (d *Document) MutationsComplete() bool {
	if d == nil || d.timedOut || len(d.notes.messages) > 0 {
		return false
	}
	return !d.truncated || d.Table("_atom_site") != nil
}

func mutationSetFor(sets map[mutationKey]*MutationSet, key mutationKey, originals map[string]string) *MutationSet {
	set, ok := sets[key]
	if !ok {
		set = &MutationSet{
			EntityID:  key.entityID,
			Accession: key.accession,
			Mutations: []string{},
			Original:  originals[key.entityID],
		}
		sets[key] = set
	}
	return set
}

// uniProtMutation returns the mutation as wild type, number, mutant (for example
// C51S) in UniProt numbering, or a reason it cannot be written that way.
func (d SequenceDifference) uniProtMutation() (string, string) {
	if !strings.EqualFold(d.SeqDBName, sequenceDatabaseUniProt) {
		return "", fmt.Sprintf("sequence database is %q, not UNP", d.SeqDBName)
	}
	if d.SeqDBAccession == "" {
		return "", "no UniProt accession"
	}
	number, err := strconv.Atoi(d.SeqDBSeqNum)
	if err != nil || number < 1 {
		return "", fmt.Sprintf("UniProt position %q is not a number", d.SeqDBSeqNum)
	}
	wildType, ok := oneLetterCodes[strings.ToUpper(d.DBMonID)]
	if !ok {
		return "", fmt.Sprintf("reference residue %q is not a standard amino acid", d.DBMonID)
	}
	mutant, ok := oneLetterCodes[strings.ToUpper(d.MonID)]
	if !ok {
		return "", fmt.Sprintf("modelled residue %q is not a standard amino acid", d.MonID)
	}
	return wildType + strconv.Itoa(number) + mutant, ""
}

func describeDifference(d SequenceDifference, reason string) string {
	return fmt.Sprintf("%s %s %s -> %s (%s): %s", d.SeqDBName, d.SeqDBAccession, d.DBMonID, d.MonID, d.SeqDBSeqNum, reason)
}

func entityMutationText(document *Document) map[string]string {
	originals := map[string]string{}
	table := document.Table("_entity")
	if table == nil {
		return originals
	}
	for _, row := range table.Rows {
		if text := table.Value(row, "pdbx_mutation"); text != "" {
			originals[table.Value(row, "id")] = text
		}
	}
	return originals
}

func alignEntityIDs(document *Document) map[string]string {
	refEntities := map[string]string{}
	if refs := document.Table("_struct_ref"); refs != nil {
		for _, row := range refs.Rows {
			refEntities[refs.Value(row, "id")] = refs.Value(row, "entity_id")
		}
	}
	alignEntities := map[string]string{}
	if aligns := document.Table("_struct_ref_seq"); aligns != nil {
		for _, row := range aligns.Rows {
			alignEntities[aligns.Value(row, "align_id")] = refEntities[aligns.Value(row, "ref_id")]
		}
	}
	return alignEntities
}
