package api

import (
	"log"

	"github.com/gin-gonic/gin"
)

// configureTrustedProxies restricts which direct peers may assert the client
// IP through X-Forwarded-For / X-Real-IP. Gin's default trusts every peer, so
// any caller could forge c.ClientIP(), which keys the agent-update cooldown,
// rate limiters, the cert-binding check and the link/code access audit rows.
//
// proxies must already be validated (config.Load rejects malformed entries).
// An empty list trusts no proxy: c.ClientIP() is the TCP peer address.
func configureTrustedProxies(r *gin.Engine, proxies []string) {
	// TrustedPlatform reads a CDN-specific header unconditionally from any
	// peer, so it must stay unset.
	r.TrustedPlatform = ""
	if len(proxies) == 0 {
		if err := r.SetTrustedProxies(nil); err != nil {
			log.Fatalf("[TrustedProxies] failed to disable proxy trust: %v", err)
		}
		return
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		// Unreachable when config.Load validated the list; fail closed anyway
		// rather than serve with Gin's trust-everyone default.
		log.Fatalf("[TrustedProxies] invalid TRUSTED_PROXIES %v: %v", proxies, err)
	}
	log.Printf("[TrustedProxies] honouring forwarded client IP only from %v", proxies)
}
