package jobs

import (
	"fmt"
	"slices"
	"strings"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const (
	manyEntriesWarning      = "models of %d entries use this file: no chain length was taken from entry FASTA records"
	conflictingFASTAWarning = "entry FASTA records give chain %s different lengths (%s): its length is left unknown"
)

// fastaChain is one chain a FASTA header names: "A" is label and author chain A, "A[auth B]"
// label chain A and author chain B.
type fastaChain struct {
	label string
	auth  string
}

// headerChains reads the chains of an RCSB-style FASTA header, "1ABC_1|Chains A, B[auth C]|name|
// organism": the second field, after "Chain" or "Chains". A header without that field names none.
func headerChains(header string) []fastaChain {
	fields := strings.Split(header, "|")
	if len(fields) < 2 {
		return nil
	}
	field := strings.TrimSpace(fields[1])
	lower := strings.ToLower(field)
	switch {
	case strings.HasPrefix(lower, "chains "):
		field = field[len("chains "):]
	case strings.HasPrefix(lower, "chain "):
		field = field[len("chain "):]
	default:
		return nil
	}
	chains := make([]fastaChain, 0)
	for _, item := range strings.Split(field, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		label, bracket, hasAuth := strings.Cut(item, "[")
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		chain := fastaChain{label: label, auth: label}
		if hasAuth {
			authID, found := strings.CutPrefix(strings.TrimSuffix(strings.TrimSpace(bracket), "]"), "auth ")
			authID = strings.TrimSpace(authID)
			if !found || authID == "" {
				continue
			}
			chain.auth = authID
		}
		chains = append(chains, chain)
	}
	return chains
}

// fastaLengths are the sequence lengths the FASTA records give each chain ID, as author chain and
// as label chain.
type fastaLengths struct {
	byAuth  map[string][]int
	byLabel map[string][]int
}

func newFASTALengths(records []models.FASTARecordLength) fastaLengths {
	lengths := fastaLengths{byAuth: map[string][]int{}, byLabel: map[string][]int{}}
	for _, record := range records {
		for _, chain := range headerChains(record.Header) {
			lengths.byAuth[chain.auth] = appendDistinct(lengths.byAuth[chain.auth], record.Length)
			lengths.byLabel[chain.label] = appendDistinct(lengths.byLabel[chain.label], record.Length)
		}
	}
	return lengths
}

func appendDistinct(values []int, value int) []int {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// lengthOf returns the FASTA length of a file's chain: the record naming it as author chain,
// else the record naming it as label chain (computed files often keep the label chain IDs). It
// reports the lengths when the records that name it disagree.
func (f fastaLengths) lengthOf(chainID string) (int, models.ChainLengthSource, []int) {
	for _, candidate := range []struct {
		lengths []int
		source  models.ChainLengthSource
	}{
		{f.byAuth[chainID], models.ChainLengthSourceFASTAAuthChain},
		{f.byLabel[chainID], models.ChainLengthSourceFASTALabelChain},
	} {
		switch len(candidate.lengths) {
		case 0:
			continue
		case 1:
			return candidate.lengths[0], candidate.source, nil
		default:
			return 0, "", candidate.lengths
		}
	}
	return 0, "", nil
}

// resolveIndex is what a save stores for a read, given the FASTA records of the entries of the
// file's models: the chains with their lengths, and the read's warnings with the FASTA ones.
func resolveIndex(read cif.CoordinateIndex, fasta models.ModelFileFASTA) (SavedIndex, error) {
	chains, fastaWarnings, err := indexedChains(read, fasta)
	if err != nil {
		return SavedIndex{}, err
	}
	return SavedIndex{
		Chains:   chains,
		Warnings: append(append([]string{}, read.Warnings...), fastaWarnings...),
		Entries:  len(fasta.EntryIDs),
	}, nil
}

// indexedChains turns a read's chains into stored chains, taking the length of a chain whose
// file states none from the entry's FASTA records. FASTA records are used only when the file
// belongs to models of one entry. It returns the warnings to add to the read's.
func indexedChains(read cif.CoordinateIndex, fasta models.ModelFileFASTA) ([]models.IndexedChain, []string, error) {
	warnings := make([]string, 0)
	useFASTA := len(fasta.EntryIDs) == 1
	if len(fasta.EntryIDs) > 1 {
		warnings = append(warnings, fmt.Sprintf(manyEntriesWarning, len(fasta.EntryIDs)))
	}
	lengths := newFASTALengths(fasta.Records)
	chains := make([]models.IndexedChain, 0, len(read.Chains))
	for _, chain := range read.Chains {
		proteinSource, err := storedProteinSource(chain.ProteinSource)
		if err != nil {
			return nil, nil, fmt.Errorf("chain %q: %w", chain.AuthAsymID, err)
		}
		stored := models.IndexedChain{
			AuthAsymID:      chain.AuthAsymID,
			EntityID:        chain.EntityID,
			ModeledResidues: chain.ModeledResidues,
			Protein:         chain.Protein,
			ProteinSource:   proteinSource,
		}
		length, source, err := fileLength(chain)
		if err != nil {
			return nil, nil, fmt.Errorf("chain %q: %w", chain.AuthAsymID, err)
		}
		if length == 0 && useFASTA {
			var conflicting []int
			length, source, conflicting = lengths.lengthOf(chain.AuthAsymID)
			if len(conflicting) > 0 {
				warnings = append(warnings, fmt.Sprintf(conflictingFASTAWarning, chain.AuthAsymID, joinInts(conflicting)))
			}
		}
		if length > 0 {
			stored.DepositedLength = &length
			stored.LengthSource = &source
		}
		chains = append(chains, stored)
	}
	return chains, warnings, nil
}

// fileLength returns the deposited length the file itself states, with its source; 0 when it
// states none.
func fileLength(chain cif.IndexedChain) (int, models.ChainLengthSource, error) {
	if chain.DepositedLength <= 0 {
		return 0, "", nil
	}
	switch chain.LengthSource {
	case cif.LengthFromEntityPolySeq:
		return chain.DepositedLength, models.ChainLengthSourceEntityPolySeq, nil
	case cif.LengthFromSeqres:
		return chain.DepositedLength, models.ChainLengthSourceSeqres, nil
	default:
		return 0, "", fmt.Errorf("unknown chain length source %q", chain.LengthSource)
	}
}

// storedProteinSource names the reader's protein test as the chain table stores it.
func storedProteinSource(source cif.ProteinSource) (models.ProteinSource, error) {
	switch source {
	case cif.ProteinFromEntityPolyType:
		return models.ProteinSourceEntityPolyType, nil
	case cif.ProteinFromResidueMajority:
		return models.ProteinSourceResidueMajority, nil
	default:
		return "", fmt.Errorf("unknown protein source %q", source)
	}
}

func joinInts(values []int) string {
	texts := make([]string, 0, len(values))
	for _, value := range values {
		texts = append(texts, fmt.Sprint(value))
	}
	return strings.Join(texts, ", ")
}
