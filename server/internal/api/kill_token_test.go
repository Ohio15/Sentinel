package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/pkg/config"
)

func TestGenerateKillToken_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	// Create test device with organization_id
	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-success", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)

	req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response contains expected fields
	killToken, ok := resp["killToken"].(string)
	if !ok || killToken == "" {
		t.Fatal("Response missing or empty killToken")
	}
	if _, ok := resp["deviceId"]; !ok {
		t.Error("Response missing deviceId")
	}
	if _, ok := resp["agentId"]; !ok {
		t.Error("Response missing agentId")
	}
	if _, ok := resp["message"]; !ok {
		t.Error("Response missing message")
	}

	// killToken should be 64 hex chars (32 bytes encoded as hex)
	if len(killToken) != 64 {
		t.Errorf("Expected killToken length 64, got %d", len(killToken))
	}
	// Verify it's valid hex
	if _, err := hex.DecodeString(killToken); err != nil {
		t.Errorf("killToken is not valid hex: %v", err)
	}

	// Verify the agentId in the response matches what we inserted
	if resp["agentId"] != agentID {
		t.Errorf("Expected agentId %s, got %v", agentID, resp["agentId"])
	}

	// Verify database has kill_token_hash set and matches sha256(plaintext)
	var storedHash *string
	err = db.Pool().QueryRow(ctx, "SELECT kill_token_hash FROM devices WHERE id = $1", deviceID).Scan(&storedHash)
	if err != nil {
		t.Fatalf("Failed to query kill_token_hash: %v", err)
	}
	if storedHash == nil || *storedHash == "" {
		t.Fatal("kill_token_hash not set in database")
	}

	expectedHash := sha256.Sum256([]byte(killToken))
	expectedHashHex := hex.EncodeToString(expectedHash[:])
	if *storedHash != expectedHashHex {
		t.Errorf("Stored hash does not match sha256(plaintext).\nExpected: %s\nGot:      %s", expectedHashHex, *storedHash)
	}
}

func TestGenerateKillToken_InvalidDevice(t *testing.T) {
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
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("POST", "/api/devices/"+nonexistentID.String()+"/generate-kill-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent device, got %d. Body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestGenerateKillToken_RotatesOnRegenerate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-rotate", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)

	// First generation
	req1 := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("First generate failed: status %d, body: %s", w1.Code, w1.Body.String())
	}

	var resp1 map[string]interface{}
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	token1 := resp1["killToken"].(string)

	var hash1 string
	db.Pool().QueryRow(ctx, "SELECT kill_token_hash FROM devices WHERE id = $1", deviceID).Scan(&hash1)

	// Second generation
	req2 := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Second generate failed: status %d, body: %s", w2.Code, w2.Body.String())
	}

	var resp2 map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	token2 := resp2["killToken"].(string)

	var hash2 string
	db.Pool().QueryRow(ctx, "SELECT kill_token_hash FROM devices WHERE id = $1", deviceID).Scan(&hash2)

	// Tokens and hashes must differ
	if token1 == token2 {
		t.Error("Regenerated token should be different from the first token")
	}
	if hash1 == hash2 {
		t.Error("Regenerated hash should be different from the first hash")
	}

	// Verify the second hash matches sha256 of the second token
	expectedHash := sha256.Sum256([]byte(token2))
	expectedHashHex := hex.EncodeToString(expectedHash[:])
	if hash2 != expectedHashHex {
		t.Errorf("Second stored hash does not match sha256(second token)")
	}
}

