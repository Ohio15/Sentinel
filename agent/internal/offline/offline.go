package offline

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrQueueFull    = errors.New("offline queue is full")
	ErrNotFound     = errors.New("item not found")
	ErrDBClosed     = errors.New("database is closed")
)

// Config holds offline store configuration
type Config struct {
	DBPath          string
	MaxMetricsQueue int           // Max number of metrics to queue (default: 100000)
	MaxEventsQueue  int           // Max number of events to queue (default: 10000)
	MaxQueueAge     time.Duration // Max age before auto-cleanup (default: 7 days)
	CompressPayload bool          // Whether to gzip compress payloads
}

// DefaultConfig returns default configuration
func DefaultConfig(dataDir string) Config {
	return Config{
		DBPath:          filepath.Join(dataDir, "offline.db"),
		MaxMetricsQueue: 100000,
		MaxEventsQueue:  10000,
		MaxQueueAge:     7 * 24 * time.Hour,
		CompressPayload: true,
	}
}

// Store manages offline data persistence
type Store struct {
	db              *sql.DB
	config          Config
	mu              sync.RWMutex
	closed          bool
}

// QueuedMetric represents a cached metric entry
type QueuedMetric struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Payload   []byte    `json:"payload"`
	Priority  int       `json:"priority"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"createdAt"`
}

// QueuedEvent represents a cached event entry
type QueuedEvent struct {
	ID        int64     `json:"id"`
	EventType string    `json:"eventType"`
	Severity  string    `json:"severity"`
	Payload   []byte    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
	Synced    bool      `json:"synced"`
}

// QueuedCommand represents a command that needs response sync
type QueuedCommand struct {
	ID          int64     `json:"id"`
	RequestID   string    `json:"requestId"`
	CommandType string    `json:"commandType"`
	Payload     []byte    `json:"payload"`
	Response    []byte    `json:"response,omitempty"`
	Status      string    `json:"status"` // pending, executed, synced
	ReceivedAt  time.Time `json:"receivedAt"`
	ExecutedAt  time.Time `json:"executedAt,omitempty"`
	SyncedAt    time.Time `json:"syncedAt,omitempty"`
}

// QueueStats provides statistics about the offline queue
type QueueStats struct {
	MetricsCount    int       `json:"metricsCount"`
	EventsCount     int       `json:"eventsCount"`
	CommandsCount   int       `json:"commandsCount"`
	OldestMetric    time.Time `json:"oldestMetric,omitempty"`
	OldestEvent     time.Time `json:"oldestEvent,omitempty"`
	TotalSizeBytes  int64     `json:"totalSizeBytes"`
	LastSync        time.Time `json:"lastSync,omitempty"`
}

