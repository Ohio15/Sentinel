//go:build windows

// testmain_test centralizes the per-test "DXGI not available" skip path so
// CI runners without a GPU (or without an interactive desktop session — the
// Windows Server 2025 hosted runners in build-installers.yml) don't hang
// the whole package's 60s default timeout.
//
// Before this file landed (issue #23), TestDXGICapture_Initialize's "primary
// monitor" subtest called capture.NewDXGICapture(0) and asserted success.
// On a GPU-less runner that call either errored OR blocked indefinitely
// waiting for a Windows GUI session, exceeding the package timeout and
// killing the test process. The panic took down every other test in the
// same `go test` invocation, including the unrelated tests/unit/webrtc
// package — making the failure look like a webrtc regression rather than
// a capture-package issue.
//
// Pattern: TestMain probes once with a hard 5-second deadline. If the probe
// fails or times out, we set a package-level flag that every test consults
// up-front to call t.Skipf cleanly.

package capture_test

import (
	"os"
	"testing"
	"time"

	"github.com/sentinel/agent/internal/capture"
)

// dxgiSkipReason captures why DXGI is unavailable on this runner, if at all.
// Empty string => DXGI works, tests run normally. Non-empty => every test
// should call t.Skipf with this reason.
var dxgiSkipReason string

func TestMain(m *testing.M) {
	dxgiSkipReason = probeDXGI(5 * time.Second)
	os.Exit(m.Run())
}

// probeDXGI runs NewDXGICapture(0) in a goroutine with a hard deadline. If
// the call returns an error within the deadline we treat DXGI as unavailable.
// If it times out we treat DXGI as unavailable AND log the timeout — that's
// the no-GUI-session case on CI runners. If it succeeds we Release the probe
// instance immediately and return empty string (tests proceed normally).
func probeDXGI(deadline time.Duration) string {
	type probeResult struct {
		cap *capture.DXGICapture
		err error
	}
	resultCh := make(chan probeResult, 1)
	go func() {
		c, err := capture.NewDXGICapture(0)
		resultCh <- probeResult{cap: c, err: err}
	}()

	select {
	case r := <-resultCh:
		if r.err != nil {
			return "DXGI capture not available (probe error: " + r.err.Error() + ")"
		}
		if r.cap == nil {
			return "DXGI capture not available (probe returned nil capture)"
		}
		r.cap.Release()
		return ""
	case <-time.After(deadline):
		// Goroutine still running — leaked, but acceptable on CI where the
		// process exits at the end of the test run anyway. The alternative
		// is forcing the underlying Windows API to abort, which we have no
		// portable way to do.
		return "DXGI capture probe timed out after " + deadline.String() +
			" (likely no GPU / no interactive desktop session — common on CI runners)"
	}
}

// requireDXGI is the standard skip-guard. Every test in this package that
// touches NewDXGICapture should call it first.
func requireDXGI(t *testing.T) {
	t.Helper()
	if dxgiSkipReason != "" {
		t.Skip(dxgiSkipReason)
	}
}

// requireLiveCapture skips timing/throughput tests that need a live, actively
// rendering desktop. On a headless CI runner DXGI initializes fine, but the
// desktop is static so AcquireNextFrame only ever returns WAIT_TIMEOUT and each
// CaptureFrame blocks the full timeout — making latency (<5ms) and FPS (>=24)
// assertions impossible. Skipping display-dependent benchmarks on CI is the
// standard practice; the init/error-path logic is still covered by
// TestDXGICapture_Initialize, which runs everywhere.
func requireLiveCapture(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("live-desktop capture benchmark skipped on CI (no interactive rendering session)")
	}
}
