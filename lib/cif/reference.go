package cif

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed cofactors.json
var referenceJSON []byte

//go:embed solvents.json
var solventsJSON []byte

type compoundKind int

const (
	kindLigand compoundKind = iota
	kindCofactor
	kindMetalIon
	kindSolvent
	// kindDraftSolvent is a compound on the solvent list while that list is not
	// approved: it is kept as a ligand and flagged, never dropped.
	kindDraftSolvent
)

// Reference sorts chemical component IDs into solvents, metal ions, cofactors
// and ligands. Cofactors and metal ions live in cofactors.json, solvents in
// solvents.json; each file is versioned and carries its own approval flag.
type Reference struct {
	// Version combines both file versions so a stored value names every list it used.
	Version         string
	CofactorVersion string
	SolventVersion  string
	// Approved is true only when researchers approved both lists.
	Approved  bool
	cofactors map[string]string
	metals    map[string]struct{}
	solvents  map[string]struct{}
}

type referenceFile struct {
	Version     string `json:"version"`
	Approved    bool   `json:"approved"`
	Description string `json:"description"`
	Cofactors   []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"cofactors"`
	MetalIons []string `json:"metal_ions"`
}

type solventFile struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Approved    bool     `json:"approved"`
	Description string   `json:"description"`
	Solvents    []string `json:"solvents"`
}

var loadDefaultReference = sync.OnceValues(func() (*Reference, error) {
	return parseReference(referenceJSON, solventsJSON)
})

// DefaultReference returns the reference list embedded in this package.
func DefaultReference() (*Reference, error) {
	reference, err := loadDefaultReference()
	if err != nil {
		return nil, fmt.Errorf("load embedded cofactor reference: %w", err)
	}
	return reference, nil
}

func parseReference(data []byte, solventData []byte) (*Reference, error) {
	var file referenceFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode cofactor reference: %w", err)
	}
	if strings.TrimSpace(file.Version) == "" {
		return nil, fmt.Errorf("cofactor reference has no version")
	}
	var solventList solventFile
	if err := json.Unmarshal(solventData, &solventList); err != nil {
		return nil, fmt.Errorf("decode solvent list: %w", err)
	}
	if strings.TrimSpace(solventList.Version) == "" {
		return nil, fmt.Errorf("solvent list has no version")
	}
	reference := &Reference{
		Version:         "cofactors:" + file.Version + "+solvents:" + solventList.Version,
		CofactorVersion: file.Version,
		SolventVersion:  solventList.Version,
		Approved:        file.Approved && solventList.Approved,
		cofactors:       map[string]string{},
		metals:          map[string]struct{}{},
		solvents:        map[string]struct{}{},
	}
	for _, cofactor := range file.Cofactors {
		reference.cofactors[strings.ToUpper(cofactor.ID)] = cofactor.Name
	}
	for _, id := range file.MetalIons {
		reference.metals[strings.ToUpper(id)] = struct{}{}
	}
	for _, id := range solventList.Solvents {
		reference.solvents[strings.ToUpper(id)] = struct{}{}
	}
	return reference, nil
}

// DraftWarning is reported with every extraction made while the reference lists
// are not approved.
const DraftWarning = "reference lists are draft and not yet approved by researchers: only water is dropped from ligands; " +
	"buffers, salts and cryoprotectants on the draft solvent list are kept as ligands and flagged for review"

// draftSolventReason explains the flag on a compound kept only because the
// solvent list is a draft.
const draftSolventReason = "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped"

// kind sorts a compound. Water is always dropped. Other solvents are dropped
// only once both reference lists are approved; until then they are kept.
func (r *Reference) kind(compoundID string) compoundKind {
	id := strings.ToUpper(compoundID)
	if isWater(id) {
		return kindSolvent
	}
	if _, ok := r.solvents[id]; ok {
		if r.Approved {
			return kindSolvent
		}
		return kindDraftSolvent
	}
	if _, ok := r.metals[id]; ok {
		return kindMetalIon
	}
	if _, ok := r.cofactors[id]; ok {
		return kindCofactor
	}
	return kindLigand
}

func (r *Reference) isSolvent(compoundID string) bool {
	return r.kind(compoundID) == kindSolvent
}
