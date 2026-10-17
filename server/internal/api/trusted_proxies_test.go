package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func clientIPEngine(proxies []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	configureTrustedProxies(r, proxies)
	r.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

func clientIPFor(t *testing.T, r *gin.Engine, peer string, headers map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	return w.Body.String()
}

func TestClientIP_ForgedXFFFromUntrustedPeerIgnored(t *testing.T) {
	r := clientIPEngine([]string{"172.25.0.3"})
	// A sibling container on the edge network calling the backend directly.
	got := clientIPFor(t, r, "172.25.0.9:51000", map[string]string{
		"X-Forwarded-For": "203.0.113.7",
		"X-Real-IP":       "203.0.113.8",
	})
	if got != "172.25.0.9" {
		t.Fatalf("forged headers from untrusted peer were honoured: got %q, want peer 172.25.0.9", got)
	}
}

func TestClientIP_TrustedProxyXFFHonoured(t *testing.T) {
	r := clientIPEngine([]string{"172.25.0.3"})
	got := clientIPFor(t, r, "172.25.0.3:40000", map[string]string{"X-Forwarded-For": "198.51.100.20"})
	if got != "198.51.100.20" {
		t.Fatalf("got %q, want client from Traefik XFF 198.51.100.20", got)
	}
}

func TestClientIP_ClientPrependedXFFEntryIgnored(t *testing.T) {
	// The client sends its own XFF; the proxy appends the real peer. Only the
	// rightmost untrusted hop may be used, never the client-supplied left entry.
	r := clientIPEngine([]string{"172.25.0.3"})
	got := clientIPFor(t, r, "172.25.0.3:40000", map[string]string{"X-Forwarded-For": "10.0.0.1, 198.51.100.20"})
	if got != "198.51.100.20" {
		t.Fatalf("got %q, want rightmost untrusted hop 198.51.100.20", got)
	}
}

func TestClientIP_CIDRTrust(t *testing.T) {
	r := clientIPEngine([]string{"172.25.0.0/16"})
	if got := clientIPFor(t, r, "172.25.4.4:1", map[string]string{"X-Forwarded-For": "198.51.100.21"}); got != "198.51.100.21" {
		t.Fatalf("in-CIDR proxy: got %q", got)
	}
	if got := clientIPFor(t, r, "172.18.0.9:1", map[string]string{"X-Forwarded-For": "198.51.100.21"}); got != "172.18.0.9" {
		t.Fatalf("out-of-CIDR peer honoured: got %q", got)
	}
}

func TestClientIP_NoTrustedProxiesUsesPeer(t *testing.T) {
	// Default (TRUSTED_PROXIES unset) and the direct mTLS agent listener.
	for _, peer := range []string{"127.0.0.1:9", "172.25.0.3:9", "192.168.1.50:9"} {
		r := clientIPEngine(nil)
		got := clientIPFor(t, r, peer, map[string]string{
			"X-Forwarded-For":  "203.0.113.7",
			"X-Real-IP":        "203.0.113.8",
			"CF-Connecting-IP": "203.0.113.9",
		})
		want := peer[:len(peer)-2]
		if got != want {
			t.Fatalf("peer %s: got %q, want %q", peer, got, want)
		}
	}
}
