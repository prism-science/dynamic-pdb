package archive

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dynamic-pdb/lib/rcsb"
)

const (
	MethodXRayCrystallography = "X-ray crystallography"
	MethodCryoEM              = "CryoEM"
)

const (
	rcsbStatusReleased = "REL"

	obsoleteRecordObsoleted  = "OBSLTE"
	obsoleteRecordSuperseded = "SPRSDE"
)

// EntryMapping is the entry-level view of an RCSB entry. Method is one of the
// Method constants, or the raw RCSB text when RCSB reports another method.
type EntryMapping struct {
	Annotations

	Title           *string
	Details         *string
	Resolution      *float64
	Method          *string
	SpaceGroup      *string
	Crystallography *Crystallography

	StatusCode         *string
	InitialReleaseDate *time.Time
	LatestRevisionDate *time.Time
	LatestVersion      *string
	RevisionGroups     []RevisionGroup
	Supersedes         []string
	SupersededBy       []string
}

type Crystallography struct {
	Crystals []Crystal
}

type Crystal struct {
	ID           string
	Growth       *CrystalGrowth
	Diffractions []Diffraction
}

type CrystalGrowth struct {
	PH                *float64
	TemperatureKelvin *float64
}

type Diffraction struct {
	ID                string
	TemperatureKelvin *float64
}

// RevisionGroup says which group of changes revision RevisionOrdinal of the entry
// belongs to, for example "Advisory" or "Data collection".
type RevisionGroup struct {
	Group           string
	RevisionOrdinal int
}

func MapEntry(details rcsb.EntryDetails) EntryMapping {
	mapping := EntryMapping{Annotations: newAnnotations()}

	mapping.Title = optionalString(details.Structure.Title)
	mapping.from("title", "struct.title")
	mapping.Details = optionalString(details.Structure.Details)
	mapping.from("details", "struct.pdbx_details")
	mapping.SpaceGroup = optionalString(details.Symmetry.SpaceGroup)
	mapping.from("space_group", "symmetry.space_group_name_H_M")

	mapping.mapResolution(details.Info.CombinedResolution)
	mapping.mapMethod(details.Experiments)
	mapping.Crystallography = crystallographyFromRCSB(details)
	mapping.from("crystallography", "exptl_crystal,exptl_crystal_grow,diffrn")
	mapping.mapRelease(details.AccessionInfo)
	mapping.mapRevisionGroups(details.RevisionGroups)
	mapping.mapObsoleteRecords(details.ObsoleteRecords)
	return mapping
}

func (mapping *EntryMapping) mapResolution(resolutions []float64) {
	mapping.from("resolution", "rcsb_entry_info.resolution_combined[0]")
	if len(resolutions) == 0 {
		return
	}
	resolution := resolutions[0]
	mapping.Resolution = &resolution
	values := make([]string, 0, len(resolutions))
	for _, value := range resolutions {
		values = append(values, strconv.FormatFloat(value, 'g', -1, 64))
	}
	mapping.conflictIfDifferent("resolution", values)
}

func (mapping *EntryMapping) mapMethod(experiments []rcsb.EntryExperiment) {
	mapping.from("method", "exptl[0].method")
	if len(experiments) == 0 {
		return
	}
	rawMethod := strings.TrimSpace(experiments[0].Method)
	if rawMethod == "" {
		return
	}
	method, recognized := normalizeMethod(rawMethod)
	mapping.Method = &method
	if !recognized {
		mapping.warn("method", "unrecognized experimental method %q kept as reported", rawMethod)
	}
	methods := make([]string, 0, len(experiments))
	for _, experiment := range experiments {
		if value := strings.TrimSpace(experiment.Method); value != "" {
			methods = append(methods, value)
		}
	}
	mapping.conflictIfDifferent("method", methods)
}

func normalizeMethod(rawMethod string) (string, bool) {
	normalizedMethod := strings.ToLower(rawMethod)
	switch {
	case strings.Contains(normalizedMethod, "x-ray"), strings.Contains(normalizedMethod, "xray"):
		return MethodXRayCrystallography, true
	case strings.Contains(normalizedMethod, "electron microscopy"),
		strings.Contains(normalizedMethod, "cryo-em"),
		strings.Contains(normalizedMethod, "cryoem"):
		return MethodCryoEM, true
	default:
		return rawMethod, false
	}
}

func (mapping *EntryMapping) mapRelease(info rcsb.EntryAccessionInfo) {
	mapping.StatusCode = optionalString(info.StatusCode)
	mapping.from("status_code", "rcsb_accession_info.status_code")
	if status := strings.TrimSpace(info.StatusCode); status != "" && status != rcsbStatusReleased {
		mapping.warn("status_code", "RCSB status is %q, not released", status)
	}

	mapping.InitialReleaseDate = mapping.mapTime(
		"initial_release_date", "rcsb_accession_info.initial_release_date", info.InitialReleaseDate,
	)
	mapping.LatestRevisionDate = mapping.mapTime(
		"latest_revision_date", "rcsb_accession_info.revision_date", info.RevisionDate,
	)
	mapping.from("latest_version", "rcsb_accession_info.major_revision,rcsb_accession_info.minor_revision")
	if info.MajorRevision != nil && info.MinorRevision != nil {
		version := fmt.Sprintf("%d.%d", *info.MajorRevision, *info.MinorRevision)
		mapping.LatestVersion = &version
	}
}

