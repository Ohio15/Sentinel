package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/sentinel/server/internal/grpc/dataplane"
	"github.com/sentinel/server/internal/metrics"
	"github.com/sentinel/server/pkg/database"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
)

// DataPlaneServer implements the gRPC DataPlaneService
type DataPlaneServer struct {
	pb.UnimplementedDataPlaneServiceServer
	db              *database.DB
	bulkInserter    *metrics.BulkInserter
	activeStreams   map[string]time.Time
	streamsMu       sync.RWMutex
	onMetrics       func(agentID string, m *pb.Metrics)
	onInventory     func(agentID string, inv *pb.InventoryData)
}

// ServerConfig holds configuration for the gRPC server
type ServerConfig struct {
	Port        int
	TLSCertFile string
	TLSKeyFile  string
	CACertFile  string
	UseTLS      bool
}

// NewDataPlaneServer creates a new DataPlane gRPC server
func NewDataPlaneServer(db *database.DB, bulkInserter *metrics.BulkInserter) *DataPlaneServer {
	return &DataPlaneServer{
		db:            db,
		bulkInserter:  bulkInserter,
		activeStreams: make(map[string]time.Time),
	}
}

// SetMetricsCallback sets a callback for when metrics are received
func (s *DataPlaneServer) SetMetricsCallback(cb func(agentID string, m *pb.Metrics)) {
	s.onMetrics = cb
}

// SetInventoryCallback sets a callback for when inventory is received
func (s *DataPlaneServer) SetInventoryCallback(cb func(agentID string, inv *pb.InventoryData)) {
	s.onInventory = cb
}

// StreamMetrics handles streaming metrics from agents
func (s *DataPlaneServer) StreamMetrics(stream pb.DataPlaneService_StreamMetricsServer) error {
	var agentID string
	var metricsCount int64

	// Get peer info for logging
	p, ok := peer.FromContext(stream.Context())
	peerAddr := "unknown"
	if ok {
		peerAddr = p.Addr.String()
	}

	defer func() {
		if agentID != "" {
			s.streamsMu.Lock()
			delete(s.activeStreams, agentID)
			s.streamsMu.Unlock()
			log.Printf("[gRPC] Metrics stream ended for agent %s (received %d metrics)", agentID, metricsCount)
		}
	}()

	for {
		m, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.StreamResponse{
				Success: true,
			})
		}
		if err != nil {
			if agentID != "" {
				log.Printf("[gRPC] Stream error for agent %s: %v", agentID, err)
			}
			return err
		}

		// Track agent ID from first message
		if agentID == "" && m.AgentId != "" {
			agentID = m.AgentId
			log.Printf("[gRPC] Metrics stream started from agent %s (peer: %s)", agentID, peerAddr)
			s.streamsMu.Lock()
			s.activeStreams[agentID] = time.Now()
			s.streamsMu.Unlock()
		}

		metricsCount++

		// Update stream activity
		s.streamsMu.Lock()
		s.activeStreams[agentID] = time.Now()
		s.streamsMu.Unlock()

		// Process metrics
		if err := s.processMetrics(stream.Context(), m); err != nil {
			log.Printf("[gRPC] Error processing metrics from %s: %v", agentID, err)
			// Don't fail the stream for processing errors
		}

		// Callback if set
		if s.onMetrics != nil {
			s.onMetrics(agentID, m)
		}
	}
}

// processMetrics stores metrics in the database
func (s *DataPlaneServer) processMetrics(ctx context.Context, m *pb.Metrics) error {
	if m.AgentId == "" {
		return fmt.Errorf("missing agent_id in metrics")
	}

	// Get device ID from agent ID
	var deviceID uuid.UUID
	err := s.db.Pool().QueryRow(ctx,
		"SELECT id FROM devices WHERE agent_id = $1",
		m.AgentId,
	).Scan(&deviceID)
	if err != nil {
		return fmt.Errorf("failed to find device for agent %s: %w", m.AgentId, err)
	}

	// Use bulk inserter if available
	if s.bulkInserter != nil {
		s.bulkInserter.Insert(metrics.MetricPoint{
			DeviceID:         deviceID,
			Timestamp:        time.Unix(m.Timestamp, 0),
			CPUPercent:       m.CpuPercent,
			MemoryPercent:    m.MemoryPercent,
			MemoryUsedBytes:  int64(m.MemoryUsed),
			MemoryTotalBytes: int64(m.MemoryUsed + m.MemoryAvailable),
			DiskPercent:      m.DiskPercent,
			DiskUsedBytes:    int64(m.DiskUsed),
			DiskTotalBytes:   int64(m.DiskTotal),
			NetworkRxBytes:   int64(m.NetworkRxBytes),
			NetworkTxBytes:   int64(m.NetworkTxBytes),
			ProcessCount:     int(m.ProcessCount),
		})
	}

	// Update device last_seen
	_, err = s.db.Pool().Exec(ctx,
		"UPDATE devices SET last_seen = NOW() WHERE id = $1",
		deviceID,
	)

	return err
}

