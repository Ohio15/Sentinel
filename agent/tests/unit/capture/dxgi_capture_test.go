//go:build windows

package capture_test

import (
	"testing"
	"time"

	"github.com/sentinel/agent/internal/capture"
	"github.com/sentinel/agent/tests/testutil"
)

// TestDXGICapture_Initialize tests DXGI capture initialization
func TestDXGICapture_Initialize(t *testing.T) {
	tests := []struct {
		name         string
		monitorIndex int
		expectError  bool
	}{
		{"primary monitor", 0, false},
		{"invalid monitor high", 99, true},
		{"invalid monitor negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap, err := capture.NewDXGICapture(tt.monitorIndex)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					if cap != nil {
						cap.Release()
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else {
					if cap == nil {
						t.Error("Capture is nil")
					} else {
						cap.Release()
					}
				}
			}
		})
	}
}

// TestDXGICapture_CaptureFrame tests frame capture
func TestDXGICapture_CaptureFrame(t *testing.T) {
	cap, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Skipf("DXGI capture not available: %v", err)
	}
	defer cap.Release()

	width, height := cap.GetDimensions()
	t.Logf("Capture dimensions: %dx%d", width, height)

	if width <= 0 || height <= 0 {
		t.Error("Invalid dimensions")
	}

	// Capture a frame
	frame, err := cap.CaptureFrame(100)
	if err != nil {
		t.Fatalf("CaptureFrame failed: %v", err)
	}

	// Frame may be nil if no changes (unlikely on first capture)
	if frame == nil {
		// Try again
		time.Sleep(100 * time.Millisecond)
		frame, err = cap.CaptureFrame(100)
		if err != nil {
			t.Fatalf("CaptureFrame retry failed: %v", err)
		}
	}

	if frame != nil {
		if frame.Width != width || frame.Height != height {
			t.Errorf("Frame dimensions mismatch: got %dx%d, expected %dx%d",
				frame.Width, frame.Height, width, height)
		}

		expectedStride := width * 4 // BGRA
		if frame.Stride < expectedStride {
			t.Errorf("Stride too small: got %d, expected at least %d",
				frame.Stride, expectedStride)
		}

		expectedSize := frame.Stride * height
		if len(frame.Data) < expectedSize {
			t.Errorf("Data buffer too small: got %d, expected %d",
				len(frame.Data), expectedSize)
		}
	}
}

// TestDXGICapture_CaptureFrameLatency measures capture latency
func TestDXGICapture_CaptureFrameLatency(t *testing.T) {
	cap, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Skipf("DXGI capture not available: %v", err)
	}
	defer cap.Release()

	stats := testutil.NewLatencyStats()
	const iterations = 100

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := cap.CaptureFrame(100)
		elapsed := time.Since(start)

		if err != nil {
			t.Logf("Frame %d failed: %v", i, err)
			continue
		}

		stats.Add(elapsed)
	}

	stats.Calculate()

	t.Logf("Capture latency (n=%d):", stats.Count)
	t.Logf("  Min: %v", stats.Min)
	t.Logf("  Max: %v", stats.Max)
	t.Logf("  Avg: %v", stats.Avg)
	t.Logf("  P50: %v", stats.P50)
	t.Logf("  P95: %v", stats.P95)
	t.Logf("  P99: %v", stats.P99)

	// Target: < 5ms average capture time
	if stats.Avg > 5*time.Millisecond {
		t.Errorf("Average capture latency too high: %v (target: <5ms)", stats.Avg)
	}
}

// TestDXGICapture_DirtyRectangles tests dirty rectangle detection
func TestDXGICapture_DirtyRectangles(t *testing.T) {
	cap, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Skipf("DXGI capture not available: %v", err)
	}
	defer cap.Release()

	// Capture first frame (should have full dirty rect)
	frame1, err := cap.CaptureFrame(100)
	if err != nil {
		t.Fatalf("First capture failed: %v", err)
	}

	if frame1 != nil {
		t.Logf("First frame dirty rects: %d", len(frame1.DirtyRects))
	}

	// Wait and capture again (screen likely unchanged = smaller/no dirty rects)
	time.Sleep(500 * time.Millisecond)

	frame2, err := cap.CaptureFrame(100)
	if err != nil {
		t.Fatalf("Second capture failed: %v", err)
	}

	// Frame2 may be nil if nothing changed
	if frame2 != nil {
		t.Logf("Second frame dirty rects: %d", len(frame2.DirtyRects))
	} else {
		t.Log("Second frame returned nil (no changes - expected)")
	}
}

// TestDXGICapture_MultipleCaptures tests sustained capture
func TestDXGICapture_MultipleCaptures(t *testing.T) {
	cap, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Skipf("DXGI capture not available: %v", err)
	}
	defer cap.Release()

	const targetFPS = 30
	const duration = 2 * time.Second

	frameInterval := time.Second / time.Duration(targetFPS)
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	start := time.Now()
	frameCount := 0
	errors := 0

	for time.Since(start) < duration {
		<-ticker.C
		_, err := cap.CaptureFrame(50)
		if err != nil {
			errors++
		} else {
			frameCount++
		}
	}

	actualFPS := float64(frameCount) / duration.Seconds()
	t.Logf("Captured %d frames in %v (%.1f FPS, %d errors)",
		frameCount, duration, actualFPS, errors)

	// Should achieve at least 80% of target FPS
	if actualFPS < float64(targetFPS)*0.8 {
		t.Errorf("Frame rate too low: %.1f FPS (target: %d FPS)", actualFPS, targetFPS)
	}
}

// TestDXGICapture_ReleaseAndRecreate tests cleanup and recreation
func TestDXGICapture_ReleaseAndRecreate(t *testing.T) {
	// First capture instance
	cap1, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Skipf("DXGI capture not available: %v", err)
	}

	_, err = cap1.CaptureFrame(100)
	if err != nil {
		t.Logf("First capture frame: %v", err)
	}

	cap1.Release()

	// Small delay
	time.Sleep(100 * time.Millisecond)

	// Second capture instance
	cap2, err := capture.NewDXGICapture(0)
	if err != nil {
		t.Fatalf("Failed to recreate capture: %v", err)
	}
	defer cap2.Release()

	_, err = cap2.CaptureFrame(100)
	if err != nil {
		t.Errorf("Second instance capture failed: %v", err)
	}
}

// BenchmarkDXGICapture benchmarks capture performance
func BenchmarkDXGICapture(b *testing.B) {
	cap, err := capture.NewDXGICapture(0)
	if err != nil {
		b.Skipf("DXGI capture not available: %v", err)
	}
	defer cap.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cap.CaptureFrame(50)
	}
}
