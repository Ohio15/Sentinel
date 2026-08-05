package database

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

// createTablePattern matches a table-defining CREATE TABLE statement. The
// trailing `\s*\(` requirement is what keeps partition DDL out of the results:
// `CREATE TABLE IF NOT EXISTS %I PARTITION OF ...` (emitted from EXECUTE
// format() inside plpgsql in 000003/000005) has no column list, and neither do
// static `... PARTITION OF ... FOR VALUES ...` statements.
var createTablePattern = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)"?\s*\(`)

// duplicateTableDefinitionAllowlist records the table definitions that are
// knowingly duplicated across migrations, with the reason each is safe. A new
// entry here means someone reviewed the interaction; an unlisted duplicate
// fails the test.
//
// The value is the up-migration filename of the LATER (redundant) definition.
var duplicateTableDefinitionAllowlist = map[string]string{
	// 000024 creates agent_health first, so 000058's CREATE TABLE body never
	// executes on any real database. 000058 is written for exactly that: every
	// column it needs is added by an explicit ADD COLUMN IF NOT EXISTS block
	// before any index references it. See the header comment in 000058.
	"agent_health:000058_agent_health_recovery.up.sql": "columns re-added via ADD COLUMN IF NOT EXISTS before use",

	// 000025 creates email_templates (subject/body_html/body_text); 000033
	// re-declares it with a different shape (subject_template/html_template).
	// 000033's definition is dead on every database, but nothing in 000033 or
	// any later migration references the dead columns, and no Go code reads
	// this table at all, so it cannot abort a migration. Left as-is rather
	// than guessed at: reconciling the two shapes needs a product decision.
	"email_templates:000033_agent_installation_links.up.sql": "dead redefinition, no downstream references",
}

// TestNoUnreviewedDuplicateTableDefinitions guards the defect class that broke
// every fresh-database install: two migrations defining the same table with
// different shapes.
//
// CREATE TABLE IF NOT EXISTS is silently skipped when an earlier migration
// already created the table, so the later file's column list is dead — but the
// statements after it (CREATE INDEX, ALTER, INSERT) still run against the
// EARLIER shape. 000013 indexed agent_logs(timestamp), a column only its own
// skipped definition declared, and aborted the whole migration with SQLSTATE
// 42703 on any database whose schema_migrations had not already passed v13.
// 000058 hit the identical trap on agent_health and left production dirty.
//
// Filename order is application order, so "earlier file wins" is decidable
// statically. Any new duplicate must be reviewed and allowlisted above.
func TestNoUnreviewedDuplicateTableDefinitions(t *testing.T) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // zero-padded versions sort in application order

	owner := make(map[string]string, 128)
	usedAllowlistEntries := make(map[string]bool, len(duplicateTableDefinitionAllowlist))
	for _, name := range names {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		seenInFile := make(map[string]bool)
		for _, m := range createTablePattern.FindAllStringSubmatch(string(body), -1) {
			table := strings.ToLower(m[1])
			if seenInFile[table] {
				continue
			}
			seenInFile[table] = true

			first, dup := owner[table]
			if !dup {
				owner[table] = name
				continue
			}
			key := fmt.Sprintf("%s:%s", table, name)
			if _, allowed := duplicateTableDefinitionAllowlist[key]; allowed {
				usedAllowlistEntries[key] = true
				continue
			}
			t.Errorf("table %q is created by %s and re-declared by %s: the later "+
				"CREATE TABLE is skipped at runtime, so any statement in %s that "+
				"touches a column only that definition declares will abort the "+
				"migration on a fresh database. Remove the duplicate definition, "+
				"or add %q to duplicateTableDefinitionAllowlist with justification.",
				table, first, name, name, key)
		}
	}

	// Keep the allowlist honest: an entry that no longer corresponds to a real
	// duplicate is a suppression waiting to hide the next regression.
	for key := range duplicateTableDefinitionAllowlist {
		if !usedAllowlistEntries[key] {
			t.Errorf("allowlist entry %q no longer matches a duplicate table definition — remove it", key)
		}
	}
}

// sqlMigrateDirectivePattern matches the rubenv/sql-migrate direction markers
// `-- +migrate Up` and `-- +migrate Down`.
var sqlMigrateDirectivePattern = regexp.MustCompile(`(?m)^\s*--\s*\+migrate\s+(Up|Down)`)

