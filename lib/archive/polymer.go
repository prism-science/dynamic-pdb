package archive

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dynamic-pdb/lib/rcsb"
)

type UniProtSource string

const (
	UniProtSourceSIFTS     UniProtSource = "sifts"
	UniProtSourceStructRef UniProtSource = "struct_ref"
	UniProtSourceAlignment UniProtSource = "alignment"
)

const (
	rcsbDatabaseUniProt = "UniProt"
	rcsbProvenanceSIFTS = "SIFTS"
	rcsbProvenancePDB   = "PDB"
)

type SourceOrganism struct {
	ScientificName string
	NCBITaxonomyID *int
}

type UniProtMapping struct {
	Accession      string
	Source         UniProtSource
	UniProtRelease *string
}

// PolymerEntityMapping is the view of one RCSB polymer entity. UniProtMappings holds
// the reference sequence identifiers RCSB reports for the entity. AlignmentMappings
// holds UniProt accessions that only appear in the entity's sequence alignments.
type PolymerEntityMapping struct {
	Annotations

	LabelEntityID     *string
	Description       *string
	SourceOrganisms   []SourceOrganism
	Construct         *string
	Mutations         *string
	UniProtMappings   []UniProtMapping
	AlignmentMappings []UniProtMapping
	CanonicalSequence string
}

type ResidueData struct {
	LabelAsymID     string
	LabelSeqID      int
	LabelCompID     string
	AuthAsymID      *string
	AuthSeqID       *int
	PDBxPDBInsCode  *string
	UniProtPosition *string
	RSCC            *float64
	BIso            *float64
	Occupancy       *float64
}

type residueMetricKind int

const (
	residueMetricRSCC residueMetricKind = iota + 1
	residueMetricBIso
	residueMetricOccupancy
)

// residueMetricKindOf picks the RCSB instance features that become residue
// metrics. RCSB reports many other feature types, which are not residue metrics.
func residueMetricKindOf(featureType string) (residueMetricKind, bool) {
	switch strings.ToUpper(strings.TrimSpace(featureType)) {
	case "RSCC":
		return residueMetricRSCC, true
	case "OWAB":
		return residueMetricBIso, true
	case "AVERAGE_OCCUPANCY":
		return residueMetricOccupancy, true
	default:
		return 0, false
	}
}

// HasSIFTSMapping reports whether RCSB maps the entity to UniProt through SIFTS, in
// which case the mapping depends on a UniProt release.
func HasSIFTSMapping(details rcsb.PolymerEntityDetails) bool {
	for _, reference := range details.Identifiers.ReferenceSequenceIdentifiers {
		if strings.EqualFold(strings.TrimSpace(reference.DatabaseName), rcsbDatabaseUniProt) &&
			strings.EqualFold(strings.TrimSpace(reference.ProvenanceSource), rcsbProvenanceSIFTS) {
			return true
		}
	}
	return false
}

// NormalizeUniProtRelease turns a SIFTS release such as 2026.03 into 2026_03.
func NormalizeUniProtRelease(release *string) *string {
	if release == nil {
		return nil
	}
	return optionalString(strings.ReplaceAll(strings.TrimSpace(*release), ".", "_"))
}

// MapPolymerEntity maps one RCSB polymer entity. uniProtRelease is the already
// normalized UniProt release the SIFTS and alignment mappings were made against, or
// nil when it is not known.
func MapPolymerEntity(
	details rcsb.PolymerEntityDetails,
	fallbackEntityID string,
	uniProtRelease *string,
) PolymerEntityMapping {
	mapping := PolymerEntityMapping{Annotations: newAnnotations()}

	entityID := strings.TrimSpace(details.Identifiers.EntityID)
	if entityID == "" {
		entityID = strings.TrimSpace(fallbackEntityID)
	}
	mapping.LabelEntityID = optionalString(entityID)
	mapping.from("label_entity_id", "rcsb_polymer_entity_container_identifiers.entity_id")
	mapping.Description = optionalString(details.Entity.Description)
	mapping.from("description", "rcsb_polymer_entity.pdbx_description")
	mapping.Construct = optionalString(details.Entity.Fragment)
	mapping.from("construct", "rcsb_polymer_entity.pdbx_fragment")
	mapping.Mutations = optionalString(details.Entity.Mutation)
	mapping.from("mutations", "rcsb_polymer_entity.pdbx_mutation")
	mapping.CanonicalSequence = details.Polymer.CanonicalSequence
	mapping.from("canonical_sequence", "entity_poly.pdbx_seq_one_letter_code_can")

	for _, sourceOrganism := range details.SourceOrganisms {
		name := strings.TrimSpace(sourceOrganism.ScientificName)
		if name == "" && sourceOrganism.NCBITaxonomyID == nil {
			continue
		}
		mapping.SourceOrganisms = append(mapping.SourceOrganisms, SourceOrganism{
			ScientificName: name,
			NCBITaxonomyID: sourceOrganism.NCBITaxonomyID,
		})
	}
	sort.Slice(mapping.SourceOrganisms, func(first int, second int) bool {
		organisms := mapping.SourceOrganisms
		if organisms[first].ScientificName != organisms[second].ScientificName {
			return organisms[first].ScientificName < organisms[second].ScientificName
		}
		return intValue(organisms[first].NCBITaxonomyID) < intValue(organisms[second].NCBITaxonomyID)
	})
	mapping.from("source_organisms", "rcsb_entity_source_organism")

	mapping.mapReferenceIdentifiers(details, uniProtRelease)
	mapping.mapAlignments(details, uniProtRelease)
	return mapping
}

