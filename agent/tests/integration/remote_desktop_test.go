//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sentinel/agent/internal/clipboard"
	"github.com/sentinel/agent/internal/diagnostics"
	"github.com/sentinel/agent/internal/filetransfer"
	"github.com/sentinel/agent/internal/webrtc"
)

// TestFileTransferIntegration tests the complete file transfer workflow
func TestFileTransferIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	config.ChunkSize = 64 // Small chunks for testing

	err := manager.Initialize(config)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Release()

	ctx := context.Background()

	// Create a temp directory for the test
	tmpDir, err := os.MkdirTemp("", "filetransfer-integration")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a source file with test content
	sourceContent := "This is test content for the file transfer integration test. "
	for i := 0; i < 10; i++ {
		sourceContent += sourceContent
	}

	sourcePath := filepath.Join(tmpDir, "source.txt")
	err = os.WriteFile(sourcePath, []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	destPath := filepath.Join(tmpDir, "dest.txt")

	// Test download flow
	t.Run("DownloadFlow", func(t *testing.T) {
		// Start download
		transfer, err := manager.StartDownload(ctx, &filetransfer.TransferRequest{
			SourcePath: sourcePath,
			DestPath:   "/remote/dest.txt",
		})
		if err != nil {
			t.Fatalf("StartDownload failed: %v", err)
		}

		// Track progress
		var progressUpdates int
		var mu sync.Mutex

		manager.OnProgress(func(tr *filetransfer.Transfer) {
			mu.Lock()
			progressUpdates++
			mu.Unlock()
		})

		// Read all chunks
		var receivedData []byte
		chunkIndex := 0
		for {
			chunk, err := manager.ReadChunk(ctx, transfer.ID, chunkIndex)
			if err != nil {
				t.Fatalf("ReadChunk failed: %v", err)
			}

			receivedData = append(receivedData, chunk.Data...)

			if chunk.IsLast {
				break
			}
			chunkIndex++
		}

		// Verify received data matches source
		if string(receivedData) != sourceContent {
			t.Errorf("Received data doesn't match source (got %d bytes, expected %d)",
				len(receivedData), len(sourceContent))
		}

		// Verify transfer completed
		finalTransfer, _ := manager.GetTransfer(ctx, transfer.ID)
		if finalTransfer.State != filetransfer.StateCompleted {
			t.Errorf("Expected state Completed, got %s", finalTransfer.State)
		}

		// Verify progress was reported
		mu.Lock()
		if progressUpdates == 0 {
			t.Error("Expected progress updates")
		}
		mu.Unlock()
	})

	// Test upload flow
	t.Run("UploadFlow", func(t *testing.T) {
		uploadContent := "Upload test content"

		transfer, err := manager.StartUpload(ctx, &filetransfer.TransferRequest{
			SourcePath: "/remote/upload.txt",
			DestPath:   destPath,
			FileSize:   int64(len(uploadContent)),
		})
		if err != nil {
			t.Fatalf("StartUpload failed: %v", err)
		}

		// Write single chunk
		ack, err := manager.WriteChunk(ctx, &filetransfer.Chunk{
			TransferID: transfer.ID,
			Index:      0,
			Offset:     0,
			Size:       len(uploadContent),
			Data:       []byte(uploadContent),
			IsLast:     true,
		})

		if err != nil || !ack.Success {
			t.Fatalf("WriteChunk failed: %v, ack error: %s", err, ack.Error)
		}

		// Verify file was written
		written, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("Failed to read destination file: %v", err)
		}

		if string(written) != uploadContent {
			t.Errorf("Written content doesn't match: got '%s', expected '%s'",
				string(written), uploadContent)
		}
	})

	// Test concurrent transfers
	t.Run("ConcurrentTransfers", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 3)

		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()

				destFile := filepath.Join(tmpDir, "concurrent%d.txt")

				_, err := manager.StartUpload(ctx, &filetransfer.TransferRequest{
					SourcePath: "/remote/source.txt",
					DestPath:   destFile,
					FileSize:   100,
				})
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Concurrent transfer error: %v", err)
		}

		// Cleanup
		transfers, _ := manager.GetActiveTransfers(ctx)
		for _, tr := range transfers {
			manager.CancelTransfer(ctx, tr.ID)
		}
	})
}

