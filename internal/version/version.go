// Package version exposes build-time identification.
//
// Values are populated via -ldflags at link time. See Makefile.
package version

var (
	// Version is the semver tag or "dev" for untagged builds.
	Version = "dev"
	// Commit is the short git SHA at build time.
	Commit = "unknown"
	// Date is the build date in ISO 8601 (UTC).
	Date = "unknown"
)