func (mapping *PolymerEntityMapping) mapReferenceIdentifiers(
	details rcsb.PolymerEntityDetails,
	uniProtRelease *string,
) {
	mapping.from("uniprot_mappings", "rcsb_polymer_entity_container_identifiers.reference_sequence_identifiers")
	for _, reference := range details.Identifiers.ReferenceSequenceIdentifiers {
		if !strings.EqualFold(strings.TrimSpace(reference.DatabaseName), rcsbDatabaseUniProt) {
			continue
		}
		accession := strings.TrimSpace(reference.DatabaseAccession)
		if accession == "" {
			continue
		}
		source, ok := uniProtSourceOf(reference.ProvenanceSource)
		if !ok {
			mapping.warn(
				"uniprot_mappings",
				"unsupported UniProt provenance %q for accession %q",
				reference.ProvenanceSource,
				accession,
			)
			continue
		}
		uniProtMapping := UniProtMapping{Accession: accession, Source: source}
		switch source {
		case UniProtSourceSIFTS:
			uniProtMapping.UniProtRelease = uniProtRelease
		case UniProtSourceStructRef, UniProtSourceAlignment:
		default:
			panic(fmt.Sprintf("unhandled UniProt source %q", source))
		}
		mapping.UniProtMappings = append(mapping.UniProtMappings, uniProtMapping)
	}
	sortUniProtMappings(mapping.UniProtMappings)
}

// mapAlignments maps UniProt accessions that RCSB only knows through its sequence
// alignments. RCSB does not say which UniProt release an alignment was made
// against, so the release SIFTS reported for the entry is assumed.
func (mapping *PolymerEntityMapping) mapAlignments(details rcsb.PolymerEntityDetails, uniProtRelease *string) {
	mapping.from("alignment_mappings", "rcsb_polymer_entity_align")
	for _, alignment := range details.Alignments {
		if !strings.EqualFold(strings.TrimSpace(alignment.ReferenceDatabaseName), rcsbDatabaseUniProt) {
			continue
		}
		accession := strings.TrimSpace(alignment.ReferenceDatabaseAccession)
		if accession == "" || mappingsContain(mapping.UniProtMappings, accession) ||
			mappingsContain(mapping.AlignmentMappings, accession) {
			continue
		}
		if uniProtRelease == nil {
			mapping.warn("alignment_mappings", "UniProt release is unknown for alignment mapping %q", accession)
		}
		mapping.AlignmentMappings = append(mapping.AlignmentMappings, UniProtMapping{
			Accession:      accession,
			Source:         UniProtSourceAlignment,
			UniProtRelease: uniProtRelease,
		})
	}
	sortUniProtMappings(mapping.AlignmentMappings)
}

func mappingsContain(mappings []UniProtMapping, accession string) bool {
	for _, mapping := range mappings {
		if mapping.Accession == accession {
			return true
		}
	}
	return false
}

func sortUniProtMappings(mappings []UniProtMapping) {
	sort.Slice(mappings, func(first int, second int) bool {
		if mappings[first].Accession != mappings[second].Accession {
			return mappings[first].Accession < mappings[second].Accession
		}
		return mappings[first].Source < mappings[second].Source
	})
}

func uniProtSourceOf(provenance string) (UniProtSource, bool) {
	switch strings.ToUpper(strings.TrimSpace(provenance)) {
	case rcsbProvenanceSIFTS:
		return UniProtSourceSIFTS, true
	case rcsbProvenancePDB:
		return UniProtSourceStructRef, true
	default:
		return "", false
	}
}

