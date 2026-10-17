package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The embedded VERSION must match the root package.json, which is what the
// release gate compares the tag against. A mismatch would make every
// nexus-deploy health check fail its version match and roll the deploy back.
func TestVersionMatchesPackageJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("package.json has no version")
	}
	if Version != pkg.Version {
		t.Fatalf("server/internal/version/VERSION = %q, package.json = %q; run `npm run sync-version`", Version, pkg.Version)
	}
}

func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(Version) {
		t.Fatalf("Version %q is not X.Y.Z", Version)
	}
}
