//go:build windows

package testutil

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestEnvironment provides a controlled environment for testing remote desktop components
type TestEnvironment struct {
	TempDir      string
	CleanupFuncs []func()
	Context      context.Context
	Cancel       context.CancelFunc
	mu           sync.Mutex
}

// NewTestEnvironment creates a new test environment with temporary directories
func NewTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	env := &TestEnvironment{
		TempDir: t.TempDir(),
		Context: ctx,
		Cancel:  cancel,
	}

	// Ensure cleanup on test failure
	t.Cleanup(func() {
		env.Cleanup()
	})

	return env
}

// AddCleanup adds a cleanup function to be called when environment is torn down
func (e *TestEnvironment) AddCleanup(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.CleanupFuncs = append(e.CleanupFuncs, fn)
}

// Cleanup releases all resources in reverse order
func (e *TestEnvironment) Cleanup() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Cancel context first
	if e.Cancel != nil {
		e.Cancel()
	}

	// Run cleanup functions in reverse order
	for i := len(e.CleanupFuncs) - 1; i >= 0; i-- {
		e.CleanupFuncs[i]()
	}
	e.CleanupFuncs = nil
}

// CreateTempFile creates a temporary file with the given content
func (e *TestEnvironment) CreateTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(e.TempDir, name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", dir, err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("Failed to create temp file %s: %v", path, err)
	}
	return path
}

// CreateTempDir creates a temporary subdirectory
func (e *TestEnvironment) CreateTempDir(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(e.TempDir, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("Failed to create temp dir %s: %v", path, err)
	}
	return path
}

// WaitFor waits for a condition to become true or timeout
func (e *TestEnvironment) WaitFor(t *testing.T, condition func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for: %s", msg)
}

// AssertEventually asserts that a condition becomes true within the timeout
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration, msgAndArgs ...interface{}) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(msgAndArgs) > 0 {
		t.Fatalf("Condition not met within %v: %v", timeout, msgAndArgs...)
	} else {
		t.Fatalf("Condition not met within %v", timeout)
	}
}

// MeasureLatency measures the time to execute a function
func MeasureLatency(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}

// LatencyStats holds latency measurement statistics
type LatencyStats struct {
	Min    time.Duration
	Max    time.Duration
	Avg    time.Duration
	P50    time.Duration
	P95    time.Duration
	P99    time.Duration
	Count  int
	values []time.Duration
}

// NewLatencyStats creates a new latency stats collector
func NewLatencyStats() *LatencyStats {
	return &LatencyStats{
		Min:    time.Duration(1<<63 - 1),
		values: make([]time.Duration, 0, 1000),
	}
}

// Add adds a latency measurement
func (s *LatencyStats) Add(d time.Duration) {
	s.values = append(s.values, d)
	s.Count++
	if d < s.Min {
		s.Min = d
	}
	if d > s.Max {
		s.Max = d
	}
}

// Calculate computes percentiles (call after all measurements)
func (s *LatencyStats) Calculate() {
	if len(s.values) == 0 {
		return
	}

	// Sort values
	sorted := make([]time.Duration, len(s.values))
	copy(sorted, s.values)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate average
	var sum time.Duration
	for _, v := range s.values {
		sum += v
	}
	s.Avg = sum / time.Duration(len(s.values))

	// Calculate percentiles
	s.P50 = sorted[len(sorted)*50/100]
	s.P95 = sorted[len(sorted)*95/100]
	s.P99 = sorted[len(sorted)*99/100]
}

// MemoryStats tracks memory usage
type MemoryStats struct {
	StartHeap  uint64
	EndHeap    uint64
	PeakHeap   uint64
	Allocs     uint64
	TotalAlloc uint64
}

// GetMemoryStats captures current memory statistics
func GetMemoryStats() MemoryStats {
	// Would use runtime.ReadMemStats in real implementation
	return MemoryStats{}
}

// TestInput creates a test input event
type TestInput struct {
	Type      string
	X, Y      float64
	Button    int
	Key       string
	Modifiers struct {
		Ctrl  bool
		Alt   bool
		Shift bool
		Meta  bool
	}
}

// GenerateTestInputs creates a sequence of test inputs for testing
func GenerateTestInputs(count int) []TestInput {
	inputs := make([]TestInput, count)
	for i := 0; i < count; i++ {
		inputs[i] = TestInput{
			Type: "move",
			X:    float64(i * 10 % 1920),
			Y:    float64(i * 10 % 1080),
		}
	}
	return inputs
}
