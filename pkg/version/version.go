// Package version holds build-time version metadata injected via ldflags.
package version

import "fmt"

// These are set at build time via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info returns a formatted version string.
func Info() string {
	return fmt.Sprintf("relayly %s (commit=%s built=%s)", Version, Commit, BuildTime)
}