// UploadInventory handles inventory uploads from agents
func (s *DataPlaneServer) UploadInventory(ctx context.Context, inv *pb.InventoryData) (*pb.StreamResponse, error) {
	if inv.AgentId == "" {
		return &pb.StreamResponse{Success: false, Error: "missing agent_id"}, nil
	}

	log.Printf("[gRPC] Received inventory from agent %s", inv.AgentId)

	// Get device ID
	var deviceID uuid.UUID
	err := s.db.Pool().QueryRow(ctx,
		"SELECT id FROM devices WHERE agent_id = $1",
		inv.AgentId,
	).Scan(&deviceID)
	if err != nil {
		return &pb.StreamResponse{Success: false, Error: "device not found"}, nil
	}

	// Update device info
	if inv.SystemInfo != nil {
		si := inv.SystemInfo
		_, err = s.db.Pool().Exec(ctx, `
			UPDATE devices SET
				hostname = COALESCE(NULLIF($2, ''), hostname),
				os_type = COALESCE(NULLIF($3, ''), os_type),
				os_version = COALESCE(NULLIF($4, ''), os_version),
				architecture = COALESCE(NULLIF($5, ''), architecture),
				cpu_model = COALESCE(NULLIF($6, ''), cpu_model),
				cpu_cores = COALESCE(NULLIF($7, 0), cpu_cores),
				total_memory = COALESCE(NULLIF($8, 0), total_memory),
				serial_number = COALESCE(NULLIF($9, ''), serial_number),
				manufacturer = COALESCE(NULLIF($10, ''), manufacturer),
				model = COALESCE(NULLIF($11, ''), model),
				last_seen = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, deviceID,
			si.Hostname, si.Os, si.OsVersion, si.Architecture,
			si.CpuModel, si.CpuCores, si.TotalMemory,
			si.SerialNumber, si.Manufacturer, si.Model,
		)
		if err != nil {
			log.Printf("[gRPC] Failed to update device info: %v", err)
		}
	}

	// Callback if set
	if s.onInventory != nil {
		s.onInventory(inv.AgentId, inv)
	}

	return &pb.StreamResponse{Success: true}, nil
}

// StreamLogs handles log streaming from agents
func (s *DataPlaneServer) StreamLogs(stream pb.DataPlaneService_StreamLogsServer) error {
	var agentID string
	var logCount int64

	defer func() {
		if agentID != "" {
			log.Printf("[gRPC] Log stream ended for agent %s (received %d batches)", agentID, logCount)
		}
	}()

	for {
		batch, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.StreamResponse{Success: true})
		}
		if err != nil {
			return err
		}

		if agentID == "" && batch.AgentId != "" {
			agentID = batch.AgentId
			log.Printf("[gRPC] Log stream started from agent %s", agentID)
		}

		logCount++

		// Process logs (store or forward as needed)
		for _, entry := range batch.Entries {
			log.Printf("[AgentLog][%s][%s] %s: %s",
				entry.AgentId, entry.Level, entry.Source, entry.Message)
		}
	}
}

// StreamFileContent handles file content streaming from agents
func (s *DataPlaneServer) StreamFileContent(stream pb.DataPlaneService_StreamFileContentServer) error {
	var agentID, requestID, filePath string
	var totalSize int64
	var receivedBytes int64

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[gRPC] File transfer complete: %s from agent %s (%d bytes)",
				filePath, agentID, receivedBytes)
			return stream.SendAndClose(&pb.StreamResponse{Success: true})
		}
		if err != nil {
			return err
		}

		if agentID == "" {
			agentID = chunk.AgentId
			requestID = chunk.RequestId
			filePath = chunk.FilePath
			totalSize = chunk.TotalSize
			log.Printf("[gRPC] File transfer started: %s from agent %s (size: %d)",
				filePath, agentID, totalSize)
		}

		receivedBytes += int64(len(chunk.Data))

		// TODO: Forward chunks to requesting client or store temporarily
		_ = requestID // Will be used for routing
	}
}

// UploadBulkData handles bulk data uploads from agents
func (s *DataPlaneServer) UploadBulkData(stream pb.DataPlaneService_UploadBulkDataServer) error {
	var agentID, requestID, dataType string
	var totalChunks int32
	var receivedChunks int32

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[gRPC] Bulk data upload complete: %s from agent %s (%d chunks)",
				dataType, agentID, receivedChunks)
			return stream.SendAndClose(&pb.StreamResponse{Success: true})
		}
		if err != nil {
			return err
		}

		if agentID == "" {
			agentID = chunk.AgentId
			requestID = chunk.RequestId
			dataType = chunk.DataType
			totalChunks = chunk.TotalChunks
			log.Printf("[gRPC] Bulk data upload started: %s from agent %s (%d chunks)",
				dataType, agentID, totalChunks)
		}

		receivedChunks++
		_ = requestID // Will be used for routing
	}
}

// GetActiveStreams returns the list of agents with active metrics streams
func (s *DataPlaneServer) GetActiveStreams() map[string]time.Time {
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()

	result := make(map[string]time.Time, len(s.activeStreams))
	for k, v := range s.activeStreams {
		result[k] = v
	}
	return result
}

// StartServer starts the gRPC server
func StartServer(config ServerConfig, server *DataPlaneServer) (*grpc.Server, net.Listener, error) {
	// Server options
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(64 * 1024 * 1024), // 64MB max message
		grpc.MaxSendMsgSize(64 * 1024 * 1024),
	}

	// Configure TLS if enabled
	if config.UseTLS && config.TLSCertFile != "" && config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load TLS certificates: %w", err)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}

		// Add CA cert for client verification if provided
		if config.CACertFile != "" {
			caCert, err := os.ReadFile(config.CACertFile)
			if err != nil {
				log.Printf("[gRPC] Warning: Could not read CA cert: %v", err)
			} else {
				caCertPool := x509.NewCertPool()
				if caCertPool.AppendCertsFromPEM(caCert) {
					tlsConfig.ClientCAs = caCertPool
					tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
					log.Printf("[gRPC] Client certificate verification enabled")
				}
			}
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		log.Printf("[gRPC] TLS enabled with cert: %s", config.TLSCertFile)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterDataPlaneServiceServer(grpcServer, server)

	// Start listening
	addr := fmt.Sprintf(":%d", config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	log.Printf("[gRPC] Data Plane server listening on port %d", config.Port)

	return grpcServer, listener, nil
}
