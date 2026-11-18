// Package version carries build metadata injected at link time.
package version

import "fmt"

// These values are overridden with -ldflags "-X ...".
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a human-readable build identifier.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
