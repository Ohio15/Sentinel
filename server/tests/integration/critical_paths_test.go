// Package integration provides end-to-end integration tests for critical paths
// Run these tests against a running test instance before deployment
//
// Usage:
//   go test -v ./tests/integration/... -base-url=http://localhost:8091
//   go test -v ./tests/integration/... -base-url=https://sentinelrmm.us:4443 -skip-destructive
package integration

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	baseURL        = flag.String("base-url", "http://localhost:8091", "Base URL of the server to test")
	skipDestructive = flag.Bool("skip-destructive", false, "Skip tests that modify data")
	timeout        = flag.Duration("timeout", 10*time.Second, "HTTP request timeout")
)

// httpClient creates a client that skips TLS verification for self-signed certs
func httpClient() *http.Client {
	return &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// doRequest makes an HTTP request and returns the response
func doRequest(t *testing.T, method, path string, body interface{}, headers map[string]string) (*http.Response, []byte) {
	t.Helper()

	url := *baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal body: %v", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	return resp, respBody
}

// TestMain runs setup before tests
func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

// ==============================================================
// PHASE 1: Health Endpoints
// ==============================================================

func TestHealthEndpoint(t *testing.T) {
	resp, body := doRequest(t, "GET", "/health", nil, nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Errorf("Failed to parse response: %v", err)
		return
	}

	if result["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", result["status"])
	}
}

func TestReadinessEndpoint(t *testing.T) {
	resp, body := doRequest(t, "GET", "/health/ready", nil, nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Errorf("Failed to parse response: %v", err)
		return
	}

	if result["database"] != true {
		t.Errorf("Database should be healthy")
	}
	if result["redis"] != true {
		t.Errorf("Redis should be healthy")
	}
}

func TestLivenessEndpoint(t *testing.T) {
	resp, body := doRequest(t, "GET", "/health/live", nil, nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}
}

// ==============================================================
// PHASE 2: Public API Endpoints
// ==============================================================

func TestAgentVersionEndpoint(t *testing.T) {
	resp, body := doRequest(t, "GET", "/api/agent/version", nil, nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Errorf("Failed to parse response: %v", err)
		return
	}

	if _, ok := result["version"]; !ok {
		t.Errorf("Response should contain 'version' field")
	}
}

// ==============================================================
// PHASE 3: Installation Code Flow (CRITICAL)
// ==============================================================

func TestInvalidInstallationCodeReturnsInvalid(t *testing.T) {
	// This is a CRITICAL test - the installation code flow has broken multiple times
	resp, body := doRequest(t, "GET", "/api/public/install/validate-code?code=INVALID-CODE-12345", nil, nil)

	// Acceptable responses: 200 with valid:false, or 400/404
	switch resp.StatusCode {
	case http.StatusOK:
		// Check that valid is false
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Errorf("Failed to parse response: %v", err)
			return
		}

		// Check for valid:false or status:invalid
		if valid, ok := result["valid"].(bool); ok && valid {
			t.Errorf("Invalid code should return valid:false, got valid:true")
		}
		if status, ok := result["status"].(string); ok && status == "valid" {
			t.Errorf("Invalid code should not return status:valid")
		}

	case http.StatusBadRequest, http.StatusNotFound:
		// Also acceptable - code is rejected
		t.Logf("Invalid code properly rejected with HTTP %d", resp.StatusCode)

	default:
		t.Errorf("Unexpected status code %d for invalid installation code. Body: %s", resp.StatusCode, string(body))
	}

	// CRITICAL: Check that response doesn't contain database errors
	bodyStr := strings.ToLower(string(body))
	dangerousStrings := []string{"sql", "pgx", "postgres", "database", "connection", "no rows"}
	for _, s := range dangerousStrings {
		if strings.Contains(bodyStr, s) {
			t.Errorf("Response may leak internal database details: contains '%s'. Body: %s", s, string(body))
		}
	}
}

func TestEmptyInstallationCodeHandled(t *testing.T) {
	resp, body := doRequest(t, "GET", "/api/public/install/validate-code?code=", nil, nil)

	// Should handle gracefully - either 400 or 200 with error
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		t.Errorf("Empty code should be handled gracefully, got %d. Body: %s", resp.StatusCode, string(body))
	}
}

func TestMalformedInstallationCodeHandled(t *testing.T) {
	// Test with special characters that might break SQL
	malformedCodes := []string{
		"'; DROP TABLE installation_codes; --",
		"<script>alert(1)</script>",
		"../../etc/passwd",
		"AAAA-AAAA-AAAA-AAAA-AAAA",
		strings.Repeat("A", 1000),
	}

	for _, code := range malformedCodes {
		t.Run(code[:min(20, len(code))], func(t *testing.T) {
			resp, body := doRequest(t, "GET", "/api/public/install/validate-code?code="+code, nil, nil)

			// Should not panic or return 500
			if resp.StatusCode == http.StatusInternalServerError {
				t.Errorf("Malformed code caused server error. Body: %s", string(body))
			}
		})
	}
}

