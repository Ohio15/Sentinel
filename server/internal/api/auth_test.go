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
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// Test helpers

func setupTestDB(t *testing.T) *database.DB {
	// Use test database connection
	testDBURL := "postgres://sentinel:sentinel@localhost:5432/sentinel_test?sslmode=disable"
	db, err := database.New(testDBURL)
	if err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}
	return db
}

func setupTestCache(t *testing.T) *cache.Cache {
	// Use test Redis connection
	testRedisURL := "redis://localhost:6379/1"
	c, err := cache.New(testRedisURL)
	if err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}
	return c
}

func cleanupTestDB(db *database.DB, ctx context.Context) {
	// Clean up test data
	db.Pool().Exec(ctx, "DELETE FROM sessions")
	db.Pool().Exec(ctx, "DELETE FROM users WHERE email LIKE '%@test.example.com'")
}

func createTestUser(t *testing.T, db *database.DB, email string, password string, role string) uuid.UUID {
	ctx := context.Background()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	var userID uuid.UUID
	err = db.Pool().QueryRow(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name, role, is_active)
		VALUES ($1, $2, 'Test', 'User', $3, true)
		RETURNING id
	`, email, string(hashedPassword), role).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return userID
}

// Authentication Tests

func TestLogin_ValidCredentials(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	// Create test user
	email := "valid@test.example.com"
	password := "ValidPassword123!"
	createTestUser(t, db, email, password, "admin")

	// Create router
	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test login
	loginReq := LoginRequest{
		Email:    email,
		Password: password,
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
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

	// Verify response structure
	if _, ok := resp["accessToken"]; !ok {
		t.Error("Response missing accessToken")
	}
	if _, ok := resp["refreshToken"]; !ok {
		t.Error("Response missing refreshToken")
	}
	if _, ok := resp["user"]; !ok {
		t.Error("Response missing user")
	}
}

func TestLogin_InvalidEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test with invalid email format
	loginReq := LoginRequest{
		Email:    "notanemail",
		Password: "SomePassword123!",
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid email, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	// Create test user
	email := "wrongpass@test.example.com"
	password := "CorrectPassword123!"
	createTestUser(t, db, email, password, "admin")

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test with wrong password
	loginReq := LoginRequest{
		Email:    email,
		Password: "WrongPassword456!",
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for wrong password, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test with nonexistent user
	loginReq := LoginRequest{
		Email:    "nonexistent@test.example.com",
		Password: "SomePassword123!",
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for nonexistent user, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestLogin_PasswordTooShort(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test with password too short
	loginReq := LoginRequest{
		Email:    "user@test.example.com",
		Password: "Short1!",
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for short password, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"Missing email", map[string]string{"password": "Password123!"}},
		{"Missing password", map[string]string{"email": "user@test.example.com"}},
		{"Empty body", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestLogin_SQLInjection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	// Test SQL injection attempts
	sqlInjectionAttempts := []string{
		"admin' OR '1'='1",
		"admin'--",
		"admin' #",
		"' OR 1=1--",
		"admin' AND 1=1--",
	}

	for _, injection := range sqlInjectionAttempts {
		loginReq := LoginRequest{
			Email:    injection,
			Password: "Password123!",
		}
		reqBody, _ := json.Marshal(loginReq)
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		// SQL injection should be prevented, resulting in bad request or unauthorized
		if w.Code == http.StatusOK {
			t.Errorf("SQL injection attempt succeeded with: %s", injection)
		}
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	// Create inactive user
	email := "inactive@test.example.com"
	password := "ValidPassword123!"
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	db.Pool().Exec(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name, role, is_active)
		VALUES ($1, $2, 'Test', 'User', 'admin', false)
	`, email, string(hashedPassword))

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/login", router.login)

	loginReq := LoginRequest{
		Email:    email,
		Password: password,
	}
	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for inactive user, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errMsg, ok := resp["error"].(string); ok {
		if errMsg != "Account is disabled" {
			t.Errorf("Expected 'Account is disabled' error, got: %s", errMsg)
		}
	}
}

func TestRefreshToken_Valid(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer cleanupTestDB(db, ctx)

	// Create test user and session
	email := "refresh@test.example.com"
	password := "ValidPassword123!"
	userID := createTestUser(t, db, email, password, "admin")

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	// Generate refresh token
	refreshToken, _ := router.generateRefreshToken(userID, "127.0.0.1", "test-agent")

	r := gin.New()
	r.POST("/api/auth/refresh", router.refreshToken)

	refreshReq := RefreshRequest{
		RefreshToken: refreshToken,
	}
	reqBody, _ := json.Marshal(refreshReq)
	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["accessToken"]; !ok {
		t.Error("Response missing accessToken")
	}
}

func TestRefreshToken_Invalid(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	cfg := &config.Config{
		JWTSecret:         "test-jwt-secret-key-32-chars!",
		RateLimitRequests: 100,
		RateLimitWindow:   60,
	}
	router := &Router{config: cfg, db: db, cache: cache}

	r := gin.New()
	r.POST("/api/auth/refresh", router.refreshToken)

	refreshReq := RefreshRequest{
		RefreshToken: "invalid-token-12345",
	}
	reqBody, _ := json.Marshal(refreshReq)
	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for invalid token, got %d", http.StatusUnauthorized, w.Code)
	}
}