// MapResidueData joins one RCSB polymer entity instance with its entity into
// per-residue values. Only residues that carry a validation metric are returned.
func MapResidueData(
	entity rcsb.PolymerEntityDetails,
	instance rcsb.PolymerEntityInstanceDetails,
	fallbackAsymID string,
) []ResidueData {
	labelAsymID := strings.TrimSpace(instance.Identifiers.AsymID)
	if labelAsymID == "" {
		labelAsymID = strings.TrimSpace(fallbackAsymID)
	}
	authAsymID := optionalString(instance.Identifiers.AuthAsymID)

	metrics := make(map[int]*residueMetrics)
	componentIDs := make(map[int]string)
	sequenceComponentIDs := polymerComponentIDs(entity.Polymer)
	for _, feature := range instance.Features {
		metricKind, supported := residueMetricKindOf(feature.Type)
		if !supported {
			continue
		}
		for _, position := range feature.Positions {
			if position.BeginSequenceID < 1 {
				continue
			}
			if componentID := strings.TrimSpace(position.BeginComponentID); componentID != "" {
				componentIDs[position.BeginSequenceID] = componentID
			}
			for offset, value := range position.Values {
				if value == nil {
					continue
				}
				sequenceID := position.BeginSequenceID + offset
				current := metrics[sequenceID]
				if current == nil {
					current = &residueMetrics{}
					metrics[sequenceID] = current
				}
				switch metricKind {
				case residueMetricRSCC:
					current.RSCC = value
				case residueMetricBIso:
					current.BIso = value
				case residueMetricOccupancy:
					current.Occupancy = value
				default:
					panic(fmt.Sprintf("unhandled residue metric kind %d", metricKind))
				}
			}
		}
	}

	identityBySequenceID := make(map[int]ResidueData)
	for _, scheme := range instance.SequenceScheme {
		if scheme.SequenceID < 1 {
			continue
		}
		if asymID := strings.TrimSpace(scheme.AsymID); asymID != "" && asymID != labelAsymID {
			continue
		}
		if _, exists := identityBySequenceID[scheme.SequenceID]; exists {
			continue
		}
		residueAuthAsymID := optionalString(firstNonEmpty(scheme.PDBStrandID, stringValue(authAsymID)))
		identityBySequenceID[scheme.SequenceID] = ResidueData{
			LabelAsymID:     labelAsymID,
			LabelSeqID:      scheme.SequenceID,
			LabelCompID:     strings.TrimSpace(scheme.MonomerID),
			AuthAsymID:      residueAuthAsymID,
			AuthSeqID:       scheme.AuthSeqNum,
			PDBxPDBInsCode:  optionalCIFString(scheme.PDBInsCode),
			UniProtPosition: uniProtPositionAt(entity.Alignments, scheme.SequenceID),
		}
	}
	residuesBySequenceID := make(map[int]ResidueData, len(metrics))
	for sequenceID, sequenceMetrics := range metrics {
		residue, exists := identityBySequenceID[sequenceID]
		if !exists {
			authSeqID, insertionCode := authSequencePosition(
				instance.Identifiers.AuthToEntityPolySeqMapping,
				sequenceID,
			)
			residue = ResidueData{
				LabelAsymID:     labelAsymID,
				LabelSeqID:      sequenceID,
				LabelCompID:     firstNonEmpty(componentIDs[sequenceID], sequenceComponentIDs[sequenceID]),
				AuthAsymID:      authAsymID,
				AuthSeqID:       authSeqID,
				PDBxPDBInsCode:  insertionCode,
				UniProtPosition: uniProtPositionAt(entity.Alignments, sequenceID),
			}
		}
		residue.RSCC = sequenceMetrics.RSCC
		residue.BIso = sequenceMetrics.BIso
		residue.Occupancy = sequenceMetrics.Occupancy
		residuesBySequenceID[sequenceID] = residue
	}

	sequenceIDs := make([]int, 0, len(residuesBySequenceID))
	for sequenceID := range residuesBySequenceID {
		sequenceIDs = append(sequenceIDs, sequenceID)
	}
	sort.Ints(sequenceIDs)
	residues := make([]ResidueData, 0, len(sequenceIDs))
	for _, sequenceID := range sequenceIDs {
		residues = append(residues, residuesBySequenceID[sequenceID])
	}
	return residues
}

