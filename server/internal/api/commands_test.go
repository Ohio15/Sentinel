package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/pkg/config"
)

func TestExecuteCommand_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	r.POST("/api/devices/:id/commands", middleware.RequireRole("admin", "operator"), router.executeCommand)

	deviceID := uuid.New()
	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for unauthenticated request, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestExecuteCommand_ViewerRoleForbidden(t *testing.T) {
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
	r.POST("/api/devices/:id/commands", middleware.RequireRole("admin", "operator"), router.executeCommand)

	deviceID := uuid.New()
	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for viewer role, got %d", http.StatusForbidden, w.Code)
	}
}

func TestExecuteCommand_InvalidDeviceID(t *testing.T) {
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
	r.POST("/api/devices/:id/commands", router.executeCommand)

	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/not-a-uuid/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestExecuteCommand_MissingBody(t *testing.T) {
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
	r.POST("/api/devices/:id/commands", router.executeCommand)

	deviceID := uuid.New()
	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for missing command, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestExecuteCommand_DeviceNotFound(t *testing.T) {
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
	r.POST("/api/devices/:id/commands", router.executeCommand)

	nonexistentID := uuid.New()
	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/"+nonexistentID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent device, got %d", http.StatusNotFound, w.Code)
	}
}

func TestExecuteCommand_DeviceOffline(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "offline-device")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	// Do not mark agent as online
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/commands", router.executeCommand)

	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for offline device, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errMsg, ok := resp["error"].(string); ok {
		if errMsg != "Device is offline" {
			t.Errorf("Expected 'Device is offline' error, got: %s", errMsg)
		}
	}
}

func TestExecuteCommand_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")
	defer db.Pool().Exec(ctx, "DELETE FROM commands")

	deviceID, agentID := createTestDevice(t, db, "online-device")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	hub.online[agentID] = true
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/commands", router.executeCommand)

	body := `{"command":"whoami","commandType":"bash"}`
	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := resp["commandId"]; !ok {
		t.Error("Response missing commandId")
	}
	if _, ok := resp["requestId"]; !ok {
		t.Error("Response missing requestId")
	}
	if resp["status"] != "running" {
		t.Errorf("Expected status 'running', got %v", resp["status"])
	}
}

func TestGetCommand_InvalidID(t *testing.T) {
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
	r.GET("/api/commands/:id", router.getCommand)

	req := httptest.NewRequest("GET", "/api/commands/not-a-uuid", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestGetCommand_NotFound(t *testing.T) {
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
	r.GET("/api/commands/:id", router.getCommand)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("GET", "/api/commands/"+nonexistentID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent command, got %d", http.StatusNotFound, w.Code)
	}
}

func TestListCommands_Success(t *testing.T) {
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
	r.GET("/api/commands", router.listCommands)

	req := httptest.NewRequest("GET", "/api/commands", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := resp["commands"]; !ok {
		t.Error("Response missing commands array")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("Response missing total count")
	}
}

func TestListDeviceCommands_InvalidDeviceID(t *testing.T) {
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
	r.GET("/api/devices/:id/commands", router.listDeviceCommands)

	req := httptest.NewRequest("GET", "/api/devices/not-a-uuid/commands", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid device ID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestListDeviceCommands_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices")

	deviceID, _ := createTestDevice(t, db, "cmd-list-device")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id/commands", router.listDeviceCommands)

	req := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/commands", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := resp["commands"]; !ok {
		t.Error("Response missing commands array")
	}
}

func TestExecuteCommand_RoleBasedAccess(t *testing.T) {
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
		{"admin can execute", "admin", http.StatusNotFound},       // passes role check, fails at device lookup
		{"operator can execute", "operator", http.StatusNotFound}, // passes role check, fails at device lookup
		{"viewer cannot execute", "viewer", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tt.role)
				c.Next()
			})
			r.POST("/api/devices/:id/commands", middleware.RequireRole("admin", "operator"), router.executeCommand)

			deviceID := uuid.New()
			body := `{"command":"whoami","commandType":"bash"}`
			req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/commands", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d for role %s, got %d. Body: %s",
					tt.wantStatus, tt.role, w.Code, w.Body.String())
			}
		})
	}
}
