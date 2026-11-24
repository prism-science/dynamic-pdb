package api

import (
	"errors"
	"regexp"
)

// The info block's version lines: info.version is the API's version and
// info.x-dynamic-pdb-schema-version the Dynamic PDB Schema version the API enforces.
var (
	apiVersionLine    = regexp.MustCompile(`(?m)^  version: "?([0-9]+\.[0-9]+\.[0-9]+)"?\s*$`)
	schemaVersionLine = regexp.MustCompile(`(?m)^  x-dynamic-pdb-schema-version: "?([0-9]+\.[0-9]+\.[0-9]+)"?\s*$`)
)

// Versions reads the API and schema versions from the embedded spec, so the response headers, the
// download manifest and the spec cannot disagree.
func Versions() (apiVersion, schemaVersion string, err error) {
	return versionsOf(Spec)
}

func versionsOf(spec []byte) (string, string, error) {
	apiMatch := apiVersionLine.FindSubmatch(spec)
	schemaMatch := schemaVersionLine.FindSubmatch(spec)
	if apiMatch == nil || schemaMatch == nil {
		return "", "", errors.New("openapi.yaml info needs version and x-dynamic-pdb-schema-version as MAJOR.MINOR.PATCH")
	}
	return string(apiMatch[1]), string(schemaMatch[1]), nil
}
