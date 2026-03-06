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

func TestGetSettings_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	// Insert a test setting
	db.Pool().Exec(ctx, `
		INSERT INTO settings (key, value) VALUES ('test_setting', 'test_value')
		ON CONFLICT (key) DO UPDATE SET value = 'test_value'
	`)
	defer db.Pool().Exec(ctx, "DELETE FROM settings WHERE key = 'test_setting'")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/settings", router.getSettings)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var settings map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if settings["test_setting"] != "test_value" {
		t.Errorf("Expected setting 'test_setting'='test_value', got '%s'", settings["test_setting"])
	}
}

func TestGetSettings_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	r.GET("/api/settings", router.getSettings)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for unauthenticated request, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestUpdateSettings_AdminSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM settings WHERE key = 'update_test_key'")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.PUT("/api/settings", router.updateSettings)

	body := `{"update_test_key":"new_value"}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "Settings updated successfully" {
		t.Errorf("Expected success message, got %v", resp["message"])
	}

	// Verify setting was persisted
	var value string
	db.Pool().QueryRow(ctx, "SELECT value FROM settings WHERE key = 'update_test_key'").Scan(&value)
	if value != "new_value" {
		t.Errorf("Expected setting value 'new_value', got '%s'", value)
	}
}

func TestUpdateSettings_NonAdminForbidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	roles := []struct {
		name string
		role string
	}{
		{"operator", "operator"},
		{"viewer", "viewer"},
	}

	for _, tt := range roles {
		t.Run(tt.name+"_forbidden", func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tt.role)
				c.Next()
			})
			r.PUT("/api/settings", middleware.RequireRole("admin"), router.updateSettings)

			body := `{"some_key":"some_value"}`
			req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status %d for %s role, got %d", http.StatusForbidden, tt.role, w.Code)
			}
		})
	}
}

func TestUpdateSettings_InvalidBody(t *testing.T) {
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
	r.PUT("/api/settings", router.updateSettings)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid JSON, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUpdateSettings_UpsertBehavior(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM settings WHERE key = 'upsert_test_key'")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	// First create the setting
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.PUT("/api/settings", router.updateSettings)

	body := `{"upsert_test_key":"initial_value"}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("First update failed: status %d", w.Code)
	}

	// Now update it (upsert)
	body = `{"upsert_test_key":"updated_value"}`
	req = httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Upsert update failed: status %d", w.Code)
	}

	// Verify the updated value
	var value string
	db.Pool().QueryRow(ctx, "SELECT value FROM settings WHERE key = 'upsert_test_key'").Scan(&value)
	if value != "updated_value" {
		t.Errorf("Expected upserted value 'updated_value', got '%s'", value)
	}
}

func TestGetSettings_ViewerCanRead(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	// Viewer should be able to read settings (no RequireRole on GET /settings)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "viewer")
		c.Next()
	})
	r.GET("/api/settings", router.getSettings)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for viewer reading settings, got %d", http.StatusOK, w.Code)
	}
}
