// Package version exposes build metadata injected via -ldflags.
package version

import "runtime"

// Set at build time:
//
//	-ldflags "-X github.com/PaulichP/Portunus/server/internal/version.Version=v0.1.0
//	          -X github.com/PaulichP/Portunus/server/internal/version.Commit=abc123"
var (
	Version = "dev"
	Commit  = "unknown"
)

// GoVersion is the Go toolchain the binary was built with.
func GoVersion() string { return runtime.Version() }
