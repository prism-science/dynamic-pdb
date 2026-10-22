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

type compoundKind int

const (
	kindLigand compoundKind = iota
	kindCofactor
	kindMetalIon
	kindSolvent
)

// Reference sorts chemical component IDs into solvents, metal ions, cofactors
// and ligands. The data lives in cofactors.json and is versioned.
type Reference struct {
	Version   string
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
	Solvents  []string `json:"solvents"`
}

var loadDefaultReference = sync.OnceValues(func() (*Reference, error) {
	return parseReference(referenceJSON)
})

// DefaultReference returns the reference list embedded in this package.
func DefaultReference() (*Reference, error) {
	reference, err := loadDefaultReference()
	if err != nil {
		return nil, fmt.Errorf("load embedded cofactor reference: %w", err)
	}
	return reference, nil
}

func parseReference(data []byte) (*Reference, error) {
	var file referenceFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode cofactor reference: %w", err)
	}
	if strings.TrimSpace(file.Version) == "" {
		return nil, fmt.Errorf("cofactor reference has no version")
	}
	reference := &Reference{
		Version:   file.Version,
		Approved:  file.Approved,
		cofactors: map[string]string{},
		metals:    map[string]struct{}{},
		solvents:  map[string]struct{}{},
	}
	for _, cofactor := range file.Cofactors {
		reference.cofactors[strings.ToUpper(cofactor.ID)] = cofactor.Name
	}
	for _, id := range file.MetalIons {
		reference.metals[strings.ToUpper(id)] = struct{}{}
	}
	for _, id := range file.Solvents {
		reference.solvents[strings.ToUpper(id)] = struct{}{}
	}
	return reference, nil
}

func (r *Reference) kind(compoundID string) compoundKind {
	id := strings.ToUpper(compoundID)
	if _, ok := r.solvents[id]; ok {
		return kindSolvent
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
