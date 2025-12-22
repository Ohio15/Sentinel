package inventory

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/security"
)

// CollectionType represents the type of inventory collection
type CollectionType string

const (
	CollectionTypeFull     CollectionType = "full"
	CollectionTypeDelta    CollectionType = "delta"
	CollectionTypeSecurity CollectionType = "security"
	CollectionTypeHardware CollectionType = "hardware"
)

// CollectionResult contains the results of a collection run
type CollectionResult struct {
	Type         CollectionType `json:"type"`
	CollectedAt  time.Time      `json:"collectedAt"`
	Duration     time.Duration  `json:"duration"`
	Software     []Software     `json:"software,omitempty"`
	Services     []Service      `json:"services,omitempty"`
	Security     *security.SecurityPosture `json:"security,omitempty"`
	Changes      []Change       `json:"changes,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// Change represents a detected inventory change
type Change struct {
	Type       string      `json:"type"` // added, removed, modified
	EntityType string      `json:"entityType"` // software, service, user
	EntityName string      `json:"entityName"`
	OldValue   interface{} `json:"oldValue,omitempty"`
	NewValue   interface{} `json:"newValue,omitempty"`
	DetectedAt time.Time   `json:"detectedAt"`
}

// SchedulerConfig holds scheduler configuration
type SchedulerConfig struct {
	FullInventoryInterval   time.Duration
	DeltaScanInterval       time.Duration
	SecurityPostureInterval time.Duration
	HardwareInventoryInterval time.Duration
}

// DefaultSchedulerConfig returns default scheduler intervals
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		FullInventoryInterval:     24 * time.Hour,
		DeltaScanInterval:         15 * time.Minute,
		SecurityPostureInterval:   1 * time.Hour,
		HardwareInventoryInterval: 10 * time.Minute,
	}
}

// ResultHandler is called when collection results are ready
type ResultHandler func(result *CollectionResult)

// Scheduler manages inventory collection schedules
type Scheduler struct {
	config  SchedulerConfig
	handler ResultHandler

	// Collectors
	softwareCollector *SoftwareCollector
	serviceCollector  *ServiceCollector
	securityCollector *security.PostureCollector

	// State tracking for delta detection
	lastSoftware map[string]Software // key: name+version
	lastServices map[string]Service  // key: name
	stateMu      sync.RWMutex

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler creates a new inventory scheduler
func NewScheduler(config SchedulerConfig, handler ResultHandler) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		config:            config,
		handler:           handler,
		softwareCollector: NewSoftwareCollector(),
		serviceCollector:  NewServiceCollector(),
		securityCollector: security.NewPostureCollector(),
		lastSoftware:      make(map[string]Software),
		lastServices:      make(map[string]Service),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// Start begins scheduled collection
func (s *Scheduler) Start() {
	log.Println("Starting inventory scheduler...")

	// Run full inventory on startup
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.collectFull()
	}()

	// Schedule periodic collections
	s.wg.Add(1)
	go s.runSchedule(s.config.FullInventoryInterval, s.collectFull)

	s.wg.Add(1)
	go s.runSchedule(s.config.DeltaScanInterval, s.collectDelta)

	s.wg.Add(1)
	go s.runSchedule(s.config.SecurityPostureInterval, s.collectSecurity)

	log.Printf("Inventory scheduler started (full: %v, delta: %v, security: %v)",
		s.config.FullInventoryInterval,
		s.config.DeltaScanInterval,
		s.config.SecurityPostureInterval)
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	log.Println("Stopping inventory scheduler...")
	s.cancel()
	s.wg.Wait()
	log.Println("Inventory scheduler stopped")
}

// runSchedule runs a collection function on a schedule
func (s *Scheduler) runSchedule(interval time.Duration, collectFunc func()) {
	defer s.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			collectFunc()
		}
	}
}

// collectFull performs a full inventory collection
func (s *Scheduler) collectFull() {
	start := time.Now()
	result := &CollectionResult{
		Type:        CollectionTypeFull,
		CollectedAt: start,
	}

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	// Collect software
	software, err := s.softwareCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting software: %v", err)
		result.Error = err.Error()
	} else {
		result.Software = software
		s.updateSoftwareState(software)
	}

	// Collect services
	services, err := s.serviceCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting services: %v", err)
	} else {
		result.Services = services
		s.updateServiceState(services)
	}

	// Collect security posture
	securityPosture, err := s.securityCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting security posture: %v", err)
	} else {
		result.Security = securityPosture
	}

	result.Duration = time.Since(start)

	log.Printf("Full inventory collected in %v (software: %d, services: %d)",
		result.Duration, len(result.Software), len(result.Services))

	if s.handler != nil {
		s.handler(result)
	}
}

// collectDelta performs delta detection
func (s *Scheduler) collectDelta() {
	start := time.Now()
	result := &CollectionResult{
		Type:        CollectionTypeDelta,
		CollectedAt: start,
	}

	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()

	// Collect current software
	software, err := s.softwareCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting software for delta: %v", err)
	} else {
		changes := s.detectSoftwareChanges(software)
		result.Changes = append(result.Changes, changes...)
		s.updateSoftwareState(software)
	}

	// Collect current services
	services, err := s.serviceCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting services for delta: %v", err)
	} else {
		changes := s.detectServiceChanges(services)
		result.Changes = append(result.Changes, changes...)
		s.updateServiceState(services)
	}

	result.Duration = time.Since(start)

	// Only report if there are changes
	if len(result.Changes) > 0 {
		log.Printf("Delta scan found %d changes in %v", len(result.Changes), result.Duration)
		if s.handler != nil {
			s.handler(result)
		}
	}
}

// collectSecurity collects security posture
func (s *Scheduler) collectSecurity() {
	start := time.Now()
	result := &CollectionResult{
		Type:        CollectionTypeSecurity,
		CollectedAt: start,
	}

	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	securityPosture, err := s.securityCollector.Collect(ctx)
	if err != nil {
		log.Printf("Error collecting security posture: %v", err)
		result.Error = err.Error()
	} else {
		result.Security = securityPosture
	}

	result.Duration = time.Since(start)

	log.Printf("Security posture collected in %v (score: %d)",
		result.Duration, securityPosture.SecurityScore)

	if s.handler != nil {
		s.handler(result)
	}
}

// CollectNow triggers an immediate collection of the specified type
func (s *Scheduler) CollectNow(collectionType CollectionType) {
	switch collectionType {
	case CollectionTypeFull:
		go s.collectFull()
	case CollectionTypeDelta:
		go s.collectDelta()
	case CollectionTypeSecurity:
		go s.collectSecurity()
	}
}

// updateSoftwareState updates the cached software state
func (s *Scheduler) updateSoftwareState(software []Software) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	s.lastSoftware = make(map[string]Software)
	for _, sw := range software {
		key := sw.Name + "|" + sw.Version
		s.lastSoftware[key] = sw
	}
}

// updateServiceState updates the cached service state
func (s *Scheduler) updateServiceState(services []Service) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	s.lastServices = make(map[string]Service)
	for _, svc := range services {
		s.lastServices[svc.Name] = svc
	}
}

// detectSoftwareChanges compares current software to cached state
func (s *Scheduler) detectSoftwareChanges(current []Software) []Change {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	var changes []Change
	now := time.Now()

	currentMap := make(map[string]Software)
	for _, sw := range current {
		key := sw.Name + "|" + sw.Version
		currentMap[key] = sw

		// Check for new software
		if _, exists := s.lastSoftware[key]; !exists {
			// Check if it's a version upgrade
			isUpgrade := false
			for oldKey := range s.lastSoftware {
				if extractName(oldKey) == sw.Name {
					isUpgrade = true
					break
				}
			}

			changeType := "added"
			if isUpgrade {
				changeType = "upgraded"
			}

			changes = append(changes, Change{
				Type:       changeType,
				EntityType: "software",
				EntityName: sw.Name,
				NewValue:   sw,
				DetectedAt: now,
			})
		}
	}

	// Check for removed software
	for key, oldSw := range s.lastSoftware {
		if _, exists := currentMap[key]; !exists {
			// Check if it's just a version change
			isUpgrade := false
			for currentKey := range currentMap {
				if extractName(currentKey) == oldSw.Name {
					isUpgrade = true
					break
				}
			}

			if !isUpgrade {
				changes = append(changes, Change{
					Type:       "removed",
					EntityType: "software",
					EntityName: oldSw.Name,
					OldValue:   oldSw,
					DetectedAt: now,
				})
			}
		}
	}

	return changes
}

// detectServiceChanges compares current services to cached state
func (s *Scheduler) detectServiceChanges(current []Service) []Change {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	var changes []Change
	now := time.Now()

	currentMap := make(map[string]Service)
	for _, svc := range current {
		currentMap[svc.Name] = svc

		// Check for new or modified services
		if oldSvc, exists := s.lastServices[svc.Name]; exists {
			// Check for state change
			if oldSvc.CurrentState != svc.CurrentState {
				changes = append(changes, Change{
					Type:       "state_changed",
					EntityType: "service",
					EntityName: svc.Name,
					OldValue:   oldSvc.CurrentState,
					NewValue:   svc.CurrentState,
					DetectedAt: now,
				})
			}
			// Check for start type change
			if oldSvc.StartType != svc.StartType {
				changes = append(changes, Change{
					Type:       "config_changed",
					EntityType: "service",
					EntityName: svc.Name,
					OldValue:   oldSvc.StartType,
					NewValue:   svc.StartType,
					DetectedAt: now,
				})
			}
		} else {
			changes = append(changes, Change{
				Type:       "added",
				EntityType: "service",
				EntityName: svc.Name,
				NewValue:   svc,
				DetectedAt: now,
			})
		}
	}

	// Check for removed services
	for name, oldSvc := range s.lastServices {
		if _, exists := currentMap[name]; !exists {
			changes = append(changes, Change{
				Type:       "removed",
				EntityType: "service",
				EntityName: name,
				OldValue:   oldSvc,
				DetectedAt: now,
			})
		}
	}

	return changes
}

// GetLastInventory returns the last collected inventory
func (s *Scheduler) GetLastInventory() ([]Software, []Service) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	software := make([]Software, 0, len(s.lastSoftware))
	for _, sw := range s.lastSoftware {
		software = append(software, sw)
	}

	services := make([]Service, 0, len(s.lastServices))
	for _, svc := range s.lastServices {
		services = append(services, svc)
	}

	return software, services
}

// ToJSON converts the collection result to JSON
func (r *CollectionResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

func extractName(key string) string {
	parts := splitOnce(key, "|")
	return parts[0]
}

func splitOnce(s, sep string) []string {
	for i := 0; i < len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return []string{s}
}
