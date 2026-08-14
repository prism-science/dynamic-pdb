package version

import "strings"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func IsRelease() bool {
	switch Version {
	case "", "dev":
		return false
	}
	return !strings.HasPrefix(Version, "v0.0.0-e2e")
}
