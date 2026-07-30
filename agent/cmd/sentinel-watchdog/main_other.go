//go:build !windows

// sentinel-watchdog is a Windows-only binary that depends on golang.org/x/sys/windows
// and the Windows service control APIs. This stub exists so the build matrix in
// .github/workflows/build-installers.yml can compile sentinel-watchdog for every
// (goos, goarch) target without exploding on non-Windows hosts. Build pipelines
// that ship a Linux or macOS watchdog binary are out of scope for v1 — agent
// service supervision on those platforms uses systemd / launchd instead.

package main

import (
	"fmt"
	"os"
	"runtime"
)

// Version is set via -ldflags at build time; mirrored from the real main.go
// so that builds on either side of the build constraint embed the same value.
var Version = "1.77.41"

func main() {
	fmt.Fprintf(os.Stderr,
		"sentinel-watchdog is Windows-only (built version=%s, runtime=%s/%s).\n"+
			"Use systemd (Linux) or launchd (macOS) to supervise sentinel-agent on this platform.\n",
		Version, runtime.GOOS, runtime.GOARCH,
	)
	os.Exit(64) // EX_USAGE — explicit non-zero so CI rejects accidental cross-platform deploys.
}
