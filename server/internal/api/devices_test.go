package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

// Mock WebSocket Hub
type mockHub struct {
	online map[string]bool
}

func (m *mockHub) IsAgentOnline(agentID string) bool {
	return m.online[agentID]
}

func (m *mockHub) SendToAgent(agentID string, message []byte) error {
	return nil
}

func (m *mockHub) BroadcastToDashboards(message []byte) {
}

func (m *mockHub) GetOnlineAgents() []string {
	agents := make([]string, 0, len(m.online))
	for id := range m.online {
		agents = append(agents, id)
	}
	return agents
}

func (m *mockHub) RegisterAgent(conn *gorillaws.Conn, agentID string, deviceID uuid.UUID) *websocket.Client {
	return nil
}

func (m *mockHub) RegisterDashboard(conn *gorillaws.Conn, userID uuid.UUID) *websocket.Client {
	return nil
}

func newMockHub() *mockHub {
	return &mockHub{
		online: make(map[string]bool),
	}
}

func createTestDevice(t *testing.T, db *database.DB, name string) (uuid.UUID, string) {
	ctx := context.Background()
	deviceID := uuid.New()
	agentID := uuid.New().String()

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (
			id, agent_id, hostname, os_type, status, created_at, updated_at
		) VALUES ($1, $2, $3, 'linux', 'offline', NOW(), NOW())
	`, deviceID, agentID, name)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	return deviceID, agentID
}

func generateTestJWT(cfg *config.Config, userID uuid.UUID, email, role string) string {
	claims := middleware.Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "sentinel",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(cfg.JWTSecret))
	return tokenString
}

func TestListDevices_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	// Create test devices
	createTestDevice(t, db, "test-device-1")
	createTestDevice(t, db, "test-device-2")

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices", router.listDevices)

	req := httptest.NewRequest("GET", "/api/devices", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var devices []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &devices); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(devices) != 2 {
		t.Errorf("Expected 2 devices, got %d", len(devices))
	}
}

func TestGetDevice_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "test-device")

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id", router.getDevice)

	req := httptest.NewRequest("GET", "/api/devices/"+deviceID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var device map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if device["hostname"] != "test-device" {
		t.Errorf("Expected hostname 'test-device', got %v", device["hostname"])
	}
}

func TestGetDevice_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id", router.getDevice)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("GET", "/api/devices/"+nonexistentID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent device, got %d", http.StatusNotFound, w.Code)
	}
}

func TestGetDevice_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id", router.getDevice)

	req := httptest.NewRequest("GET", "/api/devices/not-a-uuid", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestDeleteDevice_UninstallingStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "uninstalling-device")

	// Set device to uninstalling status
	_, err := db.Pool().Exec(ctx, "UPDATE devices SET status = 'uninstalling' WHERE id = $1", deviceID)
	if err != nil {
		t.Fatalf("Failed to update device status: %v", err)
	}

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.DELETE("/api/devices/:id", router.deleteDevice)

	req := httptest.NewRequest("DELETE", "/api/devices/"+deviceID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify device was deleted
	var count int
	db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE id = $1", deviceID).Scan(&count)
	if count != 0 {
		t.Error("Device was not deleted from database")
	}
}

func TestDeleteDevice_NotUninstalling(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "active-device")

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.DELETE("/api/devices/:id", router.deleteDevice)

	req := httptest.NewRequest("DELETE", "/api/devices/"+deviceID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for non-uninstalling device, got %d", http.StatusForbidden, w.Code)
	}

	// Verify device was NOT deleted
	var count int
	db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE id = $1", deviceID).Scan(&count)
	if count != 1 {
		t.Error("Device was unexpectedly deleted from database")
	}
}

func TestDisableDevice_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "device-to-disable")
	userID := uuid.New()

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", userID)
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/disable", router.disableDevice)

	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/disable", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify device was disabled
	var isDisabled bool
	var status string
	err := db.Pool().QueryRow(ctx, "SELECT is_disabled, status FROM devices WHERE id = $1", deviceID).Scan(&isDisabled, &status)
	if err != nil {
		t.Fatalf("Failed to query device: %v", err)
	}

	if !isDisabled {
		t.Error("Device is_disabled flag was not set")
	}
	if status != "disabled" {
		t.Errorf("Expected status 'disabled', got %s", status)
	}
}

func TestEnableDevice_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "disabled-device")

	// Disable the device first
	_, err := db.Pool().Exec(ctx, `
		UPDATE devices SET is_disabled = TRUE, status = 'disabled' WHERE id = $1
	`, deviceID)
	if err != nil {
		t.Fatalf("Failed to disable device: %v", err)
	}

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/enable", router.enableDevice)

	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/enable", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify device was enabled
	var isDisabled bool
	var status string
	err = db.Pool().QueryRow(ctx, "SELECT is_disabled, status FROM devices WHERE id = $1", deviceID).Scan(&isDisabled, &status)
	if err != nil {
		t.Fatalf("Failed to query device: %v", err)
	}

	if isDisabled {
		t.Error("Device is_disabled flag was not cleared")
	}
	if status != "offline" {
		t.Errorf("Expected status 'offline', got %s", status)
	}
}

func TestGetDeviceMetrics_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")
	defer db.Pool().Exec(ctx, "DELETE FROM device_metrics")

	deviceID, _ := createTestDevice(t, db, "metrics-device")

	// Insert test metrics
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, timestamp)
		VALUES ($1, 50.5, 60.0, NOW() - INTERVAL '30 minutes')
	`, deviceID)
	if err != nil {
		t.Fatalf("Failed to create test metrics: %v", err)
	}

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id/metrics", router.getDeviceMetrics)

	req := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/metrics", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var metrics []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected at least one metric")
	}
}

func TestGetDeviceMetrics_CustomTimeRange(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")
	defer db.Pool().Exec(ctx, "DELETE FROM device_metrics")

	deviceID, _ := createTestDevice(t, db, "metrics-device")

	cfg := &config.Config{
		JWTSecret: "test-jwt-secret-key-32-chars!",
	}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id/metrics", router.getDeviceMetrics)

	// Test with custom hours parameter
	testCases := []struct {
		hours      int
		shouldPass bool
	}{
		{1, true},
		{24, true},
		{168, true},  // 1 week
		{0, true},    // Should use default
		{-1, true},   // Should use default
		{200, true},  // Should be capped at 168
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("hours=%d", tc.hours), func(t *testing.T) {
			url := fmt.Sprintf("/api/devices/%s/metrics?hours=%d", deviceID.String(), tc.hours)
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}
		})
	}
}