// TestNoSQLMigrateDirectivesInUpMigrations guards a silent, success-reporting
// data-loss class.
//
// Two migration runners express direction in incompatible ways:
//
//	sql-migrate (rubenv/sql-migrate) puts BOTH directions in ONE file and
//	  splits them on the `-- +migrate Up` / `-- +migrate Down` marker comments.
//	golang-migrate (this repo's runner, server/go.mod) puts each direction in
//	  its OWN file and derives direction from the .up.sql / .down.sql FILENAME.
//	  It has no notion of `+migrate` markers — to golang-migrate they are
//	  ordinary SQL comments.
//
// So a file written for sql-migrate and fed to golang-migrate executes its
// Up block AND its Down block, back to back, in the same migration. The
// tables are created and then immediately dropped. Nothing errors, the
// migration is recorded as applied, and the runner exits 0 — the schema is
// simply missing. That is exactly what happened to 000041/000042/000043/000044:
// a fresh database came up "successfully" with no webhooks, patch_*, script_*
// or mfa_events tables, and the failure only surfaced later as runtime
// "relation does not exist" errors far from the cause.
//
// A .down.sql file is allowed to contain drops — that is its job, and
// golang-migrate only runs it on an explicit rollback. The invariant is
// narrow and absolute: no direction marker may appear in a .up.sql.
func TestNoSQLMigrateDirectivesInUpMigrations(t *testing.T) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("read migration %s: %v", e.Name(), err)
		}
		for _, m := range sqlMigrateDirectivePattern.FindAllStringSubmatch(string(body), -1) {
			t.Errorf("%s contains sql-migrate directive %q: this repo runs "+
				"golang-migrate, which ignores +migrate markers and executes the "+
				"WHOLE file. Any statements under a `-- +migrate Down` marker "+
				"therefore run immediately after the Up statements — the migration "+
				"drops what it just created and still reports success, leaving a "+
				"silently incomplete schema. Delete the markers and move the Down "+
				"statements into a sibling %s file.",
				e.Name(), strings.TrimSpace(m[0]),
				strings.TrimSuffix(e.Name(), ".up.sql")+".down.sql")
		}
	}
}

// dropTablePattern matches a DROP TABLE naming a literal table. Identifiers
// interpolated at runtime (`EXECUTE format('DROP TABLE IF EXISTS %I', ...)` in
// the 000003 partition sweeper) do not match, which is correct: those drop
// partitions by computed name, not anything this file created.
var dropTablePattern = regexp.MustCompile(`(?is)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)"?`)

// TestNoTableCreatedAndDroppedInSameMigration is the static half of the
// stronger schema guard: it asserts no up-migration both creates a table and
// drops that same table.
//
// The ideal check applies the full migration set to a live PostgreSQL instance
// and asserts every table created by some .up.sql (minus any intentionally
// dropped by a LATER migration) actually exists afterward. That needs a real
// database; this package's tests run with no DB dependency, so the live half
// belongs in CI's migration job rather than here. This static half catches the
// same defect at its source: a self-destructing migration is always a
// create-then-drop within one file, whatever produced it (a stray sql-migrate
// Down block, a bad merge, a copy-pasted rollback).
//
// Cross-file drops are deliberately NOT flagged — 000018 dropping a
// mistyped device_updates so a later migration can recreate it is legitimate
// repair, and only a live database can tell that apart from a mistake.
func TestNoTableCreatedAndDroppedInSameMigration(t *testing.T) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("read migration %s: %v", e.Name(), err)
		}
		text := string(body)

		created := make(map[string]bool)
		for _, m := range createTablePattern.FindAllStringSubmatch(text, -1) {
			created[strings.ToLower(m[1])] = true
		}
		if len(created) == 0 {
			continue
		}
		reported := make(map[string]bool)
		for _, m := range dropTablePattern.FindAllStringSubmatch(text, -1) {
			table := strings.ToLower(m[1])
			if !created[table] || reported[table] {
				continue
			}
			reported[table] = true
			t.Errorf("%s creates table %q and then drops it in the same migration: "+
				"golang-migrate runs the whole file as one migration, so this "+
				"applies cleanly, records the version as applied, and leaves no "+
				"%s table behind. Move the DROP into %s.",
				e.Name(), table, table,
				strings.TrimSuffix(e.Name(), ".up.sql")+".down.sql")
		}
	}
}
