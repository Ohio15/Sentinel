package metrics

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultBatchSize     = 100
	defaultFlushInterval = 5 * time.Second
)

// MetricPoint represents a single metrics data point from an agent
type MetricPoint struct {
	DeviceID         uuid.UUID
	Timestamp        time.Time
	CPUPercent       float64
	MemoryPercent    float64
	MemoryUsedBytes  int64
	MemoryTotalBytes int64
	DiskPercent      float64
	DiskUsedBytes    int64
	DiskTotalBytes   int64
	NetworkRxBytes   int64
	NetworkTxBytes   int64
	ProcessCount     int
}

// BulkInserterConfig holds configuration for the bulk inserter
type BulkInserterConfig struct {
	BatchSize     int
	FlushInterval time.Duration
}

// BulkInserter batches metric inserts for improved performance
type BulkInserter struct {
	pool   *pgxpool.Pool
	buffer []MetricPoint
	mu     sync.Mutex
	config BulkInserterConfig

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	insertedCount int64
	failedCount   int64
	lastFlush     time.Time
}

// NewBulkInserter creates a new bulk inserter with the given database pool
func NewBulkInserter(pool *pgxpool.Pool, cfg *BulkInserterConfig) *BulkInserter {
	ctx, cancel := context.WithCancel(context.Background())

	config := BulkInserterConfig{
		BatchSize:     defaultBatchSize,
		FlushInterval: defaultFlushInterval,
	}
	if cfg != nil {
		if cfg.BatchSize > 0 {
			config.BatchSize = cfg.BatchSize
		}
		if cfg.FlushInterval > 0 {
			config.FlushInterval = cfg.FlushInterval
		}
	}

	bi := &BulkInserter{
		pool:   pool,
		buffer: make([]MetricPoint, 0, config.BatchSize*2),
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	bi.wg.Add(1)
	go bi.flushLoop()

	log.Printf("Bulk inserter started (batch size: %d, flush interval: %s)",
		config.BatchSize, config.FlushInterval)

	return bi
}

// Insert adds a metric point to the buffer for batch insertion
func (bi *BulkInserter) Insert(metric MetricPoint) {
	bi.mu.Lock()
	bi.buffer = append(bi.buffer, metric)
	shouldFlush := len(bi.buffer) >= bi.config.BatchSize
	bi.mu.Unlock()

	if shouldFlush {
		go bi.flush()
	}
}

// InsertBatch adds multiple metric points at once
func (bi *BulkInserter) InsertBatch(metrics []MetricPoint) {
	bi.mu.Lock()
	bi.buffer = append(bi.buffer, metrics...)
	shouldFlush := len(bi.buffer) >= bi.config.BatchSize
	bi.mu.Unlock()

	if shouldFlush {
		go bi.flush()
	}
}

// flushLoop periodically flushes the buffer
func (bi *BulkInserter) flushLoop() {
	defer bi.wg.Done()

	ticker := time.NewTicker(bi.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bi.ctx.Done():
			bi.flush() // Final flush on shutdown
			return
		case <-ticker.C:
			bi.flush()
		}
	}
}

// flush writes buffered metrics to the database
func (bi *BulkInserter) flush() {
	bi.mu.Lock()
	if len(bi.buffer) == 0 {
		bi.mu.Unlock()
		return
	}

	// Swap buffer to avoid holding lock during insert
	toFlush := bi.buffer
	bi.buffer = make([]MetricPoint, 0, bi.config.BatchSize*2)
	bi.mu.Unlock()

	start := time.Now()

	// Use COPY for maximum performance
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	count, err := bi.pool.CopyFrom(
		ctx,
		pgx.Identifier{"device_metrics"},
		[]string{
			"device_id", "timestamp", "cpu_percent", "memory_percent",
			"memory_used_bytes", "memory_total_bytes", "disk_percent",
			"disk_used_bytes", "disk_total_bytes", "network_rx_bytes",
			"network_tx_bytes", "process_count",
		},
		pgx.CopyFromSlice(len(toFlush), func(i int) ([]any, error) {
			m := toFlush[i]
			return []any{
				m.DeviceID, m.Timestamp, m.CPUPercent, m.MemoryPercent,
				m.MemoryUsedBytes, m.MemoryTotalBytes, m.DiskPercent,
				m.DiskUsedBytes, m.DiskTotalBytes, m.NetworkRxBytes,
				m.NetworkTxBytes, m.ProcessCount,
			}, nil
		}),
	)

	duration := time.Since(start)
	bi.lastFlush = time.Now()

	if err != nil {
		log.Printf("Bulk insert failed (%d metrics): %v", len(toFlush), err)
		bi.failedCount += int64(len(toFlush))

		// Fallback to individual inserts for partial recovery
		bi.fallbackInsert(toFlush)
		return
	}

	bi.insertedCount += count
	log.Printf("Bulk inserted %d metrics in %v", count, duration)
}

// fallbackInsert attempts individual inserts when bulk insert fails
func (bi *BulkInserter) fallbackInsert(metrics []MetricPoint) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	successCount := 0
	for _, m := range metrics {
		_, err := bi.pool.Exec(ctx, `
			INSERT INTO device_metrics (
				device_id, timestamp, cpu_percent, memory_percent,
				memory_used_bytes, memory_total_bytes, disk_percent,
				disk_used_bytes, disk_total_bytes, network_rx_bytes,
				network_tx_bytes, process_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (device_id, timestamp) DO UPDATE SET
				cpu_percent = EXCLUDED.cpu_percent,
				memory_percent = EXCLUDED.memory_percent
		`,
			m.DeviceID, m.Timestamp, m.CPUPercent, m.MemoryPercent,
			m.MemoryUsedBytes, m.MemoryTotalBytes, m.DiskPercent,
			m.DiskUsedBytes, m.DiskTotalBytes, m.NetworkRxBytes,
			m.NetworkTxBytes, m.ProcessCount,
		)
		if err == nil {
			successCount++
		}
	}

	if successCount > 0 {
		log.Printf("Fallback inserted %d/%d metrics", successCount, len(metrics))
		bi.insertedCount += int64(successCount)
	}
}

// Stats returns current inserter statistics
func (bi *BulkInserter) Stats() map[string]interface{} {
	bi.mu.Lock()
	bufferLen := len(bi.buffer)
	bi.mu.Unlock()

	return map[string]interface{}{
		"buffer_size":     bufferLen,
		"inserted_count":  bi.insertedCount,
		"failed_count":    bi.failedCount,
		"last_flush":      bi.lastFlush,
		"batch_size":      bi.config.BatchSize,
		"flush_interval":  bi.config.FlushInterval.String(),
	}
}

// Flush forces an immediate flush of buffered metrics
func (bi *BulkInserter) Flush() {
	bi.flush()
}

// Close gracefully shuts down the bulk inserter
func (bi *BulkInserter) Close() {
	log.Printf("Shutting down bulk inserter...")
	bi.cancel()
	bi.wg.Wait()
	log.Printf("Bulk inserter shutdown complete (inserted: %d, failed: %d)",
		bi.insertedCount, bi.failedCount)
}