func authSequencePosition(mapping []string, labelSequenceID int) (*int, *string) {
	if labelSequenceID < 1 || labelSequenceID > len(mapping) {
		return nil, nil
	}
	value := strings.TrimSpace(mapping[labelSequenceID-1])
	if value == "" || value == "." || value == "?" {
		return nil, nil
	}

	digitEnd := 0
	if value[0] == '+' || value[0] == '-' {
		digitEnd++
	}
	digitStart := digitEnd
	for digitEnd < len(value) && value[digitEnd] >= '0' && value[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == digitStart {
		return nil, nil
	}
	position, err := strconv.Atoi(value[:digitEnd])
	if err != nil {
		return nil, nil
	}
	return &position, optionalCIFString(value[digitEnd:])
}

func polymerComponentIDs(polymer rcsb.PolymerData) map[int]string {
	sequence := polymer.Sequence
	if strings.TrimSpace(sequence) == "" {
		sequence = polymer.CanonicalSequence
	}
	components := make(map[int]string)
	sequenceID := 0
	for index := 0; index < len(sequence); {
		character := sequence[index]
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			index++
			continue
		}
		if character == '(' {
			closingOffset := strings.IndexByte(sequence[index+1:], ')')
			if closingOffset < 0 {
				break
			}
			componentID := strings.ToUpper(strings.TrimSpace(sequence[index+1 : index+1+closingOffset]))
			if componentID != "" {
				sequenceID++
				components[sequenceID] = componentID
			}
			index += closingOffset + 2
			continue
		}
		sequenceID++
		components[sequenceID] = componentIDForOneLetter(character, polymer.Type)
		index++
	}
	return components
}

func componentIDForOneLetter(character byte, polymerType string) string {
	if character >= 'a' && character <= 'z' {
		character -= 'a' - 'A'
	}
	polymerType = strings.ToLower(strings.TrimSpace(polymerType))
	if strings.Contains(polymerType, "polypeptide") {
		switch character {
		case 'A':
			return "ALA"
		case 'R':
			return "ARG"
		case 'N':
			return "ASN"
		case 'D':
			return "ASP"
		case 'C':
			return "CYS"
		case 'Q':
			return "GLN"
		case 'E':
			return "GLU"
		case 'G':
			return "GLY"
		case 'H':
			return "HIS"
		case 'I':
			return "ILE"
		case 'L':
			return "LEU"
		case 'K':
			return "LYS"
		case 'M':
			return "MET"
		case 'F':
			return "PHE"
		case 'P':
			return "PRO"
		case 'S':
			return "SER"
		case 'T':
			return "THR"
		case 'W':
			return "TRP"
		case 'Y':
			return "TYR"
		case 'V':
			return "VAL"
		case 'U':
			return "SEC"
		case 'O':
			return "PYL"
		case 'B':
			return "ASX"
		case 'Z':
			return "GLX"
		case 'J':
			return "XLE"
		default:
			return "UNK"
		}
	}
	if strings.Contains(polymerType, "polydeoxyribonucleotide") &&
		!strings.Contains(polymerType, "hybrid") {
		return "D" + string(character)
	}
	return string(character)
}

func uniProtPositionAt(alignments []rcsb.PolymerEntityAlignment, labelSequenceID int) *string {
	for _, requireSIFTS := range []bool{true, false} {
		for _, alignment := range alignments {
			if !strings.EqualFold(strings.TrimSpace(alignment.ReferenceDatabaseName), "UniProt") {
				continue
			}
			isSIFTS := strings.EqualFold(strings.TrimSpace(alignment.ProvenanceSource), "SIFTS")
			if requireSIFTS != isSIFTS {
				continue
			}
			accession := strings.TrimSpace(alignment.ReferenceDatabaseAccession)
			if accession == "" {
				continue
			}
			for _, region := range alignment.AlignedRegions {
				if region.Length < 1 || labelSequenceID < region.EntityBeginSequenceID ||
					labelSequenceID >= region.EntityBeginSequenceID+region.Length {
					continue
				}
				position := region.ReferenceBeginSequenceID + labelSequenceID - region.EntityBeginSequenceID
				value := fmt.Sprintf("%s:%d", accession, position)
				return &value
			}
		}
	}
	return nil
}

func hasSIFTSMapping(details rcsb.PolymerEntityDetails) bool {
	for _, reference := range details.Identifiers.ReferenceSequenceIdentifiers {
		if strings.EqualFold(strings.TrimSpace(reference.DatabaseName), "UniProt") &&
			strings.EqualFold(strings.TrimSpace(reference.ProvenanceSource), "SIFTS") {
			return true
		}
	}
	return false
}

type residueMetrics struct {
	RSCC      *float64
	BIso      *float64
	Occupancy *float64
}
