package selfheal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DiagnosticResult represents the result of a single diagnostic check
type DiagnosticResult struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // ok, warning, critical, unknown
	Message   string    `json:"message"`
	Value     float64   `json:"value,omitempty"`
	Threshold float64   `json:"threshold,omitempty"`
	FixApplied bool     `json:"fixApplied,omitempty"`
	FixResult  string   `json:"fixResult,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// HealthReport contains all diagnostic results
type HealthReport struct {
	Timestamp    time.Time           `json:"timestamp"`
	OverallScore int                 `json:"overallScore"`
	Status       string              `json:"status"` // healthy, degraded, unhealthy
	Diagnostics  []DiagnosticResult  `json:"diagnostics"`
	FixesApplied int                 `json:"fixesApplied"`
}

// Config holds self-healer configuration
type Config struct {
	CheckInterval     time.Duration
	DiskSpaceMinGB    float64
	MemoryMaxPercent  float64
	LogMaxSizeMB      float64
	TimeDriftMaxSec   int
	ConfigPath        string
	ConfigBackupPath  string
	StagingDir        string
	TempDir           string
	LogDir            string
	EnableAutoFix     bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig(dataDir string) Config {
	return Config{
		CheckInterval:    60 * time.Second,
		DiskSpaceMinGB:   1.0,
		MemoryMaxPercent: 80.0,
		LogMaxSizeMB:     100.0,
		TimeDriftMaxSec:  60,
		ConfigPath:       filepath.Join(dataDir, "config.json"),
		ConfigBackupPath: filepath.Join(dataDir, "config.json.bak"),
		StagingDir:       filepath.Join(dataDir, "staging"),
		TempDir:          filepath.Join(dataDir, "temp"),
		LogDir:           filepath.Join(dataDir, "logs"),
		EnableAutoFix:    true,
	}
}

// SelfHealer runs diagnostics and applies auto-remediation
type SelfHealer struct {
	config     Config
	running    bool
	mu         sync.RWMutex
	stopChan   chan struct{}
	onReport   func(HealthReport)
}

// New creates a new SelfHealer
func New(config Config) *SelfHealer {
	return &SelfHealer{
		config:   config,
		stopChan: make(chan struct{}),
	}
}

// OnReport sets the callback for health reports
func (s *SelfHealer) OnReport(fn func(HealthReport)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReport = fn
}

// Start begins the diagnostic loop
func (s *SelfHealer) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	go s.run(ctx)
}

// Stop stops the diagnostic loop
func (s *SelfHealer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stopChan)
	s.running = false
}

// run is the main diagnostic loop
func (s *SelfHealer) run(ctx context.Context) {
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start
	s.runDiagnostics(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.runDiagnostics(ctx)
		}
	}
}

// runDiagnostics executes all diagnostic checks
func (s *SelfHealer) runDiagnostics(ctx context.Context) {
	var results []DiagnosticResult
	var fixesApplied int

	// Run all checks
	checks := []func(context.Context) DiagnosticResult{
		s.checkDiskSpace,
		s.checkMemoryUsage,
		s.checkFilePermissions,
		s.checkConfigIntegrity,
		s.checkLogFileSize,
		s.checkTimeDrift,
		s.checkTempDirectory,
	}

	for _, check := range checks {
		result := check(ctx)
		if result.FixApplied {
			fixesApplied++
		}
		results = append(results, result)
	}

	// Calculate overall score
	score := s.calculateScore(results)
	status := s.getStatusFromScore(score)

	report := HealthReport{
		Timestamp:    time.Now(),
		OverallScore: score,
		Status:       status,
		Diagnostics:  results,
		FixesApplied: fixesApplied,
	}

	// Log summary
	log.Printf("[SelfHeal] Diagnostics complete: score=%d status=%s fixes=%d",
		score, status, fixesApplied)

	// Callback
	s.mu.RLock()
	callback := s.onReport
	s.mu.RUnlock()

	if callback != nil {
		callback(report)
	}
}

// RunOnce runs diagnostics once and returns the report
func (s *SelfHealer) RunOnce(ctx context.Context) HealthReport {
	var results []DiagnosticResult
	var fixesApplied int

	checks := []func(context.Context) DiagnosticResult{
		s.checkDiskSpace,
		s.checkMemoryUsage,
		s.checkFilePermissions,
		s.checkConfigIntegrity,
		s.checkLogFileSize,
		s.checkTimeDrift,
		s.checkTempDirectory,
	}

	for _, check := range checks {
		result := check(ctx)
		if result.FixApplied {
			fixesApplied++
		}
		results = append(results, result)
	}

	score := s.calculateScore(results)
	status := s.getStatusFromScore(score)

	return HealthReport{
		Timestamp:    time.Now(),
		OverallScore: score,
		Status:       status,
		Diagnostics:  results,
		FixesApplied: fixesApplied,
	}
}

// checkDiskSpace verifies sufficient disk space is available
func (s *SelfHealer) checkDiskSpace(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "disk_space",
		Timestamp: time.Now(),
		Threshold: s.config.DiskSpaceMinGB,
	}

	freeGB, err := getDiskFreeSpaceGB(s.config.ConfigPath)
	if err != nil {
		result.Status = "unknown"
		result.Message = fmt.Sprintf("Failed to check disk space: %v", err)
		return result
	}

	result.Value = freeGB

	if freeGB < s.config.DiskSpaceMinGB {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Low disk space: %.2f GB free (min: %.2f GB)", freeGB, s.config.DiskSpaceMinGB)

		// Try to fix by cleaning temp and staging directories
		if s.config.EnableAutoFix {
			cleaned := s.cleanDirectories()
			if cleaned > 0 {
				result.FixApplied = true
				result.FixResult = fmt.Sprintf("Cleaned %d bytes from temp/staging directories", cleaned)
			}
		}
	} else if freeGB < s.config.DiskSpaceMinGB*2 {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Disk space is low: %.2f GB free", freeGB)
	} else {
		result.Status = "ok"
		result.Message = fmt.Sprintf("Disk space OK: %.2f GB free", freeGB)
	}

	return result
}

// checkMemoryUsage monitors process memory usage
func (s *SelfHealer) checkMemoryUsage(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "memory_usage",
		Timestamp: time.Now(),
		Threshold: s.config.MemoryMaxPercent,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Get system memory to calculate percentage
	allocMB := float64(m.Alloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024

	// Use a conservative estimate of max expected memory (500MB)
	maxExpectedMB := 500.0
	usagePercent := (allocMB / maxExpectedMB) * 100

	result.Value = usagePercent

	if usagePercent > s.config.MemoryMaxPercent {
		result.Status = "critical"
		result.Message = fmt.Sprintf("High memory usage: %.1f MB allocated (%.1f%%)", allocMB, usagePercent)

		// Try to fix by running GC and clearing caches
		if s.config.EnableAutoFix {
			runtime.GC()
			result.FixApplied = true
			result.FixResult = "Triggered garbage collection"
		}
	} else if usagePercent > s.config.MemoryMaxPercent*0.8 {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Memory usage elevated: %.1f MB allocated (%.1f%%)", allocMB, usagePercent)
	} else {
		result.Status = "ok"
		result.Message = fmt.Sprintf("Memory usage OK: %.1f MB allocated, %.1f MB system", allocMB, sysMB)
	}

	return result
}

// checkFilePermissions verifies the agent can read/write required files
func (s *SelfHealer) checkFilePermissions(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "file_permissions",
		Timestamp: time.Now(),
	}

	// Test write to config directory
	testFile := filepath.Join(filepath.Dir(s.config.ConfigPath), ".permtest")
	err := os.WriteFile(testFile, []byte("test"), 0600)
	if err != nil {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Cannot write to config directory: %v", err)

		// Try to fix with icacls on Windows
		if s.config.EnableAutoFix && runtime.GOOS == "windows" {
			if s.fixWindowsPermissions(filepath.Dir(s.config.ConfigPath)) {
				result.FixApplied = true
				result.FixResult = "Reset directory permissions with icacls"
			}
		}
		return result
	}

	// Clean up test file
	os.Remove(testFile)

	// Test read of config file
	if _, err := os.ReadFile(s.config.ConfigPath); err != nil && !os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Cannot read config file: %v", err)
		return result
	}

	result.Status = "ok"
	result.Message = "File permissions OK"
	return result
}

// checkConfigIntegrity verifies the config file is valid
func (s *SelfHealer) checkConfigIntegrity(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "config_integrity",
		Timestamp: time.Now(),
	}

	// Check if config exists
	data, err := os.ReadFile(s.config.ConfigPath)
	if os.IsNotExist(err) {
		result.Status = "critical"
		result.Message = "Config file missing"

		// Try to restore from backup
		if s.config.EnableAutoFix {
			if s.restoreConfigFromBackup() {
				result.FixApplied = true
				result.FixResult = "Restored config from backup"
			}
		}
		return result
	}
	if err != nil {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Cannot read config: %v", err)
		return result
	}

	// Validate JSON
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Invalid config JSON: %v", err)

		// Try to restore from backup
		if s.config.EnableAutoFix {
			if s.restoreConfigFromBackup() {
				result.FixApplied = true
				result.FixResult = "Restored config from backup"
			}
		}
		return result
	}

	// Check required fields
	requiredFields := []string{"agentId", "serverUrl"}
	for _, field := range requiredFields {
		if _, ok := config[field]; !ok {
			result.Status = "critical"
			result.Message = fmt.Sprintf("Config missing required field: %s", field)
			return result
		}
	}

	result.Status = "ok"
	result.Message = "Config integrity OK"
	return result
}

// checkLogFileSize monitors log file sizes
func (s *SelfHealer) checkLogFileSize(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "log_file_size",
		Timestamp: time.Now(),
		Threshold: s.config.LogMaxSizeMB,
	}

	// Check log directory
	if s.config.LogDir == "" {
		result.Status = "ok"
		result.Message = "No log directory configured"
		return result
	}

	totalSizeMB := 0.0
	largestFile := ""
	largestSizeMB := 0.0

	err := filepath.Walk(s.config.LogDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".log") {
			sizeMB := float64(info.Size()) / 1024 / 1024
			totalSizeMB += sizeMB
			if sizeMB > largestSizeMB {
				largestSizeMB = sizeMB
				largestFile = path
			}
		}
		return nil
	})

	if err != nil {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Cannot scan log directory: %v", err)
		return result
	}

	result.Value = largestSizeMB

	if largestSizeMB > s.config.LogMaxSizeMB {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Large log file: %s (%.1f MB)", filepath.Base(largestFile), largestSizeMB)

		// Try to rotate the log
		if s.config.EnableAutoFix {
			if s.rotateLogFile(largestFile) {
				result.FixApplied = true
				result.FixResult = "Rotated large log file"
			}
		}
	} else {
		result.Status = "ok"
		result.Message = fmt.Sprintf("Log files OK: %.1f MB total", totalSizeMB)
	}

	return result
}

// checkTimeDrift verifies system time is synchronized
func (s *SelfHealer) checkTimeDrift(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "time_drift",
		Timestamp: time.Now(),
		Threshold: float64(s.config.TimeDriftMaxSec),
	}

	// On Windows, use w32tm to check time sync status
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "w32tm", "/query", "/status")
		output, err := cmd.Output()
		if err != nil {
			result.Status = "warning"
			result.Message = "Cannot query time sync status"
			return result
		}

		// Check for "Leap Indicator: 3" which indicates no sync
		if strings.Contains(string(output), "Leap Indicator: 3") {
			result.Status = "warning"
			result.Message = "Time sync not configured"

			// Try to force sync
			if s.config.EnableAutoFix {
				if s.forceTimeSync(ctx) {
					result.FixApplied = true
					result.FixResult = "Forced time synchronization"
				}
			}
			return result
		}

		result.Status = "ok"
		result.Message = "Time synchronized"
	} else {
		// On Linux/Mac, just report OK for now
		result.Status = "ok"
		result.Message = "Time sync check skipped on this platform"
	}

	return result
}

// checkTempDirectory ensures temp directory exists and is writable
func (s *SelfHealer) checkTempDirectory(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Name:      "temp_directory",
		Timestamp: time.Now(),
	}

	if s.config.TempDir == "" {
		result.Status = "ok"
		result.Message = "No temp directory configured"
		return result
	}

	// Ensure temp directory exists
	if err := os.MkdirAll(s.config.TempDir, 0700); err != nil {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Cannot create temp directory: %v", err)
		return result
	}

	// Test write
	testFile := filepath.Join(s.config.TempDir, ".writetest")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		result.Status = "critical"
		result.Message = fmt.Sprintf("Cannot write to temp directory: %v", err)
		return result
	}
	os.Remove(testFile)

	result.Status = "ok"
	result.Message = "Temp directory OK"
	return result
}

// Helper methods for auto-remediation

func (s *SelfHealer) cleanDirectories() int64 {
	var cleaned int64

	// Clean staging directory
	if s.config.StagingDir != "" {
		cleaned += s.cleanDirectory(s.config.StagingDir, 24*time.Hour)
	}

	// Clean temp directory
	if s.config.TempDir != "" {
		cleaned += s.cleanDirectory(s.config.TempDir, 1*time.Hour)
	}

	return cleaned
}

func (s *SelfHealer) cleanDirectory(dir string, maxAge time.Duration) int64 {
	var cleaned int64
	cutoff := time.Now().Add(-maxAge)

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || path == dir {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			size := info.Size()
			if os.Remove(path) == nil {
				cleaned += size
				log.Printf("[SelfHeal] Cleaned old file: %s", path)
			}
		}
		return nil
	})

	return cleaned
}

func (s *SelfHealer) fixWindowsPermissions(path string) bool {
	cmd := exec.Command("icacls", path, "/reset", "/t")
	if err := cmd.Run(); err != nil {
		log.Printf("[SelfHeal] Failed to reset permissions: %v", err)
		return false
	}
	return true
}

func (s *SelfHealer) restoreConfigFromBackup() bool {
	if s.config.ConfigBackupPath == "" {
		return false
	}

	data, err := os.ReadFile(s.config.ConfigBackupPath)
	if err != nil {
		log.Printf("[SelfHeal] No backup config available: %v", err)
		return false
	}

	if err := os.WriteFile(s.config.ConfigPath, data, 0600); err != nil {
		log.Printf("[SelfHeal] Failed to restore config: %v", err)
		return false
	}

	log.Println("[SelfHeal] Config restored from backup")
	return true
}

func (s *SelfHealer) rotateLogFile(path string) bool {
	// Rename to .old
	oldPath := path + ".old"
	os.Remove(oldPath) // Remove existing .old file

	if err := os.Rename(path, oldPath); err != nil {
		log.Printf("[SelfHeal] Failed to rotate log: %v", err)
		return false
	}

	log.Printf("[SelfHeal] Rotated log file: %s", path)
	return true
}

func (s *SelfHealer) forceTimeSync(ctx context.Context) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	cmd := exec.CommandContext(ctx, "w32tm", "/resync", "/force")
	if err := cmd.Run(); err != nil {
		log.Printf("[SelfHeal] Failed to force time sync: %v", err)
		return false
	}

	log.Println("[SelfHeal] Forced time synchronization")
	return true
}

func (s *SelfHealer) calculateScore(results []DiagnosticResult) int {
	if len(results) == 0 {
		return 100
	}

	total := 0
	for _, r := range results {
		switch r.Status {
		case "ok":
			total += 100
		case "warning":
			total += 70
		case "critical":
			total += 20
		case "unknown":
			total += 50
		}
	}

	return total / len(results)
}

func (s *SelfHealer) getStatusFromScore(score int) string {
	if score >= 80 {
		return "healthy"
	}
	if score >= 50 {
		return "degraded"
	}
	return "unhealthy"
}

// getDiskFreeSpaceGB returns free disk space in GB for the given path
func getDiskFreeSpaceGB(path string) (float64, error) {
	if runtime.GOOS == "windows" {
		return getWindowsDiskFreeSpace(path)
	}
	return getUnixDiskFreeSpace(path)
}

func getWindowsDiskFreeSpace(path string) (float64, error) {
	// Get drive letter from path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	drive := absPath[:3] // e.g., "C:\"

	cmd := exec.Command("wmic", "logicaldisk", "where", fmt.Sprintf("DeviceID='%s'", drive[:2]), "get", "FreeSpace", "/value")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse "FreeSpace=12345678"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "FreeSpace=") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				var bytes int64
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &bytes)
				return float64(bytes) / 1024 / 1024 / 1024, nil
			}
		}
	}

	return 0, fmt.Errorf("could not parse disk space")
}

func getUnixDiskFreeSpace(path string) (float64, error) {
	// For Unix-like systems, use df
	cmd := exec.Command("df", "-B1", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("could not parse df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("could not parse df output")
	}

	var available int64
	fmt.Sscanf(fields[3], "%d", &available)
	return float64(available) / 1024 / 1024 / 1024, nil
}
