package database

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestMigrationFilenameInvariants enforces three invariants that together
// prevent the dead-letter migration class of bug (file added but not
// registered in the runner — produced today's incident).
//
// Invariant 1: every file in the embedded migrations/ directory is named
//              ^000NNN_<snake_description>\.sql$ (6-digit canonical version).
// Invariant 2: the version numbers parsed from filenames are strictly
//              contiguous from 1 to N, with no gaps and no duplicates.
// Invariant 3: the migrationFiles slice in database.go has the same set
//              of files as the embedded directory, in the same order.
//
// Phase 3 of the golang-migrate cutover will retire the slice and rely on
// invariants 1 and 2 alone (golang-migrate parses filenames). Until then,
// invariant 3 catches the specific failure mode that bit us today.
func TestMigrationFilenameInvariants(t *testing.T) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}

	pattern := regexp.MustCompile(`^([0-9]{6})_[a-z0-9_]+\.up\.sql$`)
	versions := make([]int, 0, len(entries))
	embeddedNames := make(map[string]bool, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("invariant 1: subdirectory found in migrations/: %s", e.Name())
			continue
		}
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("invariant 1: filename %q does not match ^[0-9]{6}_[a-z0-9_]+\\.up\\.sql$", e.Name())
			continue
		}
		v, _ := strconv.Atoi(m[1])
		versions = append(versions, v)
		embeddedNames["migrations/"+e.Name()] = true
	}

	sort.Ints(versions)
	for i, v := range versions {
		want := i + 1
		if v != want {
			t.Errorf("invariant 2: expected version %d at sorted index %d, got %d (gap or duplicate in migrations dir)", want, i, v)
		}
	}

	// Invariant 3: slice in database.go matches embedded directory.
	// We can't read the slice directly without import cycle; instead, we
	// rebuild the runner's view from the same source via the Migrate
	// function's contract, by introspecting via a dedicated accessor. To
	// keep this test self-contained without coupling to package internals,
	// we re-parse the embed.FS and compare to a hardcoded "expected count"
	// that's updated as part of any new-migration PR. The diff between
	// embedded count and slice count is what catches dead-letter bugs.
	if len(versions) == 0 {
		t.Fatal("no migrations found in embedded directory")
	}
	maxV := versions[len(versions)-1]
	if maxV != len(versions) {
		t.Errorf("invariant 2: max version %d != file count %d (gap detected)", maxV, len(versions))
	}

	// Invariant 3: every embedded file appears in the migrationFiles slice.
	// The slice is a private package variable; we surface it via a test
	// hook below to avoid re-declaring the file list.
	registered := registeredMigrationFilesForTest()
	if len(registered) != len(embeddedNames) {
		t.Errorf("invariant 3: slice has %d entries, embedded dir has %d files (one or more dead-letter)", len(registered), len(embeddedNames))
	}
	for _, p := range registered {
		if !embeddedNames[p] {
			t.Errorf("invariant 3: slice references %q which is not in embedded migrations/ — file deleted or renamed without slice update", p)
		}
	}
	for name := range embeddedNames {
		found := false
		for _, p := range registered {
			if p == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("invariant 3: embedded file %q is not registered in migrationFiles slice — dead-letter risk", name)
		}
	}

	// Invariant 3b: slice is in version order. Slice index N should reference
	// file 000(N+1)_*.sql.
	for i, p := range registered {
		want := fmt.Sprintf("migrations/%06d_", i+1)
		if !strings.HasPrefix(p, want) {
			t.Errorf("invariant 3b: slice[%d] = %q, expected to start with %q", i, p, want)
		}
	}
}
