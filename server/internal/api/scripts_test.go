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
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

func createTestScript(t *testing.T, db *database.DB, name, language, content string, userID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var scriptID uuid.UUID
	err := db.Pool().QueryRow(ctx, `
		INSERT INTO scripts (name, description, language, content, os_types, created_by, organization_id)
		VALUES ($1, 'test script', $2, $3, ARRAY['linux'], $4, $5)
		RETURNING id
	`, name, language, content, userID, constants.CurrentOrganizationID).Scan(&scriptID)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}
	return scriptID
}

func TestListScripts_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM scripts WHERE name LIKE 'test-script-%'")

	userID := createTestUser(t, db, "scripts-list@test.example.com", "ValidPassword123!", "admin")
	defer cleanupTestDB(db, ctx)

	createTestScript(t, db, "test-script-1", "bash", "echo hello", userID)
	createTestScript(t, db, "test-script-2", "powershell", "Write-Host hello", userID)

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", userID)
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/scripts", router.listScripts)

	req := httptest.NewRequest("GET", "/api/scripts", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var scripts []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &scripts); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(scripts) < 2 {
		t.Errorf("Expected at least 2 scripts, got %d", len(scripts))
	}
}

func TestCreateScript_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM scripts WHERE name = 'created-script'")
	defer cleanupTestDB(db, ctx)

	userID := createTestUser(t, db, "scripts-create@test.example.com", "ValidPassword123!", "admin")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", userID)
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/scripts", router.createScript)

	body := `{"name":"created-script","language":"bash","content":"echo test","osTypes":["linux"]}`
	req := httptest.NewRequest("POST", "/api/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := resp["id"]; !ok {
		t.Error("Response missing id")
	}
	if resp["name"] != "created-script" {
		t.Errorf("Expected name 'created-script', got %v", resp["name"])
	}
	if resp["language"] != "bash" {
		t.Errorf("Expected language 'bash', got %v", resp["language"])
	}
}

func TestCreateScript_InvalidLanguage(t *testing.T) {
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
	r.POST("/api/scripts", router.createScript)

	body := `{"name":"bad-script","language":"ruby","content":"puts 'hello'"}`
	req := httptest.NewRequest("POST", "/api/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid language, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateScript_MissingRequiredFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	tests := []struct {
		name string
		body string
	}{
		{"Missing name", `{"language":"bash","content":"echo hello"}`},
		{"Missing language", `{"name":"test","content":"echo hello"}`},
		{"Missing content", `{"name":"test","language":"bash"}`},
		{"Empty body", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", "admin")
				c.Next()
			})
			r.POST("/api/scripts", router.createScript)

			req := httptest.NewRequest("POST", "/api/scripts", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestCreateScript_ViewerForbidden(t *testing.T) {
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
	r.POST("/api/scripts", middleware.RequireRole("admin", "operator"), router.createScript)

	body := `{"name":"forbidden-script","language":"bash","content":"echo hello"}`
	req := httptest.NewRequest("POST", "/api/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for viewer role, got %d", http.StatusForbidden, w.Code)
	}
}

func TestGetScript_NotFound(t *testing.T) {
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
	r.GET("/api/scripts/:id", router.getScript)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("GET", "/api/scripts/"+nonexistentID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent script, got %d", http.StatusNotFound, w.Code)
	}
}

func TestGetScript_InvalidID(t *testing.T) {
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
	r.GET("/api/scripts/:id", router.getScript)

	req := httptest.NewRequest("GET", "/api/scripts/not-a-uuid", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestDeleteScript_NotFound(t *testing.T) {
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
	r.DELETE("/api/scripts/:id", router.deleteScript)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("DELETE", "/api/scripts/"+nonexistentID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent script, got %d", http.StatusNotFound, w.Code)
	}
}

func TestDeleteScript_OnlyAdminAllowed(t *testing.T) {
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
		{"admin can delete", "admin", http.StatusNotFound},     // passes role check, fails at not found
		{"operator cannot delete", "operator", http.StatusForbidden},
		{"viewer cannot delete", "viewer", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tt.role)
				c.Next()
			})
			r.DELETE("/api/scripts/:id", middleware.RequireRole("admin"), router.deleteScript)

			scriptID := uuid.New()
			req := httptest.NewRequest("DELETE", "/api/scripts/"+scriptID.String(), nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d for role %s, got %d",
					tt.wantStatus, tt.role, w.Code)
			}
		})
	}
}

func TestUpdateScript_InvalidID(t *testing.T) {
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
	r.PUT("/api/scripts/:id", router.updateScript)

	body := `{"name":"updated-name"}`
	req := httptest.NewRequest("PUT", "/api/scripts/not-a-uuid", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestExecuteScript_InvalidScriptID(t *testing.T) {
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
	r.POST("/api/scripts/:id/execute", router.executeScript)

	body := `{"deviceId":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/api/scripts/not-a-uuid/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid script ID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestExecuteScript_MissingDeviceID(t *testing.T) {
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
	r.POST("/api/scripts/:id/execute", router.executeScript)

	scriptID := uuid.New()
	body := `{}`
	req := httptest.NewRequest("POST", "/api/scripts/"+scriptID.String()+"/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for missing deviceId, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateScript_ValidLanguages(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM scripts WHERE name LIKE 'lang-test-%'")
	defer cleanupTestDB(db, ctx)

	userID := createTestUser(t, db, "scripts-lang@test.example.com", "ValidPassword123!", "admin")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	validLanguages := []string{"powershell", "bash", "python", "batch"}

	for _, lang := range validLanguages {
		t.Run("language_"+lang, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", userID)
				c.Set("role", "admin")
				c.Next()
			})
			r.POST("/api/scripts", router.createScript)

			body, _ := json.Marshal(map[string]interface{}{
				"name":     "lang-test-" + lang,
				"language": lang,
				"content":  "echo test",
			})
			req := httptest.NewRequest("POST", "/api/scripts", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Errorf("Expected status %d for language %s, got %d. Body: %s",
					http.StatusCreated, lang, w.Code, w.Body.String())
			}
		})
	}
}
