package updater

import (
	"strings"

	"golang.org/x/mod/semver"
)

// CompareVersions compares two semantic version strings, with or without a
// leading "v", by semver precedence: pre-releases, including the Go
// pseudo-versions of binaries built from a checkout (e.g.
// "v1.2.4-0.20260102150405-abcdef123456"), sort before their release, and
// build metadata (e.g. "+dirty") is ignored.
// Returns -1 if current < latest, 0 if equal, 1 if current > latest.
// Invalid versions (e.g. "undefined") are treated as older than any valid version.
func CompareVersions(current, latest string) int {
	return semver.Compare(withVPrefix(current), withVPrefix(latest))
}

// withVPrefix adds the leading "v" that golang.org/x/mod/semver requires.
func withVPrefix(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
