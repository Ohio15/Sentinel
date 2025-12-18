package protection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IntegrityConfig holds file integrity monitoring configuration
type IntegrityConfig struct {
	ProtectedFiles  []string      // Paths to protected files
	BaselinePath    string        // Path to store baseline hashes
	CheckInterval   time.Duration // How often to check integrity
	OnTamperReport  func(TamperReport)
	OnRecoverFile   func(path string) error // Callback to recover a tampered file
}

// FileBaseline represents the expected state of a protected file
type FileBaseline struct {
	Path         string    `json:"path"`
	Hash         string    `json:"hash"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"modTime"`
	RecordedAt   time.Time `json:"recordedAt"`
}

// BaselineStore holds all file baselines
type BaselineStore struct {
	Version   int                      `json:"version"`
	CreatedAt time.Time                `json:"createdAt"`
	UpdatedAt time.Time                `json:"updatedAt"`
	Files     map[string]FileBaseline  `json:"files"`
}

// TamperReport represents a detected integrity violation
type TamperReport struct {
	Timestamp    time.Time `json:"timestamp"`
	Path         string    `json:"path"`
	ExpectedHash string    `json:"expectedHash"`
	ActualHash   string    `json:"actualHash"`
	TamperType   string    `json:"tamperType"` // modified, deleted, size_changed
	Recovered    bool      `json:"recovered"`
	RecoverError string    `json:"recoverError,omitempty"`
}

// IntegrityMonitor monitors file integrity
type IntegrityMonitor struct {
	config   IntegrityConfig
	baseline *BaselineStore
	running  bool
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewIntegrityMonitor creates a new integrity monitor
func NewIntegrityMonitor(config IntegrityConfig) *IntegrityMonitor {
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Minute
	}
	return &IntegrityMonitor{
		config:   config,
		baseline: &BaselineStore{Files: make(map[string]FileBaseline)},
		stopChan: make(chan struct{}),
	}
}

// GenerateBaseline creates hash baselines for all protected files
func (im *IntegrityMonitor) GenerateBaseline() error {
	im.mu.Lock()
	defer im.mu.Unlock()

	store := &BaselineStore{
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Files:     make(map[string]FileBaseline),
	}

	for _, path := range im.config.ProtectedFiles {
		baseline, err := im.generateFileBaseline(path)
		if err != nil {
			log.Printf("[Integrity] Warning: Cannot baseline %s: %v", path, err)
			continue
		}
		store.Files[path] = baseline
		log.Printf("[Integrity] Baseline generated for %s: %s", filepath.Base(path), baseline.Hash[:16]+"...")
	}

	im.baseline = store

	// Save to disk
	if im.config.BaselinePath != "" {
		if err := im.saveBaseline(store); err != nil {
			log.Printf("[Integrity] Warning: Cannot save baseline: %v", err)
		}
	}

	log.Printf("[Integrity] Baseline generated for %d files", len(store.Files))
	return nil
}

// LoadBaseline loads the baseline from disk
func (im *IntegrityMonitor) LoadBaseline() error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.config.BaselinePath == "" {
		return fmt.Errorf("baseline path not configured")
	}

	data, err := os.ReadFile(im.config.BaselinePath)
	if err != nil {
		return fmt.Errorf("failed to read baseline: %w", err)
	}

	var store BaselineStore
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("failed to parse baseline: %w", err)
	}

	im.baseline = &store
	log.Printf("[Integrity] Loaded baseline for %d files", len(store.Files))
	return nil
}

// Start begins the integrity monitoring loop
func (im *IntegrityMonitor) Start(ctx context.Context) {
	im.mu.Lock()
	if im.running {
		im.mu.Unlock()
		return
	}
	im.running = true
	im.stopChan = make(chan struct{})
	im.mu.Unlock()

	go im.run(ctx)
}

// Stop stops the integrity monitoring loop
func (im *IntegrityMonitor) Stop() {
	im.mu.Lock()
	defer im.mu.Unlock()

	if !im.running {
		return
	}

	close(im.stopChan)
	im.running = false
}

// run is the main monitoring loop
func (im *IntegrityMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(im.config.CheckInterval)
	defer ticker.Stop()

	// Check immediately on start
	im.checkIntegrity(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-im.stopChan:
			return
		case <-ticker.C:
			im.checkIntegrity(ctx)
		}
	}
}

// CheckIntegrity performs an immediate integrity check
func (im *IntegrityMonitor) CheckIntegrity() []TamperReport {
	return im.checkIntegrity(context.Background())
}

// checkIntegrity verifies all protected files
func (im *IntegrityMonitor) checkIntegrity(ctx context.Context) []TamperReport {
	im.mu.RLock()
	baseline := im.baseline
	im.mu.RUnlock()

	if baseline == nil || len(baseline.Files) == 0 {
		return nil
	}

	var reports []TamperReport

	for path, expected := range baseline.Files {
		select {
		case <-ctx.Done():
			return reports
		default:
		}

		report := im.verifyFile(path, expected)
		if report != nil {
			reports = append(reports, *report)

			// Report tamper detection
			if im.config.OnTamperReport != nil {
				im.config.OnTamperReport(*report)
			}
		}
	}

	if len(reports) > 0 {
		log.Printf("[Integrity] Detected %d integrity violations", len(reports))
	}

	return reports
}

