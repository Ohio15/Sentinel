package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConfigLoadNoDeadlock ensures Load() doesn't deadlock during migration
func TestConfigLoadNoDeadlock(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()

	// Create a test config file (unencrypted to trigger migration)
	configPath := filepath.Join(tmpDir, "config.json")
	testConfig := `{"serverUrl": "http://localhost:8080", "enrolled": false}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Override GetConfigPath for testing
	originalGetConfigPath := GetConfigPath
	GetConfigPath = func() string { return configPath }
	defer func() { GetConfigPath = originalGetConfigPath }()

	// Reset singleton
	mu.Lock()
	instance = nil
	mu.Unlock()

	// Test that Load completes without deadlock
	done := make(chan bool, 1)
	go func() {
		_, err := Load()
		if err != nil {
			t.Logf("Load error (may be expected in test): %v", err)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK: Load() did not complete within 5 seconds")
	}
}

// TestConfigSaveNoDeadlock ensures Save() doesn't deadlock
func TestConfigSaveNoDeadlock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Override GetConfigPath for testing
	originalGetConfigPath := GetConfigPath
	GetConfigPath = func() string { return configPath }
	defer func() { GetConfigPath = originalGetConfigPath }()

	cfg := DefaultConfig()
	cfg.ServerURL = "http://test:8080"

	done := make(chan bool, 1)
	go func() {
		err := cfg.Save()
		if err != nil {
			t.Logf("Save error (may be expected in test): %v", err)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK: Save() did not complete within 5 seconds")
	}
}

// TestConfigConcurrentAccess tests concurrent Load/Save operations
func TestConfigConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create initial config
	testConfig := `{"serverUrl": "http://localhost:8080", "enrolled": false}`
	if err := os.WriteFile(configPath, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Override GetConfigPath for testing
	originalGetConfigPath := GetConfigPath
	GetConfigPath = func() string { return configPath }
	defer func() { GetConfigPath = originalGetConfigPath }()

	// Reset singleton
	mu.Lock()
	instance = nil
	mu.Unlock()

	var wg sync.WaitGroup
	errors := make(chan error, 20)
	timeout := time.After(10 * time.Second)

	// Run concurrent operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Load()
			if err != nil {
				errors <- err
			}
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := DefaultConfig()
			cfg.ServerURL = "http://concurrent:8080"
			err := cfg.Save()
			if err != nil {
				errors <- err
			}
		}()
	}

	// Wait with timeout
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-timeout:
		t.Fatal("DEADLOCK: Concurrent operations did not complete within 10 seconds")
	}

	close(errors)
	for err := range errors {
		t.Logf("Concurrent operation error (may be expected): %v", err)
	}
}

// TestGetConfigPathNotEmpty ensures config path is never empty
func TestGetConfigPathNotEmpty(t *testing.T) {
	path := GetConfigPath()
	if path == "" {
		t.Error("GetConfigPath() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("GetConfigPath() should return absolute path, got: %s", path)
	}
}

// TestDefaultConfigValues ensures default config has sensible values
func TestDefaultConfigValues(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.HeartbeatInterval <= 0 {
		t.Error("HeartbeatInterval should be positive")
	}
	if cfg.MetricsInterval <= 0 {
		t.Error("MetricsInterval should be positive")
	}
	if cfg.Enrolled {
		t.Error("Default config should not be enrolled")
	}
}