func (mapping *EntryMapping) mapTime(field string, path string, raw string) *time.Time {
	mapping.from(field, path)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := parseRCSBTime(raw)
	if err != nil {
		mapping.warn(field, "%s", err)
		return nil
	}
	return &parsed
}

func (mapping *EntryMapping) mapRevisionGroups(groups []rcsb.EntryRevisionGroup) {
	mapping.from("revision_groups", "pdbx_audit_revision_group")
	seen := make(map[RevisionGroup]bool, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Group)
		if name == "" {
			continue
		}
		revisionGroup := RevisionGroup{Group: name, RevisionOrdinal: group.RevisionOrdinal}
		if seen[revisionGroup] {
			continue
		}
		seen[revisionGroup] = true
		mapping.RevisionGroups = append(mapping.RevisionGroups, revisionGroup)
	}
	sort.SliceStable(mapping.RevisionGroups, func(first int, second int) bool {
		if mapping.RevisionGroups[first].RevisionOrdinal != mapping.RevisionGroups[second].RevisionOrdinal {
			return mapping.RevisionGroups[first].RevisionOrdinal < mapping.RevisionGroups[second].RevisionOrdinal
		}
		return mapping.RevisionGroups[first].Group < mapping.RevisionGroups[second].Group
	})
}

func (mapping *EntryMapping) mapObsoleteRecords(records []rcsb.EntryObsoleteRecord) {
	mapping.from("supersedes", "pdbx_database_PDB_obs_spr")
	mapping.from("superseded_by", "pdbx_database_PDB_obs_spr")
	for _, record := range records {
		replaced := strings.ToUpper(strings.TrimSpace(record.ReplacePDBID))
		switch strings.ToUpper(strings.TrimSpace(record.ID)) {
		case obsoleteRecordSuperseded:
			mapping.Supersedes = appendUnique(mapping.Supersedes, replaced)
		case obsoleteRecordObsoleted:
			mapping.SupersededBy = appendUnique(mapping.SupersededBy, replaced)
		default:
			mapping.warn("supersedes", "unknown obsolete record kind %q", record.ID)
		}
	}
	sort.Strings(mapping.Supersedes)
	sort.Strings(mapping.SupersededBy)
}

func appendUnique(values []string, value string) []string {
	if value == "" || containsString(values, value) {
		return values
	}
	return append(values, value)
}

func crystallographyFromRCSB(details rcsb.EntryDetails) *Crystallography {
	crystalsByID := make(map[string]Crystal)
	ensureCrystal := func(rawID string) (Crystal, string, bool) {
		crystalID := strings.TrimSpace(rawID)
		if crystalID == "" {
			return Crystal{}, "", false
		}
		crystal, ok := crystalsByID[crystalID]
		if !ok {
			crystal = Crystal{ID: crystalID}
		}
		return crystal, crystalID, true
	}

	for _, sourceCrystal := range details.Crystals {
		crystal, crystalID, ok := ensureCrystal(sourceCrystal.ID)
		if ok {
			crystalsByID[crystalID] = crystal
		}
	}
	for _, sourceGrowth := range details.CrystalGrowth {
		crystal, crystalID, ok := ensureCrystal(sourceGrowth.CrystalID)
		if !ok {
			continue
		}
		if sourceGrowth.PH != nil || sourceGrowth.TemperatureKelvin != nil {
			crystal.Growth = &CrystalGrowth{
				PH:                sourceGrowth.PH,
				TemperatureKelvin: sourceGrowth.TemperatureKelvin,
			}
		}
		crystalsByID[crystalID] = crystal
	}
	for _, sourceDiffraction := range details.Diffractions {
		crystal, crystalID, ok := ensureCrystal(sourceDiffraction.CrystalID)
		if !ok {
			continue
		}
		diffractionID := strings.TrimSpace(sourceDiffraction.ID)
		if diffractionID == "" {
			continue
		}
		crystal.Diffractions = append(crystal.Diffractions, Diffraction{
			ID:                diffractionID,
			TemperatureKelvin: sourceDiffraction.TemperatureKelvin,
		})
		crystalsByID[crystalID] = crystal
	}
	if len(crystalsByID) == 0 {
		return nil
	}

	crystalIDs := make([]string, 0, len(crystalsByID))
	for crystalID := range crystalsByID {
		crystalIDs = append(crystalIDs, crystalID)
	}
	sort.Strings(crystalIDs)
	crystals := make([]Crystal, 0, len(crystalIDs))
	for _, crystalID := range crystalIDs {
		crystal := crystalsByID[crystalID]
		sort.Slice(crystal.Diffractions, func(first int, second int) bool {
			return crystal.Diffractions[first].ID < crystal.Diffractions[second].ID
		})
		crystals = append(crystals, crystal)
	}
	return &Crystallography{Crystals: crystals}
}