// verifyFile checks a single file against its baseline
func (im *IntegrityMonitor) verifyFile(path string, expected FileBaseline) *TamperReport {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		report := &TamperReport{
			Timestamp:    time.Now(),
			Path:         path,
			ExpectedHash: expected.Hash,
			ActualHash:   "",
			TamperType:   "deleted",
		}

		log.Printf("[Integrity] TAMPER DETECTED: %s was deleted!", filepath.Base(path))

		// Try to recover
		if im.config.OnRecoverFile != nil {
			if err := im.config.OnRecoverFile(path); err != nil {
				report.RecoverError = err.Error()
			} else {
				report.Recovered = true
				log.Printf("[Integrity] Recovered: %s", filepath.Base(path))
			}
		}

		return report
	}

	if err != nil {
		log.Printf("[Integrity] Cannot stat %s: %v", path, err)
		return nil
	}

	// Quick check: size changed
	if info.Size() != expected.Size {
		report := &TamperReport{
			Timestamp:    time.Now(),
			Path:         path,
			ExpectedHash: expected.Hash,
			TamperType:   "size_changed",
		}

		log.Printf("[Integrity] TAMPER DETECTED: %s size changed (%d -> %d)",
			filepath.Base(path), expected.Size, info.Size())

		// Calculate actual hash
		if hash, err := im.hashFile(path); err == nil {
			report.ActualHash = hash
		}

		// Try to recover
		if im.config.OnRecoverFile != nil {
			if err := im.config.OnRecoverFile(path); err != nil {
				report.RecoverError = err.Error()
			} else {
				report.Recovered = true
				log.Printf("[Integrity] Recovered: %s", filepath.Base(path))
			}
		}

		return report
	}

	// Full hash check
	actualHash, err := im.hashFile(path)
	if err != nil {
		log.Printf("[Integrity] Cannot hash %s: %v", path, err)
		return nil
	}

	if actualHash != expected.Hash {
		report := &TamperReport{
			Timestamp:    time.Now(),
			Path:         path,
			ExpectedHash: expected.Hash,
			ActualHash:   actualHash,
			TamperType:   "modified",
		}

		log.Printf("[Integrity] TAMPER DETECTED: %s was modified!", filepath.Base(path))

		// Try to recover
		if im.config.OnRecoverFile != nil {
			if err := im.config.OnRecoverFile(path); err != nil {
				report.RecoverError = err.Error()
			} else {
				report.Recovered = true
				log.Printf("[Integrity] Recovered: %s", filepath.Base(path))
			}
		}

		return report
	}

	return nil
}

// generateFileBaseline creates a baseline for a single file
func (im *IntegrityMonitor) generateFileBaseline(path string) (FileBaseline, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileBaseline{}, err
	}

	hash, err := im.hashFile(path)
	if err != nil {
		return FileBaseline{}, err
	}

	return FileBaseline{
		Path:       path,
		Hash:       hash,
		Size:       info.Size(),
		ModTime:    info.ModTime(),
		RecordedAt: time.Now(),
	}, nil
}

// hashFile calculates SHA256 hash of a file
func (im *IntegrityMonitor) hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// saveBaseline saves the baseline to disk
func (im *IntegrityMonitor) saveBaseline(store *BaselineStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(im.config.BaselinePath, data, 0600)
}

// UpdateFileBaseline updates the baseline for a single file after a legitimate change
func (im *IntegrityMonitor) UpdateFileBaseline(path string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	baseline, err := im.generateFileBaseline(path)
	if err != nil {
		return err
	}

	im.baseline.Files[path] = baseline
	im.baseline.UpdatedAt = time.Now()

	if im.config.BaselinePath != "" {
		return im.saveBaseline(im.baseline)
	}

	log.Printf("[Integrity] Updated baseline for %s", filepath.Base(path))
	return nil
}

// AddProtectedFile adds a new file to the protected list
func (im *IntegrityMonitor) AddProtectedFile(path string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	// Check if already in list
	for _, p := range im.config.ProtectedFiles {
		if p == path {
			return nil
		}
	}

	baseline, err := im.generateFileBaseline(path)
	if err != nil {
		return err
	}

	im.config.ProtectedFiles = append(im.config.ProtectedFiles, path)
	im.baseline.Files[path] = baseline
	im.baseline.UpdatedAt = time.Now()

	if im.config.BaselinePath != "" {
		return im.saveBaseline(im.baseline)
	}

	log.Printf("[Integrity] Added protected file: %s", filepath.Base(path))
	return nil
}

// GetBaseline returns the current baseline (for reporting)
func (im *IntegrityMonitor) GetBaseline() BaselineStore {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if im.baseline == nil {
		return BaselineStore{Files: make(map[string]FileBaseline)}
	}
	return *im.baseline
}

// DefaultProtectedFiles returns the standard list of files to protect
func DefaultProtectedFiles(installDir string) []string {
	return []string{
		filepath.Join(installDir, "sentinel-agent.exe"),
		filepath.Join(installDir, "sentinel-watchdog.exe"),
		filepath.Join(installDir, "config.json"),
		filepath.Join(installDir, "protection.dat"),
	}
}
