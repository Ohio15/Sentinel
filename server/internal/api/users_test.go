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

func TestListUsers_AdminSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	createTestUser(t, db, "listuser1@test.example.com", "ValidPassword123!", "admin")
	createTestUser(t, db, "listuser2@test.example.com", "ValidPassword123!", "viewer")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/users", router.listUsers)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var users []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(users) < 2 {
		t.Errorf("Expected at least 2 users, got %d", len(users))
	}

	// Verify required fields
	for _, user := range users {
		requiredFields := []string{"id", "email", "role", "isActive", "createdAt"}
		for _, field := range requiredFields {
			if _, ok := user[field]; !ok {
				t.Errorf("User missing required field: %s", field)
			}
		}
	}
}

func TestListUsers_NonAdminForbidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	roles := []string{"operator", "viewer"}

	for _, role := range roles {
		t.Run(role+"_forbidden", func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", role)
				c.Next()
			})
			r.GET("/api/users", middleware.RequireRole("admin"), router.listUsers)

			req := httptest.NewRequest("GET", "/api/users", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status %d for %s role, got %d", http.StatusForbidden, role, w.Code)
			}
		})
	}
}

func TestCreateUser_AdminSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM users WHERE email = 'newuser@test.example.com'")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/users", router.createUser)

	body := `{"email":"newuser@test.example.com","password":"SecurePass123!","firstName":"New","lastName":"User","role":"operator"}`
	req := httptest.NewRequest("POST", "/api/users", bytes.NewBufferString(body))
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

	if resp["email"] != "newuser@test.example.com" {
		t.Errorf("Expected email 'newuser@test.example.com', got %v", resp["email"])
	}
	if resp["role"] != "operator" {
		t.Errorf("Expected role 'operator', got %v", resp["role"])
	}
	if _, ok := resp["id"]; !ok {
		t.Error("Response missing id")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	createTestUser(t, db, "duplicate@test.example.com", "ValidPassword123!", "admin")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/users", router.createUser)

	body := `{"email":"duplicate@test.example.com","password":"AnotherPass123!","firstName":"Dup","lastName":"User","role":"viewer"}`
	req := httptest.NewRequest("POST", "/api/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected status %d for duplicate email, got %d", http.StatusConflict, w.Code)
	}
}

func TestCreateUser_DefaultRole(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM users WHERE email = 'defaultrole@test.example.com'")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/users", router.createUser)

	// No role specified - should default to "user"
	body := `{"email":"defaultrole@test.example.com","password":"SecurePass123!","firstName":"Default","lastName":"Role"}`
	req := httptest.NewRequest("POST", "/api/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["role"] != "user" {
		t.Errorf("Expected default role 'user', got %v", resp["role"])
	}
}

func TestCreateUser_NonAdminForbidden(t *testing.T) {
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
		c.Set("role", "operator")
		c.Next()
	})
	r.POST("/api/users", middleware.RequireRole("admin"), router.createUser)

	body := `{"email":"blocked@test.example.com","password":"SecurePass123!","firstName":"Block","lastName":"User"}`
	req := httptest.NewRequest("POST", "/api/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for non-admin, got %d", http.StatusForbidden, w.Code)
	}
}

func TestDeleteUser_AdminSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	userID := createTestUser(t, db, "deleteuser@test.example.com", "ValidPassword123!", "viewer")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New()) // different admin user
		c.Set("role", "admin")
		c.Next()
	})
	r.DELETE("/api/users/:id", router.deleteUser)

	req := httptest.NewRequest("DELETE", "/api/users/"+userID.String(), nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify soft delete - is_active should be false
	var isActive bool
	db.Pool().QueryRow(ctx, "SELECT is_active FROM users WHERE id = $1", userID).Scan(&isActive)
	if isActive {
		t.Error("User should be soft-deleted (is_active=false)")
	}
}

func TestDeleteUser_InvalidID(t *testing.T) {
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
	r.DELETE("/api/users/:id", router.deleteUser)

	req := httptest.NewRequest("DELETE", "/api/users/not-a-uuid", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid UUID, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestDeleteUser_NonAdminForbidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	roles := []string{"operator", "viewer"}

	for _, role := range roles {
		t.Run(role+"_forbidden", func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", role)
				c.Next()
			})
			r.DELETE("/api/users/:id", middleware.RequireRole("admin"), router.deleteUser)

			userID := uuid.New()
			req := httptest.NewRequest("DELETE", "/api/users/"+userID.String(), nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status %d for %s role, got %d", http.StatusForbidden, role, w.Code)
			}
		})
	}
}

func TestCreateInvitation_AdminSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM invitations WHERE email = 'invite@test.example.com'")
	defer cleanupTestDB(db, ctx)

	userID := createTestUser(t, db, "inviter@test.example.com", "ValidPassword123!", "admin")

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", userID)
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/invitations", router.createInvitation)

	body := `{"email":"invite@test.example.com","role":"viewer"}`
	req := httptest.NewRequest("POST", "/api/invitations", bytes.NewBufferString(body))
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
	if _, ok := resp["token"]; !ok {
		t.Error("Response missing token")
	}
	if resp["role"] != "viewer" {
		t.Errorf("Expected role 'viewer', got %v", resp["role"])
	}
}

func TestCreateInvitation_InvalidRole(t *testing.T) {
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
	r.POST("/api/invitations", router.createInvitation)

	body := `{"email":"bad@test.example.com","role":"superadmin"}`
	req := httptest.NewRequest("POST", "/api/invitations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid role, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateInvitation_NonAdminForbidden(t *testing.T) {
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
		c.Set("role", "operator")
		c.Next()
	})
	r.POST("/api/invitations", middleware.RequireRole("admin"), router.createInvitation)

	body := `{"email":"blocked@test.example.com","role":"viewer"}`
	req := httptest.NewRequest("POST", "/api/invitations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for non-admin, got %d", http.StatusForbidden, w.Code)
	}
}

func TestListUsers_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	r.GET("/api/users", middleware.RequireRole("admin"), router.listUsers)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for unauthenticated request, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestCreateUser_RoleBasedAccess(t *testing.T) {
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
		{"admin allowed", "admin", http.StatusCreated},
		{"operator forbidden", "operator", http.StatusForbidden},
		{"viewer forbidden", "viewer", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := "roletest-" + tt.role + "@test.example.com"
			defer func() {
				ctx := context.Background()
				db.Pool().Exec(ctx, "DELETE FROM users WHERE email = $1", email)
			}()

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tt.role)
				c.Next()
			})
			r.POST("/api/users", middleware.RequireRole("admin"), router.createUser)

			body, _ := json.Marshal(map[string]string{
				"email":     email,
				"password":  "SecurePass123!",
				"firstName": "Role",
				"lastName":  "Test",
				"role":      "viewer",
			})
			req := httptest.NewRequest("POST", "/api/users", bytes.NewBuffer(body))
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