// TestReconnectionIntegration tests reconnection manager behavior
func TestReconnectionIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := webrtc.DefaultReconnectionConfig()
	config.InitialDelay = 50 * time.Millisecond
	config.MaxAttempts = 3

	manager := webrtc.NewReconnectionManager(config)

	// Track state changes
	var states []webrtc.ConnectionState
	var mu sync.Mutex

	manager.SetCallbacks(
		func(state webrtc.ConnectionState) {
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		},
		func() error {
			// Simulate successful reconnection
			return nil
		},
		func() error {
			// Simulate ICE restart
			return nil
		},
	)

	manager.Start()
	defer manager.Stop()

	// Simulate connection lifecycle
	t.Run("ConnectionLifecycle", func(t *testing.T) {
		// Connect
		manager.NotifyConnected()
		time.Sleep(50 * time.Millisecond)

		if manager.GetState() != webrtc.StateConnected {
			t.Errorf("Expected Connected, got %s", manager.GetState())
		}

		// Disconnect
		manager.NotifyDisconnected(nil)
		time.Sleep(200 * time.Millisecond)

		// Check state history
		mu.Lock()
		connectedFound := false
		disconnectedFound := false
		for _, s := range states {
			if s == webrtc.StateConnected {
				connectedFound = true
			}
			if s == webrtc.StateDisconnected {
				disconnectedFound = true
			}
		}
		mu.Unlock()

		if !connectedFound {
			t.Error("Expected Connected in state history")
		}
		if !disconnectedFound {
			t.Error("Expected Disconnected in state history")
		}
	})

	// Test quality tracking
	t.Run("QualityTracking", func(t *testing.T) {
		manager.NotifyConnected()

		// Simulate quality updates
		for i := 0; i < 10; i++ {
			manager.UpdateQuality(
				time.Duration(20+i*5)*time.Millisecond,
				float64(i)*0.1,
				time.Duration(i)*time.Millisecond,
			)
			time.Sleep(10 * time.Millisecond)
		}

		quality := manager.GetQuality()
		if quality.RTT == 0 {
			t.Error("Expected RTT to be updated")
		}
	})
}

// TestDiagnosticsIntegration tests diagnostics collection
func TestDiagnosticsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dm := diagnostics.NewStreamDiagnosticsManager("test-session", "1.0.0")

	// Register components
	dm.RegisterComponent("capture", true)
	dm.RegisterComponent("encoder", true)
	dm.RegisterComponent("network", true)

	// Track diagnostics updates
	var diagUpdates int
	var mu sync.Mutex

	dm.OnDiagnostics(func(diag *diagnostics.FullStreamDiagnostics) {
		mu.Lock()
		diagUpdates++
		mu.Unlock()
	})

	stopCh := make(chan struct{})
	dm.StartPeriodicReport(100*time.Millisecond, stopCh)

	// Simulate activity
	for i := 0; i < 100; i++ {
		dm.RecordFrame(float64(i%10), uint64(1000+i), i%3 == 0)
		dm.RecordEncode(float64(i%5), uint64(500+i), i%30 == 0)
		dm.RecordInput("move")
		time.Sleep(10 * time.Millisecond)
	}

	// Stop periodic reporting
	close(stopCh)

	// Verify diagnostics were collected
	diag := dm.GetDiagnostics()

	if diag.Capture == nil {
		t.Fatal("Expected non-nil capture stats")
	}

	if diag.Capture.FramesCaptured != 100 {
		t.Errorf("Expected 100 frames captured, got %d", diag.Capture.FramesCaptured)
	}

	if diag.Encoder == nil {
		t.Fatal("Expected non-nil encoder stats")
	}

	if diag.Encoder.FramesEncoded != 100 {
		t.Errorf("Expected 100 frames encoded, got %d", diag.Encoder.FramesEncoded)
	}

	if diag.Input == nil {
		t.Fatal("Expected non-nil input stats")
	}

	if diag.Input.MouseMoves != 100 {
		t.Errorf("Expected 100 mouse moves, got %d", diag.Input.MouseMoves)
	}

	// Verify components were tracked
	if len(diag.Components) != 3 {
		t.Errorf("Expected 3 components, got %d", len(diag.Components))
	}

	// Verify periodic reports were generated
	mu.Lock()
	if diagUpdates == 0 {
		t.Error("Expected periodic diagnostics updates")
	}
	mu.Unlock()
}

