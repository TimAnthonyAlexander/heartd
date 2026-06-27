// Package version exposes the heartd build version. The value defaults to "dev"
// and is overridden at build time via -ldflags
// "-X github.com/timanthonyalexander/heartd/internal/version.Version=<v>"
// (see the Makefile, which injects `git describe`).
package version

// Version is the heartd build version, e.g. "v0.4.4" or "v0.4.4-4-gabc1234".
var Version = "dev"
