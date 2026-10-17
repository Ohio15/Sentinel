// Package version exposes the server release version baked into the binary.
//
// VERSION is written by scripts/sync-version.js from the root package.json,
// the single version source that release tags are gated against. It is
// embedded rather than injected with -ldflags so every build path (the
// GitHub-hosted release build, local docker builds, `go run`) reports the same
// value without extra build arguments. The nexus-deploy helper requires the
// /health response's `version` to equal the deployed tag (without the "v").
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// Version is the server release version, e.g. "1.77.52".
var Version = strings.TrimSpace(raw)