// TestClipboardConfigIntegration tests clipboard configuration
func TestClipboardConfigIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test different configurations
	t.Run("BidirectionalConfig", func(t *testing.T) {
		config := clipboard.DefaultClipboardConfig()
		config.Direction = clipboard.DirectionBidirectional

		if config.Direction != clipboard.DirectionBidirectional {
			t.Errorf("Expected bidirectional, got %s", config.Direction)
		}

		if !config.EnableText || !config.EnableImages || !config.EnableFiles {
			t.Error("Expected all formats enabled by default")
		}
	})

	t.Run("RestrictedConfig", func(t *testing.T) {
		config := clipboard.ClipboardConfig{
			Direction:    clipboard.DirectionHostToViewer,
			MaxTextSize:  1024,
			MaxImageSize: 1024 * 1024,
			EnableText:   true,
			EnableImages: false,
			EnableFiles:  false,
		}

		if config.Direction != clipboard.DirectionHostToViewer {
			t.Errorf("Expected host-to-viewer, got %s", config.Direction)
		}

		if config.EnableImages || config.EnableFiles {
			t.Error("Expected images and files disabled")
		}
	})

	t.Run("DisabledConfig", func(t *testing.T) {
		config := clipboard.ClipboardConfig{
			Direction: clipboard.DirectionDisabled,
		}

		if config.Direction != clipboard.DirectionDisabled {
			t.Errorf("Expected disabled, got %s", config.Direction)
		}
	})
}

// TestLatencyMeasurement tests end-to-end latency measurement
func TestLatencyMeasurement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dm := diagnostics.NewStreamDiagnosticsManager("test-session", "1.0.0")

	// Simulate latency measurements
	for i := 0; i < 10; i++ {
		id := "capture-%d"
		dm.StartMeasurement(id, "capture")

		// Simulate work
		time.Sleep(time.Duration(5+i) * time.Millisecond)

		duration := dm.EndMeasurement(id, true, nil)

		if duration < 5*time.Millisecond {
			t.Errorf("Expected duration >= 5ms, got %v", duration)
		}
	}

	// Verify quick stats
	stats := dm.GetQuickStats()
	if stats == nil {
		t.Fatal("Expected non-nil quick stats")
	}

	if stats["uptime"] == nil {
		t.Error("Expected uptime in quick stats")
	}
}

// TestMessageHandling tests message protocol handling
func TestMessageHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dm := diagnostics.NewStreamDiagnosticsManager("test-session", "1.0.0")

	// Test ping/pong
	t.Run("PingPong", func(t *testing.T) {
		pingMsg := []byte(`{"type":"streamDiag.ping","id":"test-1","timestamp":1234567890}`)

		resp, err := dm.HandleMessage(diagnostics.StreamDiagMsgPing, pingMsg)
		if err != nil {
			t.Fatalf("HandleMessage failed: %v", err)
		}

		if resp == nil {
			t.Fatal("Expected non-nil response")
		}

		// Response should contain pong
		if len(resp) == 0 {
			t.Error("Expected non-empty pong response")
		}
	})

	// Test get stats
	t.Run("GetStats", func(t *testing.T) {
		resp, err := dm.HandleMessage(diagnostics.StreamDiagMsgGetStats, nil)
		if err != nil {
			t.Fatalf("HandleMessage failed: %v", err)
		}

		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
	})

	// Test set level
	t.Run("SetLevel", func(t *testing.T) {
		levelMsg := []byte(`{"type":"streamDiag.setLevel","level":2}`)

		resp, err := dm.HandleMessage(diagnostics.StreamDiagMsgSetLevel, levelMsg)
		if err != nil {
			t.Fatalf("HandleMessage failed: %v", err)
		}

		if resp == nil {
			t.Fatal("Expected non-nil response")
		}

		if dm.GetLevel() != diagnostics.StreamLevelVerbose {
			t.Errorf("Expected level %d, got %d", diagnostics.StreamLevelVerbose, dm.GetLevel())
		}
	})
}
