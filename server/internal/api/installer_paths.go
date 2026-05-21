package api

import (
	"fmt"
	"os"
	"path/filepath"
)

// canonicalArtifactSearchRoots is the ordered list of filesystem roots searched
// when locating installer-related artifacts (agent, watchdog, bootstrap, desktop
// helper, installer template). Container-mounted paths come first so production
// deployments take precedence over local dev fallbacks.
//
// This is the single source of truth. Per-handler ad-hoc search lists drifted
// over time and caused production 404s (incident 2026-05-21: installer template
// missing from /app/installers because 4 of 5 handlers omitted that root).
var canonicalArtifactSearchRoots = []string{
	"/app/installers",
	"installers",
	"release/agent",
	"agent",
	"../installers",
	".",
}

// findArtifact walks the canonical search roots in order and returns the first
// matching path for any of the candidate filenames. Returns "" if none exist.
// Candidate names may include subdirectory components (e.g. "windows/foo.exe").
func findArtifact(filenames ...string) string {
	for _, root := range canonicalArtifactSearchRoots {
		for _, name := range filenames {
			p := filepath.Join(root, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// platformBinaryName returns the standard filename for a per-platform/arch
// binary. The naming convention is `sentinel-{kind}-{platform}-{arch}` with
// `.exe` appended for Windows.
func platformBinaryName(kind, platform, arch string) string {
	ext := ""
	if platform == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("sentinel-%s-%s-%s%s", kind, platform, arch, ext)
}

// findPlatformBinary locates a per-platform binary by kind (agent, watchdog,
// bootstrap, desktop-helper). It tries the platform-arch-specific name first,
// falling back to the unsuffixed name used by local dev builds.
func findPlatformBinary(kind, platform, arch string) string {
	primary := platformBinaryName(kind, platform, arch)
	ext := ""
	if platform == "windows" {
		ext = ".exe"
	}
	fallback := fmt.Sprintf("sentinel-%s%s", kind, ext)
	return findArtifact(primary, fallback)
}

// findInstallerTemplate locates the Inno Setup template for the Windows
// installer flow. The template is shared across architectures because it's
// always a Windows-only PE that bundles agent.exe / watchdog.exe internally.
func findInstallerTemplate() string {
	return findArtifact("sentinel-installer-template.exe")
}

// findBaseInstaller locates a platform-specific base installer package
// (sentinel-setup-base.exe on Windows, sentinel-agent-base-{arch}.{deb|rpm|pkg|spk}
// elsewhere). On Windows the template form is preferred when present.
func findBaseInstaller(platform, arch string) string {
	var (
		baseName  string
		extension string
	)
	switch platform {
	case "windows":
		baseName = "sentinel-setup-base"
		extension = ".exe"
	case "linux-deb":
		baseName = "sentinel-agent-base-" + arch
		extension = ".deb"
	case "linux-rpm":
		baseName = "sentinel-agent-base-" + arch
		extension = ".rpm"
	case "macos":
		baseName = "sentinel-agent-base-" + arch
		extension = ".pkg"
	case "synology":
		baseName = "sentinel-agent-base-" + arch
		extension = ".spk"
	default:
		return ""
	}

	candidates := []string{
		filepath.Join(platform, baseName+extension),
		baseName + extension,
	}
	if platform == "windows" {
		// Inno Setup template is preferred over a raw base installer on Windows
		// because the patch path expects the XYZCFG/marker block.
		candidates = append([]string{"sentinel-installer-template.exe"}, candidates...)
	}
	return findArtifact(candidates...)
}