// New creates a new offline store
func New(config Config) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open SQLite database
	db, err := sql.Open("sqlite", config.DBPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite works best with single connection
	db.SetMaxIdleConns(1)

	store := &Store{
		db:     db,
		config: config,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the database tables
func (s *Store) initSchema() error {
	schema := `
	-- Metrics queue table
	CREATE TABLE IF NOT EXISTS metrics_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		payload BLOB NOT NULL,
		priority INTEGER DEFAULT 0,
		attempts INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		UNIQUE(timestamp)
	);

	-- Events queue table
	CREATE TABLE IF NOT EXISTS event_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT NOT NULL,
		severity TEXT NOT NULL,
		payload BLOB NOT NULL,
		timestamp INTEGER NOT NULL,
		synced INTEGER DEFAULT 0
	);

	-- Commands queue table
	CREATE TABLE IF NOT EXISTS command_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_id TEXT NOT NULL UNIQUE,
		command_type TEXT NOT NULL,
		payload BLOB NOT NULL,
		response BLOB,
		status TEXT DEFAULT 'pending',
		received_at INTEGER NOT NULL,
		executed_at INTEGER,
		synced_at INTEGER
	);

	-- Sync state table
	CREATE TABLE IF NOT EXISTS sync_state (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at INTEGER
	);

	-- Indexes
	CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics_queue(timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_priority ON metrics_queue(priority DESC, created_at);
	CREATE INDEX IF NOT EXISTS idx_events_synced ON event_queue(synced, timestamp);
	CREATE INDEX IF NOT EXISTS idx_commands_status ON command_queue(status);
	`

	_, err := s.db.Exec(schema)
	return err
}

// QueueAnyMetrics adds any marshallable metrics to the offline queue
func (s *Store) QueueAnyMetrics(ctx context.Context, metrics interface{}, timestamp time.Time, priority int) error {
	payload, err := s.encodePayload(metrics)
	if err != nil {
		return err
	}
	return s.QueueMetricsWithContext(ctx, payload, timestamp, priority)
}

// QueueAnyEvent adds an event with any marshallable payload to the offline queue
func (s *Store) QueueAnyEvent(ctx context.Context, eventType, severity string, payload interface{}) error {
	payloadBytes, err := s.encodePayload(payload)
	if err != nil {
		return err
	}
	return s.QueueEventWithContext(ctx, eventType, severity, payloadBytes)
}

// QueueCommand stores a command for later sync
func (s *Store) QueueCommand(ctx context.Context, requestID, cmdType string, payload interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	payloadBytes, err := s.encodePayload(payload)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO command_queue (request_id, command_type, payload, status, received_at) VALUES (?, ?, ?, 'pending', ?)",
		requestID, cmdType, payloadBytes, time.Now().UnixMilli())

	return err
}

// SetCommandResponse updates a command with its execution response
func (s *Store) SetCommandResponse(ctx context.Context, requestID string, response interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	responseBytes, err := s.encodePayload(response)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		"UPDATE command_queue SET response = ?, status = 'executed', executed_at = ? WHERE request_id = ?",
		responseBytes, time.Now().UnixMilli(), requestID)

	return err
}

// Note: GetPendingMetrics convenience method is defined later in the file

