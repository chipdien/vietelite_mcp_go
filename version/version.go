// Package version exposes build metadata for the MCP server.
//
// The values are injected at link time by the release workflow:
//
//	go build -ldflags "-X vietelite_mcp_go/version.Version=v1.2.3 ..."
//
// When built with a plain `go build` (local development), the values fall back
// to the development defaults below.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// Injected at build time via -ldflags. Keep these as plain vars so the linker
// can overwrite them.
var (
	// Version is the semantic version tag, e.g. "v1.2.3". "dev" when unset.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
	// BuildDate is the UTC build timestamp in RFC3339 form.
	BuildDate = "unknown"
)

// IsRelease reports whether this binary was built from a release tag.
func IsRelease() bool {
	return Version != "" && Version != "dev"
}

// String returns a human-readable one-line version description.
func String() string {
	var b strings.Builder
	b.WriteString("vietelite-mcp ")
	b.WriteString(Version)
	if Commit != "" && Commit != "none" {
		fmt.Fprintf(&b, " (commit %s)", Commit)
	}
	if BuildDate != "" && BuildDate != "unknown" {
		fmt.Fprintf(&b, " built %s", BuildDate)
	}
	fmt.Fprintf(&b, " [%s/%s, %s]", runtime.GOOS, runtime.GOARCH, runtime.Version())
	return b.String()
}
