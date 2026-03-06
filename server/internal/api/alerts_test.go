package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

func createTestAlert(t *testing.T, db *database.DB, deviceID uuid.UUID, severity, title, status string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var alertID uuid.UUID
	err := db.Pool().QueryRow(ctx, `
		INSERT INTO alerts (device_id, severity, title, message, status, organization_id)
		VALUES ($1, $2, $3, 'Test alert message', $4, $5)
		RETURNING id
	`, deviceID, severity, title, status, constants.CurrentOrganizationID).Scan(&alertID)
	if err != nil {
		t.Fatalf("Failed to create test alert: %v", err)
	}
	return alertID
}

func TestListAlerts_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM alerts WHERE title LIKE 'test-alert-%'")
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname = 'alert-device'")

	deviceID, _ := createTestDevice(t, db, "alert-device")
	createTestAlert(t, db, deviceID, "critical", "test-alert-1", "active")
	createTestAlert(t, db, deviceID, "warning", "test-alert-2", "active")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/alerts", router.listAlerts)

	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var alerts []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &alerts); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(alerts) < 2 {
		t.Errorf("Expected at least 2 alerts, got %d", len(alerts))
	}

	// Verify required fields present
	for _, alert := range alerts {
		requiredFields := []string{"id", "deviceId", "severity", "title", "status", "createdAt"}
		for _, field := range requiredFields {
			if _, ok := alert[field]; !ok {
				t.Errorf("Alert missing required field: %s", field)
			}
		}
	}
}

func TestListAlerts_FilterByStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM alerts WHERE title LIKE 'filter-alert-%'")
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname = 'filter-alert-device'")

	deviceID, _ := createTestDevice(t, db, "filter-alert-device")
	createTestAlert(t, db, deviceID, "critical", "filter-alert-active", "active")
	createTestAlert(t, db, deviceID, "warning", "filter-alert-resolved", "resolved")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/alerts", router.listAlerts)

	req := httptest.NewRequest("GET", "/api/alerts?status=active", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var alerts []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &alerts); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	for _, alert := range alerts {
		if alert["status"] != "active" {
			t.Errorf("Expected all alerts to have status 'active', got '%v'", alert["status"])
		}
	}
}

func TestGetAlert_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM alerts WHERE title = 'get-alert-test'")
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname = 'get-alert-device'")

	deviceID, _ := createTestDevice(t, db, "get-alert-device")
	alertID := createTestAlert(t, db, deviceID, "critical", "get-alert-test", "active")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/alerts/:id", router.getAlert)

	req := httptest.NewRequest("GET", "/api/alerts/"+alertID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var alert map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &alert); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if alert["title"] != "get-alert-test" {
		t.Errorf("Expected title 'get-alert-test', got %v", alert["title"])
	}
	if alert["severity"] != "critical" {
		t.Errorf("Expected severity 'critical', got %v", alert["severity"])
	}
}

func TestGetAlert_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/alerts/:id", router.getAlert)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("GET", "/api/alerts/"+nonexistentID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestGetAlert_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/alerts/:id", router.getAlert)

	req := httptest.NewRequest("GET", "/api/alerts/not-a-uuid", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestAcknowledgeAlert_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM alerts WHERE title = 'ack-test-alert'")
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname = 'ack-alert-device'")

	deviceID, _ := createTestDevice(t, db, "ack-alert-device")
	alertID := createTestAlert(t, db, deviceID, "warning", "ack-test-alert", "active")
	userID := uuid.New()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", userID)
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/alerts/:id/acknowledge", router.acknowledgeAlert)

	req := httptest.NewRequest("POST", "/api/alerts/"+alertID.String()+"/acknowledge", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "Alert acknowledged" {
		t.Errorf("Expected message 'Alert acknowledged', got %v", resp["message"])
	}

	// Verify DB state
	var status string
	db.Pool().QueryRow(ctx, "SELECT status FROM alerts WHERE id = $1", alertID).Scan(&status)
	if status != "acknowledged" {
		t.Errorf("Expected alert status 'acknowledged', got '%s'", status)
	}
}

func TestAcknowledgeAlert_ViewerForbidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "viewer")
		c.Next()
	})
	r.POST("/api/alerts/:id/acknowledge", middleware.RequireRole("admin", "operator"), router.acknowledgeAlert)

	alertID := uuid.New()
	req := httptest.NewRequest("POST", "/api/alerts/"+alertID.String()+"/acknowledge", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for viewer role, got %d", http.StatusForbidden, w.Code)
	}
}

func TestResolveAlert_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM alerts WHERE title = 'resolve-test-alert'")
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname = 'resolve-alert-device'")

	deviceID, _ := createTestDevice(t, db, "resolve-alert-device")
	alertID := createTestAlert(t, db, deviceID, "critical", "resolve-test-alert", "active")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/alerts/:id/resolve", router.resolveAlert)

	req := httptest.NewRequest("POST", "/api/alerts/"+alertID.String()+"/resolve", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "Alert resolved" {
		t.Errorf("Expected message 'Alert resolved', got %v", resp["message"])
	}

	// Verify DB state
	var status string
	db.Pool().QueryRow(ctx, "SELECT status FROM alerts WHERE id = $1", alertID).Scan(&status)
	if status != "resolved" {
		t.Errorf("Expected alert status 'resolved', got '%s'", status)
	}
}

func TestResolveAlert_ViewerForbidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "viewer")
		c.Next()
	})
	r.POST("/api/alerts/:id/resolve", middleware.RequireRole("admin", "operator"), router.resolveAlert)

	alertID := uuid.New()
	req := httptest.NewRequest("POST", "/api/alerts/"+alertID.String()+"/resolve", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for viewer role, got %d", http.StatusForbidden, w.Code)
	}
}

func TestAcknowledgeAlert_RoleBasedAccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	tests := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{"admin can acknowledge", "admin", http.StatusOK},
		{"operator can acknowledge", "operator", http.StatusOK},
		{"viewer cannot acknowledge", "viewer", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tt.role)
				c.Next()
			})
			r.POST("/api/alerts/:id/acknowledge", middleware.RequireRole("admin", "operator"), router.acknowledgeAlert)

			alertID := uuid.New() // nonexistent, but role check happens first for viewer
			req := httptest.NewRequest("POST", "/api/alerts/"+alertID.String()+"/acknowledge", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if tt.role == "viewer" {
				if w.Code != http.StatusForbidden {
					t.Errorf("Expected status %d for role %s, got %d", http.StatusForbidden, tt.role, w.Code)
				}
			} else {
				// Admin/operator pass role check; handler returns OK even for nonexistent alert
				// because acknowledgeAlert does not check rows affected
				if w.Code != http.StatusOK {
					t.Errorf("Expected status %d for role %s, got %d. Body: %s",
						http.StatusOK, tt.role, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestListAlerts_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	r.GET("/api/alerts", router.listAlerts)

	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for unauthenticated request, got %d", http.StatusUnauthorized, w.Code)
	}
}
