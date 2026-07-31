package api

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestSafeCreateAutoUnhideAlertContainsPanic locks the panic-containment scope
// that two review rounds got wrong.
//
// The alert path runs BEFORE the audit write in notifyAutoUnhide, and the alert
// row is committed before the dashboard broadcast. If a panic in that path is
// only caught by the goroutine-level recover, it unwinds past audit.LogEvent and
// the restore lands in `alerts` with no audit record — a silent unhide, which is
// precisely what this feature exists to prevent. The containment therefore has
// to live around the alert call, not around the goroutine.
//
// A nil pool injects a genuine panic (createAutoUnhideAlert dereferences it on
// the INSERT), so this asserts the real recover rather than a mock of it.
// Deleting the defer/recover in safeCreateAutoUnhideAlert turns this test red.
func TestSafeCreateAutoUnhideAlertContainsPanic(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("panic escaped safeCreateAutoUnhideAlert (audit write would be skipped): %v", rec)
		}
	}()

	created := safeCreateAutoUnhideAlert(
		context.Background(),
		nil, // panics inside the alert INSERT
		nil,
		uuid.New(),
		"host",
		"title",
		"message",
	)

	if created {
		t.Error("alert reported as created after a panic in the alert path")
	}
}

// TestAutoUnhideOnReconnectNilPool guards the synchronous half: a nil pool must
// short-circuit before the unhide UPDATE rather than panicking on the
// connection-establishment path, which is not a detached goroutine.
func TestAutoUnhideOnReconnectNilPool(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("autoUnhideOnReconnect panicked on the connection path: %v", rec)
		}
	}()

	autoUnhideOnReconnect(context.Background(), nil, nil, uuid.New(), unhideTriggerWSAuth)
}

// TestSanitizeForLog covers the log-forgery defence: devices.hostname is
// agent-supplied, so it must never reach a log line or an audit detail with
// control characters intact.
func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "LAPTOP-S7NS7F7D", "LAPTOP-S7NS7F7D"},
		{"crlf forged record", "evil\r\n2026/07/31 [Unhide] forged", "evil��2026/07/31 [Unhide] forged"},
		{"bare newline", "a\nb", "a�b"},
		{"ansi escape", "\x1b[31mRED\x1b[0m", "�[31mRED�[0m"},
		{"nul byte", "a\x00b", "a�b"},
		{"tab is control", "a\tb", "a�b"},
		{"multibyte preserved", "服务器-01", "服务器-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeForLog(tt.in); got != tt.want {
				t.Errorf("sanitizeForLog(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeForLogTruncation checks the rune-based bound, including the
// multi-byte case where a byte-based limit would cut mid-character.
func TestSanitizeForLogTruncation(t *testing.T) {
	got := sanitizeForLog(strings.Repeat("A", 200))
	if want := strings.Repeat("A", maxLoggedNameRunes) + "…"; got != want {
		t.Errorf("long ASCII: got %q, want %q", got, want)
	}

	// Exactly at the limit must not be marked as truncated.
	exact := strings.Repeat("A", maxLoggedNameRunes)
	if got := sanitizeForLog(exact); got != exact {
		t.Errorf("exact-length input was altered: got %q, want %q", got, exact)
	}

	// Multi-byte runes are counted as runes, not bytes.
	multi := sanitizeForLog(strings.Repeat("服", 200))
	if wantRunes := maxLoggedNameRunes + 1; len([]rune(multi)) != wantRunes {
		t.Errorf("multibyte truncation produced %d runes, want %d", len([]rune(multi)), wantRunes)
	}
	if !strings.HasSuffix(multi, "…") {
		t.Errorf("multibyte truncation missing ellipsis: %q", multi)
	}
}