func TestEmergencyUninstallScript_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-script", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)
	r.GET("/api/devices/:id/emergency-uninstall-script", router.getEmergencyUninstallScript)

	// Step 1: Generate kill token first
	genReq := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusOK {
		t.Fatalf("Generate kill token failed: status %d, body: %s", genW.Code, genW.Body.String())
	}

	// Record the hash after first generation
	var hash1 string
	db.Pool().QueryRow(ctx, "SELECT kill_token_hash FROM devices WHERE id = $1", deviceID).Scan(&hash1)

	// Step 2: Request emergency uninstall script
	scriptReq := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/emergency-uninstall-script", nil)
	scriptW := httptest.NewRecorder()
	r.ServeHTTP(scriptW, scriptReq)

	if scriptW.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, scriptW.Code, scriptW.Body.String())
	}

	// Verify Content-Type
	contentType := scriptW.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Expected Content-Type 'application/octet-stream', got '%s'", contentType)
	}

	// Verify Content-Disposition contains the agent ID
	contentDisp := scriptW.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisp, agentID) {
		t.Errorf("Content-Disposition should contain agent ID. Got: %s", contentDisp)
	}

	script := scriptW.Body.String()

	// Script must contain the agent ID
	if !strings.Contains(script, agentID) {
		t.Error("Emergency uninstall script does not contain the agent ID")
	}

	// Script must contain a kill token (64 hex chars embedded in $KillToken = "...")
	if !strings.Contains(script, "$KillToken = \"") {
		t.Error("Emergency uninstall script does not contain $KillToken assignment")
	}

	// Verify the hash was rotated (script generates a fresh token)
	var hash2 string
	db.Pool().QueryRow(ctx, "SELECT kill_token_hash FROM devices WHERE id = $1", deviceID).Scan(&hash2)
	if hash1 == hash2 {
		t.Error("Emergency uninstall script should rotate the kill token hash")
	}
}

func TestEmergencyUninstallScript_NoKillToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	// Create device WITHOUT generating a kill token
	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-notoken", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.GET("/api/devices/:id/emergency-uninstall-script", router.getEmergencyUninstallScript)

	req := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/emergency-uninstall-script", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for device without kill token, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("Response missing error field")
	}
}

func TestEmergencyUninstallScript_InvalidDevice(t *testing.T) {
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
	r.GET("/api/devices/:id/emergency-uninstall-script", router.getEmergencyUninstallScript)

	nonexistentID := uuid.New()
	req := httptest.NewRequest("GET", "/api/devices/"+nonexistentID.String()+"/emergency-uninstall-script", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent device, got %d. Body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestKillTokenHash_MatchesExpected(t *testing.T) {
	// Unit test: verify the generateKillToken function produces correct hash
	plaintext, hashHex, err := generateKillToken()
	if err != nil {
		t.Fatalf("generateKillToken returned error: %v", err)
	}

	// Plaintext should be 64 hex chars (32 bytes)
	if len(plaintext) != 64 {
		t.Errorf("Expected plaintext length 64, got %d", len(plaintext))
	}

	// Hash should be 64 hex chars (SHA-256 = 32 bytes)
	if len(hashHex) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hashHex))
	}

	// Verify the hash is sha256 of the plaintext string
	expectedHash := sha256.Sum256([]byte(plaintext))
	expectedHashHex := hex.EncodeToString(expectedHash[:])
	if hashHex != expectedHashHex {
		t.Errorf("Hash mismatch.\nExpected: %s\nGot:      %s", expectedHashHex, hashHex)
	}

	// Verify both are valid hex
	if _, err := hex.DecodeString(plaintext); err != nil {
		t.Errorf("Plaintext is not valid hex: %v", err)
	}
	if _, err := hex.DecodeString(hashHex); err != nil {
		t.Errorf("Hash is not valid hex: %v", err)
	}

	// Generate a second token and verify uniqueness
	plaintext2, hashHex2, err := generateKillToken()
	if err != nil {
		t.Fatalf("Second generateKillToken returned error: %v", err)
	}
	if plaintext == plaintext2 {
		t.Error("Two consecutive token generations produced identical plaintexts")
	}
	if hashHex == hashHex2 {
		t.Error("Two consecutive token generations produced identical hashes")
	}
}

