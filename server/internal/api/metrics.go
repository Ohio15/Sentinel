package api

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sentinel/server/pkg/database"
)

var (
	agentConnected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel_agent_connected",
			Help: "1 if devices.status='online', 0 otherwise. Per-agent label set.",
		},
		[]string{"agent_id", "hostname"},
	)

	agentLastSeenSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel_agent_last_seen_seconds",
			Help: "Seconds since the agent's last_seen heartbeat was recorded.",
		},
		[]string{"agent_id", "hostname"},
	)

	certExpiresInDays = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel_cert_expires_in_days",
			Help: "Days until the agent's mTLS client cert expires. Negative once expired.",
		},
		[]string{"agent_id", "hostname"},
	)

	agentVersionInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel_agent_version",
			Help: "Constant 1; the running agent version is carried on the version label.",
		},
		[]string{"agent_id", "hostname", "version"},
	)

	// agentLegacyCount counts active (non-disabled) agents running a version older
	// than safeSelfUpdateMinVersion. v1.77.4 and earlier predate the CheckForUpdate
	// polling loop in agent/internal/updater; those builds cannot self-update and
	// require manual reinstall. Surfacing the count lets us alert when a ghost
	// reconnects (security-adjacent: those agents have known-broken auth/dispatch).
	agentLegacyCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel_agents_legacy_count",
			Help: "Count of non-disabled agents running a legacy version that cannot self-update. Labeled by version.",
		},
		[]string{"version"},
	)

	heartbeatTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "sentinel_heartbeat_total",
			Help: "Cumulative agent heartbeats received since this server process started.",
		},
	)

	websocketActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "sentinel_websocket_active",
			Help: "Currently active agent WebSocket connections on this backend instance.",
		},
	)
)

func MetricsIncHeartbeat()              { heartbeatTotal.Inc() }
func MetricsSetWebsocketActive(n int)   { websocketActive.Set(float64(n)) }

// safeSelfUpdateMinVersion is the lowest agent version that contains the
// CheckForUpdate polling loop (agent/internal/updater). Anything below this
// is "legacy": cannot self-update, must be reinstalled manually.
const safeSelfUpdateMinVersion = "1.77.5"

func metricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}

// startMetricsRefresher reads the devices table on a 15s cadence and refreshes the
// per-agent gauges. Reset() before each refresh ensures disabled or removed agents
// stop emitting time series. Cardinality is bounded by active fleet size, which is
// the right shape for fleets in the low-hundreds; revisit if the fleet grows past ~500.
func startMetricsRefresher(ctx context.Context, db *database.DB, hub WebSocketHub) {
	refresh := func() {
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		rows, err := db.Pool().Query(qctx, `
			SELECT agent_id, COALESCE(hostname, ''), COALESCE(agent_version, ''),
			       COALESCE(status, ''), last_seen, client_cert_expires_at
			FROM devices
			WHERE is_disabled = false
		`)
		if err != nil {
			log.Printf("[metrics] devices query failed: %v", err)
			return
		}
		defer rows.Close()

		agentConnected.Reset()
		agentLastSeenSeconds.Reset()
		certExpiresInDays.Reset()
		agentVersionInfo.Reset()
		agentLegacyCount.Reset()

		now := time.Now()
		legacyByVersion := map[string]int{}
		for rows.Next() {
			var (
				agentID, hostname, version, status string
				lastSeen, certExpires              *time.Time
			)
			if err := rows.Scan(&agentID, &hostname, &version, &status, &lastSeen, &certExpires); err != nil {
				log.Printf("[metrics] scan failed: %v", err)
				continue
			}
			connected := 0.0
			if status == "online" {
				connected = 1.0
			}
			agentConnected.WithLabelValues(agentID, hostname).Set(connected)
			agentVersionInfo.WithLabelValues(agentID, hostname, version).Set(1)
			if lastSeen != nil {
				agentLastSeenSeconds.WithLabelValues(agentID, hostname).Set(now.Sub(*lastSeen).Seconds())
			}
			if certExpires != nil {
				certExpiresInDays.WithLabelValues(agentID, hostname).Set(certExpires.Sub(now).Hours() / 24)
			}
			if version != "" && isNewerVersion(safeSelfUpdateMinVersion, version) {
				legacyByVersion[version]++
			}
		}
		for ver, n := range legacyByVersion {
			agentLegacyCount.WithLabelValues(ver).Set(float64(n))
		}
		if hub != nil {
			websocketActive.Set(float64(hub.ActiveAgentCount()))
		}
	}

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		refresh()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}