// GetPendingEvents retrieves events that need to be synced
func (s *Store) GetPendingEvents(ctx context.Context, limit int) ([]QueuedEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDBClosed
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, event_type, severity, payload, timestamp, synced FROM event_queue WHERE synced = 0 ORDER BY timestamp LIMIT ?",
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []QueuedEvent
	for rows.Next() {
		var e QueuedEvent
		var ts int64
		var synced int
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.Payload, &ts, &synced); err != nil {
			return nil, err
		}
		e.Timestamp = time.UnixMilli(ts)
		e.Synced = synced == 1
		e.Payload, _ = s.decodePayload(e.Payload)
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetPendingCommands retrieves executed commands that need response sync
func (s *Store) GetPendingCommands(ctx context.Context, limit int) ([]QueuedCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDBClosed
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, request_id, command_type, payload, response, status, received_at, executed_at FROM command_queue WHERE status = 'executed' ORDER BY executed_at LIMIT ?",
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commands []QueuedCommand
	for rows.Next() {
		var c QueuedCommand
		var receivedAt, executedAt sql.NullInt64
		if err := rows.Scan(&c.ID, &c.RequestID, &c.CommandType, &c.Payload, &c.Response, &c.Status, &receivedAt, &executedAt); err != nil {
			return nil, err
		}
		if receivedAt.Valid {
			c.ReceivedAt = time.UnixMilli(receivedAt.Int64)
		}
		if executedAt.Valid {
			c.ExecutedAt = time.UnixMilli(executedAt.Int64)
		}
		c.Payload, _ = s.decodePayload(c.Payload)
		c.Response, _ = s.decodePayload(c.Response)
		commands = append(commands, c)
	}

	return commands, rows.Err()
}

// Note: MarkMetricsSynced convenience method is defined later in the file

// MarkEventsSynced marks events as synced
func (s *Store) MarkEventsSynced(ctx context.Context, ids []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	if len(ids) == 0 {
		return nil
	}

	query := "UPDATE event_queue SET synced = 1 WHERE id IN (?" + repeatString(",?", len(ids)-1) + ")"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// MarkCommandsSynced marks commands as synced
func (s *Store) MarkCommandsSynced(ctx context.Context, requestIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	if len(requestIDs) == 0 {
		return nil
	}

	query := "UPDATE command_queue SET status = 'synced', synced_at = ? WHERE request_id IN (?" + repeatString(",?", len(requestIDs)-1) + ")"
	args := make([]interface{}, len(requestIDs)+1)
	args[0] = time.Now().UnixMilli()
	for i, id := range requestIDs {
		args[i+1] = id
	}

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// Prune removes old data from the queue
func (s *Store) Prune(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	cutoff := time.Now().Add(-s.config.MaxQueueAge).UnixMilli()

	// Remove old metrics
	_, err := s.db.ExecContext(ctx, "DELETE FROM metrics_queue WHERE created_at < ?", cutoff)
	if err != nil {
		return err
	}

	// Remove old synced events
	_, err = s.db.ExecContext(ctx, "DELETE FROM event_queue WHERE synced = 1 AND timestamp < ?", cutoff)
	if err != nil {
		return err
	}

	// Remove old synced commands
	_, err = s.db.ExecContext(ctx, "DELETE FROM command_queue WHERE status = 'synced' AND synced_at < ?", cutoff)
	if err != nil {
		return err
	}

	// Vacuum to reclaim space
	_, err = s.db.ExecContext(ctx, "VACUUM")
	return err
}

// GetStats returns statistics about the offline queue
func (s *Store) GetStats(ctx context.Context) (QueueStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats QueueStats

	if s.closed {
		return stats, ErrDBClosed
	}

	// Get counts
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics_queue").Scan(&stats.MetricsCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_queue WHERE synced = 0").Scan(&stats.EventsCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM command_queue WHERE status != 'synced'").Scan(&stats.CommandsCount)

	// Get oldest timestamps
	var oldestMetric, oldestEvent sql.NullInt64
	s.db.QueryRowContext(ctx, "SELECT MIN(timestamp) FROM metrics_queue").Scan(&oldestMetric)
	s.db.QueryRowContext(ctx, "SELECT MIN(timestamp) FROM event_queue WHERE synced = 0").Scan(&oldestEvent)

	if oldestMetric.Valid {
		stats.OldestMetric = time.UnixMilli(oldestMetric.Int64)
	}
	if oldestEvent.Valid {
		stats.OldestEvent = time.UnixMilli(oldestEvent.Int64)
	}

	// Get last sync time
	var lastSync sql.NullString
	s.db.QueryRowContext(ctx, "SELECT value FROM sync_state WHERE key = 'last_sync'").Scan(&lastSync)
	if lastSync.Valid {
		if ts, err := time.Parse(time.RFC3339, lastSync.String); err == nil {
			stats.LastSync = ts
		}
	}

	// Get database file size
	if info, err := os.Stat(s.config.DBPath); err == nil {
		stats.TotalSizeBytes = info.Size()
	}

	return stats, nil
}

// SetLastSync updates the last sync timestamp
func (s *Store) SetLastSync(ctx context.Context, timestamp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO sync_state (key, value, updated_at) VALUES ('last_sync', ?, ?)",
		timestamp.Format(time.RFC3339), time.Now().UnixMilli())
	return err
}

// GetSyncState retrieves a sync state value
func (s *Store) GetSyncState(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return "", ErrDBClosed
	}

	var value sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT value FROM sync_state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value.String, nil
}

// SetSyncState sets a sync state value
func (s *Store) SetSyncState(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO sync_state (key, value, updated_at) VALUES (?, ?, ?)",
		key, value, time.Now().UnixMilli())
	return err
}

// Close closes the database connection
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	return s.db.Close()
}

// Priority constants for queueing
const (
	PriorityLow    = 0
	PriorityNormal = 50
	PriorityHigh   = 100
)

// GetMetricsCount returns the number of cached metrics
func (s *Store) GetMetricsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM metrics_queue").Scan(&count)
	return count
}

// GetEventsCount returns the number of unsynced events
func (s *Store) GetEventsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE synced = 0").Scan(&count)
	return count
}

// GetPendingCommandsCount returns the number of pending commands
func (s *Store) GetPendingCommandsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM command_queue WHERE status != 'synced'").Scan(&count)
	return count
}

// QueueMetrics is a convenience method that adds metrics with a background context
func (s *Store) QueueMetrics(payload []byte, priority int) error {
	return s.QueueMetricsWithContext(context.Background(), payload, time.Now(), priority)
}