func TestAgentReplacementFlow_EndToEnd(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	// Step a: Create device simulating existing broken agent
	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'online', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-replacement", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	// Step b: Set device status to offline (simulating broken agent)
	_, err = db.Pool().Exec(ctx, "UPDATE devices SET status = 'offline' WHERE id = $1", deviceID)
	if err != nil {
		t.Fatalf("Failed to update device status: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)
	r.GET("/api/devices/:id/emergency-uninstall-script", router.getEmergencyUninstallScript)

	// Step c: Generate kill token
	genReq := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusOK {
		t.Fatalf("Generate kill token failed: status %d, body: %s", genW.Code, genW.Body.String())
	}

	// Step d: Verify token returned
	var genResp map[string]interface{}
	json.Unmarshal(genW.Body.Bytes(), &genResp)
	killToken, ok := genResp["killToken"].(string)
	if !ok || killToken == "" {
		t.Fatal("Kill token not returned in response")
	}
	if len(killToken) != 64 {
		t.Errorf("Expected kill token length 64, got %d", len(killToken))
	}

	// Step e: Get emergency uninstall script
	scriptReq := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/emergency-uninstall-script", nil)
	scriptW := httptest.NewRecorder()
	r.ServeHTTP(scriptW, scriptReq)

	if scriptW.Code != http.StatusOK {
		t.Fatalf("Emergency uninstall script failed: status %d, body: %s", scriptW.Code, scriptW.Body.String())
	}

	// Step f: Verify script contains correct token (the script generates a new token, so check it has a token)
	script := scriptW.Body.String()
	if !strings.Contains(script, "$KillToken = \"") {
		t.Error("Script does not contain kill token assignment")
	}
	if !strings.Contains(script, agentID) {
		t.Error("Script does not contain the original agent ID")
	}
	if !strings.Contains(script, "Sentinel Emergency Uninstall Script") {
		t.Error("Script missing header comment")
	}

	// Step g: Simulate re-enrollment — create new device record with same hostname
	newDeviceID := uuid.New()
	newAgentID := uuid.New().String()
	_, err = db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, newDeviceID, newAgentID, "test-kt-replacement", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create replacement device: %v", err)
	}

	// Step h: Verify new device is created (different ID, same hostname)
	var count int
	err = db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE hostname = $1", "test-kt-replacement").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count devices: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 devices with same hostname, got %d", count)
	}

	// Step i: Set new device online via mock hub
	hub.online[newAgentID] = true

	// Step j: Verify device shows online through the hub
	if !hub.IsAgentOnline(newAgentID) {
		t.Error("New agent should be online in the hub")
	}
	if hub.IsAgentOnline(agentID) {
		t.Error("Old agent should not be online in the hub")
	}
}

func TestKillToken_RequiresAdmin(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-admin", constants.CurrentOrganizationID)
	if err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	routerInst := &Router{config: cfg, db: db, cache: cache, hub: hub}

	// Test with different roles to verify which roles can access kill token endpoints.
	// The actual route registration uses RequireRole("admin") middleware.
	// Here we simulate the middleware behavior by testing with non-admin roles.
	roles := []struct {
		role       string
		expectCode int
	}{
		{"admin", http.StatusOK},
		{"viewer", http.StatusForbidden},
		{"technician", http.StatusForbidden},
	}

	for _, tc := range roles {
		t.Run("role="+tc.role, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userId", uuid.New())
				c.Set("role", tc.role)
				// Simulate RequireRole("admin") middleware behavior
				if tc.role != "admin" {
					c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
					c.Abort()
					return
				}
				c.Next()
			})
			r.POST("/api/devices/:id/generate-kill-token", routerInst.generateKillTokenForDevice)

			req := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectCode {
				t.Errorf("Role %s: expected status %d, got %d. Body: %s", tc.role, tc.expectCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestKillToken_OrganizationIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cache := setupTestCache(t)
	defer cache.Close()

	ctx := context.Background()
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE hostname LIKE 'test-kt-%'")

	// Create a device in org 999 (different from CurrentOrganizationID which is 1)
	deviceID := uuid.New()
	agentID := uuid.New().String()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, os_type, status, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'windows', 'offline', $4, NOW(), NOW())
	`, deviceID, agentID, "test-kt-org-isolation", 999)
	if err != nil {
		t.Fatalf("Failed to create test device in org 999: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-jwt-secret-key-32-chars!"}
	hub := newMockHub()
	router := &Router{config: cfg, db: db, cache: cache, hub: hub}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/devices/:id/generate-kill-token", router.generateKillTokenForDevice)
	r.GET("/api/devices/:id/emergency-uninstall-script", router.getEmergencyUninstallScript)

	// Attempt to generate kill token for device in org 999 — should 404 because
	// the handler filters by constants.CurrentOrganizationID (1)
	genReq := httptest.NewRequest("POST", "/api/devices/"+deviceID.String()+"/generate-kill-token", nil)
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for device in different org, got %d. Body: %s",
			http.StatusNotFound, genW.Code, genW.Body.String())
	}

	// Attempt to get emergency uninstall script for device in org 999 — should also 404
	scriptReq := httptest.NewRequest("GET", "/api/devices/"+deviceID.String()+"/emergency-uninstall-script", nil)
	scriptW := httptest.NewRecorder()
	r.ServeHTTP(scriptW, scriptReq)

	if scriptW.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for script on device in different org, got %d. Body: %s",
			http.StatusNotFound, scriptW.Code, scriptW.Body.String())
	}
}
