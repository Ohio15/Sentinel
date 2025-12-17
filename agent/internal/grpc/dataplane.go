package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/sentinel/agent/internal/grpc/dataplane"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// DataPlaneClient manages the gRPC connection to the Data Plane server
type DataPlaneClient struct {
	agentID       string
	serverAddress string
	conn          *grpc.ClientConn
	client        pb.DataPlaneServiceClient
	connected     bool
	mu            sync.RWMutex
	stopCh        chan struct{}
	useTLS        bool
	caCertPath    string

	// Metrics streaming
	metricsStream pb.DataPlaneService_StreamMetricsClient
	streamMu      sync.Mutex
}

// NewDataPlaneClient creates a new Data Plane gRPC client
func NewDataPlaneClient(agentID, serverAddress string) *DataPlaneClient {
	return NewDataPlaneClientWithTLS(agentID, serverAddress, true, "")
}

// NewDataPlaneClientWithTLS creates a new Data Plane gRPC client with TLS configuration
func NewDataPlaneClientWithTLS(agentID, serverAddress string, useTLS bool, caCertPath string) *DataPlaneClient {
	// If no CA cert path provided, try to find it in default locations
	if useTLS && caCertPath == "" {
		caCertPath = findCACertificate()
	}

	return &DataPlaneClient{
		agentID:       agentID,
		serverAddress: serverAddress,
		stopCh:        make(chan struct{}),
		useTLS:        useTLS,
		caCertPath:    caCertPath,
	}
}

// findCACertificate looks for CA certificate in common locations
func findCACertificate() string {
	// Get executable directory
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("[gRPC] Failed to get executable path: %v", err)
		return ""
	}
	execDir := filepath.Dir(execPath)

	// Try different possible locations
	possiblePaths := []string{
		filepath.Join(execDir, "ca-cert.pem"),
		filepath.Join(execDir, "certs", "ca-cert.pem"),
		"/etc/sentinel/certs/ca-cert.pem",           // Linux
		"C:\\ProgramData\\Sentinel\\certs\\ca-cert.pem", // Windows
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			log.Printf("[gRPC] Found CA certificate at: %s", path)
			return path
		}
	}

	log.Printf("[gRPC] CA certificate not found in standard locations")
	return ""
}

// Connect establishes a connection to the gRPC server
func (c *DataPlaneClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	log.Printf("[gRPC] Connecting to Data Plane at %s...", c.serverAddress)

	// Configure keepalive
	kacp := keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             3 * time.Second,
		PermitWithoutStream: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create transport credentials
	creds, err := c.createTransportCredentials()
	if err != nil {
		return fmt.Errorf("failed to create credentials: %w", err)
	}

	// Dial with options
	conn, err := grpc.DialContext(
		ctx,
		c.serverAddress,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	c.conn = conn
	c.client = pb.NewDataPlaneServiceClient(conn)
	c.connected = true

	securityMode := "insecure"
	if c.useTLS {
		securityMode = "TLS"
	}
	log.Printf("[gRPC] Connected to Data Plane at %s (%s)", c.serverAddress, securityMode)

	return nil
}

// createTransportCredentials creates the appropriate transport credentials
func (c *DataPlaneClient) createTransportCredentials() (credentials.TransportCredentials, error) {
	if !c.useTLS {
		log.Printf("[gRPC] WARNING: Using insecure connection (no TLS)")
		return insecure.NewCredentials(), nil
	}

	// Load CA certificate
	if c.caCertPath == "" {
		log.Printf("[gRPC] WARNING: No CA certificate found, using insecure connection")
		return insecure.NewCredentials(), nil
	}

	// Read CA certificate
	caCert, err := os.ReadFile(c.caCertPath)
	if err != nil {
		log.Printf("[gRPC] ERROR: Failed to read CA certificate from %s: %v", c.caCertPath, err)
		log.Printf("[gRPC] Falling back to insecure connection")
		return insecure.NewCredentials(), nil
	}

	// Create certificate pool
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		log.Printf("[gRPC] ERROR: Failed to parse CA certificate")
		log.Printf("[gRPC] Falling back to insecure connection")
		return insecure.NewCredentials(), nil
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
		// For self-signed certificates in development, you might need to skip verification
		// In production, remove this line and use proper certificates
		// InsecureSkipVerify: false,
	}

	// Create and return credentials
	creds := credentials.NewTLS(tlsConfig)
	log.Printf("[gRPC] TLS enabled with CA certificate: %s", c.caCertPath)

	return creds, nil
}

// Disconnect closes the gRPC connection
func (c *DataPlaneClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close metrics stream if open
	c.streamMu.Lock()
	if c.metricsStream != nil {
		c.metricsStream.CloseAndRecv()
		c.metricsStream = nil
	}
	c.streamMu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.client = nil
	c.connected = false

	// Signal stop
	select {
	case <-c.stopCh:
		// Already closed
	default:
		close(c.stopCh)
	}

	log.Printf("[gRPC] Disconnected from Data Plane")
}