// ==============================================================
// PHASE 4: Authentication Endpoints
// ==============================================================

func TestLoginEndpointRejectsInvalidCredentials(t *testing.T) {
	resp, body := doRequest(t, "POST", "/api/auth/login", map[string]string{
		"email":    "invalid@test.com",
		"password": "wrongpassword",
	}, nil)

	// Should return 400 or 401, not 500
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("Invalid login caused server error. Body: %s", string(body))
	}

	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected 400, 401, or 429 for invalid login, got %d. Body: %s", resp.StatusCode, string(body))
	}
}

func TestLoginEndpointValidatesInput(t *testing.T) {
	// Empty email
	resp, body := doRequest(t, "POST", "/api/auth/login", map[string]string{
		"email":    "",
		"password": "test",
	}, nil)

	if resp.StatusCode == http.StatusOK {
		t.Errorf("Login with empty email should fail. Body: %s", string(body))
	}

	// Empty password
	resp, body = doRequest(t, "POST", "/api/auth/login", map[string]string{
		"email":    "test@test.com",
		"password": "",
	}, nil)

	if resp.StatusCode == http.StatusOK {
		t.Errorf("Login with empty password should fail. Body: %s", string(body))
	}
}

// ==============================================================
// PHASE 5: Protected Endpoint Security
// ==============================================================

func TestProtectedEndpointsRequireAuth(t *testing.T) {
	protectedEndpoints := []string{
		"/api/devices",
		"/api/alerts",
		"/api/scripts",
		"/api/users",
		"/api/clients",
		"/api/dashboard/stats",
	}

	for _, endpoint := range protectedEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp, body := doRequest(t, "GET", endpoint, nil, nil)

			// Should return 401 or 403, NOT 200
			if resp.StatusCode == http.StatusOK {
				t.Errorf("Endpoint %s accessible without auth! Body: %s", endpoint, string(body))
			}

			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
				t.Logf("Endpoint %s returned %d (expected 401/403). Body: %s", endpoint, resp.StatusCode, string(body))
			}
		})
	}
}

// ==============================================================
// PHASE 6: Agent Endpoints
// ==============================================================

func TestAgentEnrollmentRequiresToken(t *testing.T) {
	resp, body := doRequest(t, "POST", "/api/agent/enroll", map[string]string{
		"hostname": "test-device",
	}, nil)

	// Should require enrollment token
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Errorf("Agent enrollment should require token. Body: %s", string(body))
	}

	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Logf("Agent enrollment returned %d. Body: %s", resp.StatusCode, string(body))
	}
}

func TestAgentEnrollmentWithInvalidToken(t *testing.T) {
	resp, body := doRequest(t, "POST", "/api/agent/enroll", map[string]string{
		"hostname": "test-device",
	}, map[string]string{
		"X-Enrollment-Token": "invalid-token-12345",
	})

	// Should reject invalid token
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Errorf("Agent enrollment accepted invalid token. Body: %s", string(body))
	}
}

// ==============================================================
// PHASE 7: Error Handling
// ==============================================================

func TestNonExistentResourceReturnsProperError(t *testing.T) {
	// Test with auth header to get past 401
	resp, body := doRequest(t, "GET", "/api/devices/99999999", nil, nil)

	// Should return 401 (no auth) or 404 (not found)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("Non-existent device should not return 200. Body: %s", string(body))
	}

	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("Non-existent device caused server error. Body: %s", string(body))
	}

	// Check no database details leaked
	bodyStr := strings.ToLower(string(body))
	if strings.Contains(bodyStr, "sql") || strings.Contains(bodyStr, "pgx") || strings.Contains(bodyStr, "no rows") {
		t.Errorf("Error response leaks database details. Body: %s", string(body))
	}
}

func TestServerDoesNotPanicOnMalformedJSON(t *testing.T) {
	malformedJSON := []string{
		`{"email": "test@test.com", "password":}`,
		`{{{`,
		`null`,
		`[]`,
		`{"nested": {"too": {"deep": {"for": {"comfort": true}}}}}`,
	}

	for i, payload := range malformedJSON {
		t.Run(fmt.Sprintf("malformed-%d", i), func(t *testing.T) {
			req, _ := http.NewRequest("POST", *baseURL+"/api/auth/login", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient().Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			// Should handle gracefully - no 500
			if resp.StatusCode == http.StatusInternalServerError {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("Malformed JSON caused server error. Body: %s", string(body))
			}
		})
	}
}

// ==============================================================
// Helper
// ==============================================================

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
