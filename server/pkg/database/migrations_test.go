package database

import (
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// TestMigrationFilenameInvariants enforces two filename-level invariants
// that together prevent the migration-discovery class of bugs:
//
// Invariant 1: every file in the embedded migrations/ directory is named
//              ^000NNN_<snake_description>\.(up|down)\.sql$ — 6-digit
//              canonical version + direction suffix required by
//              golang-migrate's iofs source. Down migrations are optional
//              companions, so both directions are valid filenames.
// Invariant 2: the version numbers parsed from .up.sql filenames are
//              strictly contiguous from 1 to N, with no gaps and no
//              duplicates. Only up-migrations are counted: a .down.sql
//              companion shares its version with the .up.sql it reverses,
//              so counting both would register every companion as a
//              duplicate version.
//
// Phase 5 of the golang-migrate cutover (v1.77.29) retired the
// migrationFilesRegistry slice; filesystem order alone now drives
// application order. The previous Invariant 3 (slice == embedded dir)
// is no longer applicable.
func TestMigrationFilenameInvariants(t *testing.T) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}

	pattern := regexp.MustCompile(`^([0-9]{6})_[a-z0-9_]+\.(up|down)\.sql$`)
	versions := make([]int, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("invariant 1: subdirectory found in migrations/: %s", e.Name())
			continue
		}
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("invariant 1: filename %q does not match ^[0-9]{6}_[a-z0-9_]+\\.(up|down)\\.sql$", e.Name())
			continue
		}
		if m[2] != "up" {
			// Down migrations are version companions, not new versions.
			continue
		}
		v, _ := strconv.Atoi(m[1])
		versions = append(versions, v)
	}

	if len(versions) == 0 {
		t.Fatal("no migrations found in embedded directory")
	}

	sort.Ints(versions)
	for i, v := range versions {
		want := i + 1
		if v != want {
			t.Errorf("invariant 2: expected version %d at sorted index %d, got %d (gap or duplicate in migrations dir)", want, i, v)
		}
	}

	maxV := versions[len(versions)-1]
	if maxV != len(versions) {
		t.Errorf("invariant 2: max version %d != file count %d (gap detected)", maxV, len(versions))
	}
}