// IsConnected returns whether the client is connected
func (c *DataPlaneClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetClient returns the gRPC client (for direct access if needed)
func (c *DataPlaneClient) GetClient() pb.DataPlaneServiceClient {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// SendMetrics sends a single metrics message via the streaming RPC
func (c *DataPlaneClient) SendMetrics(ctx context.Context, metrics *Metrics) error {
	c.mu.RLock()
	connected := c.connected
	client := c.client
	c.mu.RUnlock()

	if !connected || client == nil {
		return fmt.Errorf("not connected to gRPC server")
	}

	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	// Create stream if not exists
	if c.metricsStream == nil {
		stream, err := client.StreamMetrics(ctx)
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			return fmt.Errorf("failed to create metrics stream: %w", err)
		}
		c.metricsStream = stream
		log.Printf("[gRPC] Metrics stream established")
	}

	// Convert to protobuf message
	pbMetrics := &pb.Metrics{
		AgentId:         metrics.AgentID,
		Timestamp:       metrics.Timestamp,
		CpuPercent:      metrics.CPUPercent,
		MemoryPercent:   metrics.MemoryPercent,
		MemoryUsed:      metrics.MemoryUsed,
		MemoryAvailable: metrics.MemoryAvailable,
		DiskPercent:     metrics.DiskPercent,
		DiskUsed:        metrics.DiskUsed,
		DiskTotal:       metrics.DiskTotal,
		NetworkRxBytes:  metrics.NetworkRxBytes,
		NetworkTxBytes:  metrics.NetworkTxBytes,
		ProcessCount:    metrics.ProcessCount,
		Uptime:          metrics.Uptime,
	}

	// Send metrics
	if err := c.metricsStream.Send(pbMetrics); err != nil {
		// Stream broken, reset it
		c.metricsStream = nil
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
		return fmt.Errorf("failed to send metrics: %w", err)
	}

	return nil
}

// RunWithReconnect runs the gRPC client with automatic reconnection
func (c *DataPlaneClient) RunWithReconnect(ctx context.Context) {
	reconnectDelay := time.Second
	maxDelay := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			c.Disconnect()
			return
		case <-c.stopCh:
			return
		default:
		}

		if !c.IsConnected() {
			err := c.Connect()
			if err != nil {
				log.Printf("[gRPC] Connection failed: %v, retrying in %v", err, reconnectDelay)

				select {
				case <-ctx.Done():
					c.Disconnect()
					return
				case <-c.stopCh:
					return
				case <-time.After(reconnectDelay):
				}

				// Exponential backoff
				reconnectDelay *= 2
				if reconnectDelay > maxDelay {
					reconnectDelay = maxDelay
				}
				continue
			}

			// Reset delay on successful connection
			reconnectDelay = time.Second
		}

		// Health check interval
		select {
		case <-ctx.Done():
			c.Disconnect()
			return
		case <-c.stopCh:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// UploadInventory sends inventory data to the server
func (c *DataPlaneClient) UploadInventory(ctx context.Context, inventory *InventoryData) error {
	c.mu.RLock()
	connected := c.connected
	client := c.client
	c.mu.RUnlock()

	if !connected || client == nil {
		return fmt.Errorf("not connected to gRPC server")
	}

	// Convert to protobuf
	pbInventory := &pb.InventoryData{
		AgentId:   inventory.AgentID,
		Timestamp: inventory.Timestamp,
	}

	if inventory.SystemInfo != nil {
		pbInventory.SystemInfo = &pb.SystemInfo{
			Hostname:     inventory.SystemInfo.Hostname,
			Os:           inventory.SystemInfo.OS,
			OsVersion:    inventory.SystemInfo.OSVersion,
			Platform:     inventory.SystemInfo.Platform,
			Architecture: inventory.SystemInfo.Architecture,
			CpuModel:     inventory.SystemInfo.CPUModel,
			CpuCores:     inventory.SystemInfo.CPUCores,
			CpuThreads:   inventory.SystemInfo.CPUThreads,
			CpuSpeed:     inventory.SystemInfo.CPUSpeed,
			TotalMemory:  inventory.SystemInfo.TotalMemory,
			SerialNumber: inventory.SystemInfo.SerialNumber,
			Manufacturer: inventory.SystemInfo.Manufacturer,
			Model:        inventory.SystemInfo.Model,
		}
	}

	for _, sw := range inventory.Software {
		pbInventory.Software = append(pbInventory.Software, &pb.InstalledSoftware{
			Name:        sw.Name,
			Version:     sw.Version,
			Publisher:   sw.Publisher,
			InstallDate: sw.InstallDate,
		})
	}

	resp, err := client.UploadInventory(ctx, pbInventory)
	if err != nil {
		return fmt.Errorf("failed to upload inventory: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server rejected inventory: %s", resp.Error)
	}

	log.Printf("[gRPC] Inventory uploaded successfully")
	return nil
}

// Stop signals the client to stop
func (c *DataPlaneClient) Stop() {
	select {
	case <-c.stopCh:
		// Already closed
	default:
		close(c.stopCh)
	}
}

// GetServerAddress returns the configured server address
func (c *DataPlaneClient) GetServerAddress() string {
	return c.serverAddress
}

// SetServerAddress updates the server address
func (c *DataPlaneClient) SetServerAddress(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serverAddress = address
}

// Metrics represents system metrics sent to the server
// Kept for compatibility with existing code
type Metrics struct {
	AgentID         string
	Timestamp       int64
	CPUPercent      float64
	MemoryPercent   float64
	MemoryUsed      uint64
	MemoryAvailable uint64
	DiskPercent     float64
	DiskUsed        uint64
	DiskTotal       uint64
	NetworkRxBytes  uint64
	NetworkTxBytes  uint64
	ProcessCount    int32
	Uptime          uint64
}

// SystemInfo represents detailed system information
type SystemInfo struct {
	Hostname     string
	OS           string
	OSVersion    string
	Platform     string
	Architecture string
	CPUModel     string
	CPUCores     int32
	CPUThreads   int32
	CPUSpeed     float64
	TotalMemory  uint64
	SerialNumber string
	Manufacturer string
	Model        string
}

// InstalledSoftware represents installed software
type InstalledSoftware struct {
	Name        string
	Version     string
	Publisher   string
	InstallDate string
}

// InventoryData represents inventory data sent to the server
type InventoryData struct {
	AgentID    string
	Timestamp  int64
	SystemInfo *SystemInfo
	Software   []InstalledSoftware
}
