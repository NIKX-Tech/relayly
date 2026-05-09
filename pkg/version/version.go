// Package version holds build-time version metadata injected via ldflags.
package version

import "fmt"

// These are set at build time via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// String returns the version info as a string.
func String() string {
	return Info()
}

// Info returns a formatted version string.
func Info() string {
	return fmt.Sprintf("relayly %s (commit=%s built=%s)", Version, Commit, BuildTime)
}
