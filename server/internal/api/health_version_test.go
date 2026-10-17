package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/internal/version"
	"github.com/sentinel/server/pkg/config"
)

// nexus-deploy fails (and rolls back) a release unless GET /health reports
// `version` equal to the deployed tag; both listeners must carry it.
func healthVersion(t *testing.T, r http.Handler) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", w.Code)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if body.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", body.Status)
	}
	return body.Version
}

func TestMainListenerHealthReportsVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", liveness)
	if got := healthVersion(t, r); got != version.Version || got == "" {
		t.Fatalf("version = %q, want %q", got, version.Version)
	}
}

func TestAgentMTLSHealthReportsVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewAgentMTLSRouter(&Services{Config: &config.Config{}}, middleware.NewAgentRateLimiter())
	if got := healthVersion(t, r); got != version.Version || got == "" {
		t.Fatalf("version = %q, want %q", got, version.Version)
	}
}