// QueueMetricsWithContext adds metrics to the queue (renamed from original QueueMetrics)
func (s *Store) QueueMetricsWithContext(ctx context.Context, payload []byte, timestamp time.Time, priority int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	// Check queue size
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics_queue").Scan(&count)
	if err != nil {
		return err
	}
	if count >= s.config.MaxMetricsQueue {
		// Remove oldest entries to make room
		_, err = s.db.ExecContext(ctx,
			"DELETE FROM metrics_queue WHERE id IN (SELECT id FROM metrics_queue ORDER BY timestamp LIMIT ?)",
			count-s.config.MaxMetricsQueue+1)
		if err != nil {
			return err
		}
	}

	// Optionally compress payload
	if s.config.CompressPayload {
		payload, _ = s.compressPayload(payload)
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO metrics_queue (timestamp, payload, priority, attempts, created_at) VALUES (?, ?, ?, 0, ?)",
		timestamp.UnixMilli(), payload, priority, time.Now().UnixMilli())

	return err
}

// QueueEvent is a convenience method that adds an event with a background context
func (s *Store) QueueEvent(eventType, severity string, payload []byte) error {
	return s.QueueEventWithContext(context.Background(), eventType, severity, payload)
}

// QueueEventWithContext adds an event to the queue
func (s *Store) QueueEventWithContext(ctx context.Context, eventType, severity string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	// Check queue size
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_queue WHERE synced = 0").Scan(&count)
	if err != nil {
		return err
	}
	if count >= s.config.MaxEventsQueue {
		// Remove oldest synced entries first
		s.db.ExecContext(ctx, "DELETE FROM event_queue WHERE synced = 1")
		var newCount int
		s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_queue WHERE synced = 0").Scan(&newCount)
		if newCount >= s.config.MaxEventsQueue {
			return ErrQueueFull
		}
	}

	// Optionally compress
	if s.config.CompressPayload {
		payload, _ = s.compressPayload(payload)
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO event_queue (event_type, severity, payload, timestamp, synced) VALUES (?, ?, ?, ?, 0)",
		eventType, severity, payload, time.Now().UnixMilli())

	return err
}

// GetPendingMetrics is a convenience method with background context
func (s *Store) GetPendingMetrics(limit int) ([]QueuedMetric, error) {
	return s.GetPendingMetricsWithContext(context.Background(), limit)
}

// GetPendingMetricsWithContext retrieves metrics that need to be synced
func (s *Store) GetPendingMetricsWithContext(ctx context.Context, limit int) ([]QueuedMetric, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDBClosed
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, timestamp, payload, priority, attempts, created_at FROM metrics_queue ORDER BY priority DESC, timestamp LIMIT ?",
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []QueuedMetric
	for rows.Next() {
		var m QueuedMetric
		var ts, createdAt int64
		if err := rows.Scan(&m.ID, &ts, &m.Payload, &m.Priority, &m.Attempts, &createdAt); err != nil {
			return nil, err
		}
		m.Timestamp = time.UnixMilli(ts)
		m.CreatedAt = time.UnixMilli(createdAt)

		// Decompress if needed
		m.Payload, _ = s.decodePayload(m.Payload)
		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}

// MarkMetricsSynced is a convenience method with background context for single ID
func (s *Store) MarkMetricsSynced(id int64) error {
	return s.MarkMetricsSyncedBatch(context.Background(), []int64{id})
}

// MarkMetricsSyncedBatch marks metrics as synced and removes them
func (s *Store) MarkMetricsSyncedBatch(ctx context.Context, ids []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDBClosed
	}

	if len(ids) == 0 {
		return nil
	}

	// Build query with placeholders
	query := "DELETE FROM metrics_queue WHERE id IN (?" + repeatString(",?", len(ids)-1) + ")"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// compressPayload compresses data with gzip
func (s *Store) compressPayload(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return data, err
	}
	if err := gw.Close(); err != nil {
		return data, err
	}
	return buf.Bytes(), nil
}

// encodePayload marshals and optionally compresses data
func (s *Store) encodePayload(data interface{}) ([]byte, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	if !s.config.CompressPayload {
		return jsonBytes, nil
	}

	// Compress with gzip
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(jsonBytes); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decodePayload decompresses (if needed) and returns the raw bytes
func (s *Store) decodePayload(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	// Check for gzip magic number
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return data, nil // Not actually gzipped
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}

	return data, nil
}

func repeatString(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
