package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/middleware"
	ws "github.com/sentinel/server/internal/websocket"
)

// getUpgrader returns a WebSocket upgrader with proper origin validation
func (r *Router) getUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(req *http.Request) bool {
			// In non-production, allow all origins
			if r.config.Environment != "production" {
				return true
			}
			origin := req.Header.Get("Origin")

			// Agent endpoints - allow no Origin (native apps don't send it)
			if strings.HasPrefix(req.URL.Path, "/ws/agent") {
				return true
			}

			// Dashboard MUST have valid Origin
			if origin == "" {
				log.Printf("[SECURITY] WebSocket rejected: no Origin header from %s on %s", req.RemoteAddr, req.URL.Path)
				return false
			}

			for _, allowed := range r.config.AllowedOrigins {
				if origin == allowed {
					return true
				}
			}
			log.Printf("[SECURITY] WebSocket rejected: invalid Origin %q from %s", origin, req.RemoteAddr)
			return false
		},
	}
}



// getDeviceCertStatuses returns certificate status for all devices
func (r *Router) getDeviceCertStatuses(c *gin.Context) {
	ctx := context.Background()

	type CertStatus struct {
		AgentID       string  `json:"agentId"`
		AgentName     *string `json:"agentName"`
		CACertHash    *string `json:"caCertHash"`
		DistributedAt *string `json:"distributedAt"`
		ConfirmedAt   *string `json:"confirmedAt"`
	}

	rows, err := r.db.Pool().Query(ctx, `
		SELECT
			agent_id,
			hostname,
			ca_cert_hash,
			ca_cert_distributed_at,
			ca_cert_updated_at
		FROM devices
		WHERE agent_id IS NOT NULL
		ORDER BY hostname
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch certificate statuses"})
		return
	}
	defer rows.Close()

	var statuses []CertStatus
	for rows.Next() {
		var (
			agentID       string
			hostname      *string
			caCertHash    *string
			distributedAt *time.Time
			updatedAt     *time.Time
		)
		if err := rows.Scan(&agentID, &hostname, &caCertHash, &distributedAt, &updatedAt); err != nil {
			continue
		}

		status := CertStatus{
			AgentID:    agentID,
			AgentName:  hostname,
			CACertHash: caCertHash,
		}
		if distributedAt != nil {
			t := distributedAt.Format(time.RFC3339)
			status.DistributedAt = &t
		}
		if updatedAt != nil {
			t := updatedAt.Format(time.RFC3339)
			status.ConfirmedAt = &t
		}
		statuses = append(statuses, status)
	}

	if statuses == nil {
		statuses = []CertStatus{}
	}

	c.JSON(http.StatusOK, statuses)
}

// WebSocket Handlers

func (r *Router) handleAgentWebSocket(c *gin.Context) {
	upgrader := r.getUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// Wait for auth message with deadline to prevent resource exhaustion (H-01)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	// Clear deadline for normal operation
	conn.SetReadDeadline(time.Time{})

	var authMsg ws.Message
	if err := json.Unmarshal(message, &authMsg); err != nil || authMsg.Type != ws.MsgTypeAuth {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth message"}`)})
		conn.Close()
		return
	}

	var authPayload struct {
		AgentID       string `json:"agentId"`
		Token         string `json:"token"`
		CACertHash    string `json:"caCertHash"`
		HasClientCert bool   `json:"hasClientCert"` // Agent indicates if it already has a client certificate
		DeviceInfo    *struct {
			Hostname     string `json:"hostname"`
			Platform     string `json:"platform"`
			OSType       string `json:"osType"`
			OSVersion    string `json:"osVersion"`
			Architecture string `json:"architecture"`
			CPUModel     string `json:"cpuModel"`
			CPUCores     int    `json:"cpuCores"`
			TotalMemory  uint64 `json:"totalMemory"`
			SerialNumber string `json:"serialNumber"`
			Manufacturer string `json:"manufacturer"`
			Model        string `json:"model"`
			IPAddress    string `json:"ipAddress"`
			MACAddress   string `json:"macAddress"`
			GPU          []struct {
				Name          string `json:"name"`
				Vendor        string `json:"vendor"`
				Memory        uint64 `json:"memory"`
				DriverVersion string `json:"driver_version"`
			} `json:"gpu,omitempty"`
			Storage []struct {
				Device     string  `json:"device"`
				Mountpoint string  `json:"mountpoint"`
				FSType     string  `json:"fstype"`
				Total      uint64  `json:"total"`
				Used       uint64  `json:"used"`
				Free       uint64  `json:"free"`
				Percent    float64 `json:"percent"`
			} `json:"storage,omitempty"`
		} `json:"deviceInfo,omitempty"`
	}
	if err := json.Unmarshal(authMsg.Payload, &authPayload); err != nil {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth payload"}`)})
		conn.Close()
		return
	}

	// Verify token using shared validation (handles both legacy plaintext and bcrypt-hashed tokens)
	// H-08: ValidateDatabaseToken checks is_active = TRUE, expires_at, and max_uses,
	// so deactivating a token (is_active = FALSE) will reject reconnecting agents.
	// This applies to BOTH new enrollments and WebSocket reconnections.
	//
	// FUTURE: Add bulk device disable by enrollment token — when a token is revoked,
	// optionally disable all devices that were enrolled with that token. This would
	// handle the case where agents are already connected and won't re-authenticate
	// until their next reconnection attempt.
	tokenValid := false

	// Use the same token validation as the enrollment middleware (CW-003 compliant)
	// This handles both legacy plaintext and bcrypt-hashed tokens correctly
	if r.db.Pool() != nil {
		valid, _ := middleware.ValidateDatabaseToken(context.Background(), r.db.Pool(), authPayload.Token)
		if valid {
			tokenValid = true
		}
	}

	// Fallback to static env var token
	if !tokenValid && r.config.EnrollmentToken != "" {
		if subtle.ConstantTimeCompare([]byte(authPayload.Token), []byte(r.config.EnrollmentToken)) == 1 {
			tokenValid = true
		}
	}

	if !tokenValid {
		log.Printf("[WS] Invalid/revoked token from %s for agent %s", c.ClientIP(), authPayload.AgentID)
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid token"}`)})
		conn.Close()
		return
	}

	// Defensive: reject auth payloads with an empty agent_id. Previously these
	// would INSERT a row with agent_id='' which violates the UNIQUE expectation
	// on lookups and produced the "blank agent_id but status=online" state
	// observed on PS-BSIKORA-LT (2026-05-22). The agent must regenerate a
	// hardware-fingerprint agent_id in config.Load() — see PR #18.
	if strings.TrimSpace(authPayload.AgentID) == "" {
		log.Printf("[WS] Rejecting auth with empty agent_id from %s (ua=%q)", c.ClientIP(), c.Request.UserAgent())
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Missing agent_id — agent must regenerate fingerprint and reconnect"}`)})
		conn.Close()
		return
	}

	// Token→cert binding hardening (decision 0e6e20d7). The agent authenticated
	// with its enrollment token over this (tunnel-terminated) WS path. If it also
	// holds an active mTLS client certificate, it SHOULD be connecting via direct
	// mTLS (/ws/agent/mtls on :8443) instead — token-over-tunnel exposes it to
	// on-path / token-theft impersonation that the cert would prevent. In WARN
	// mode (default) we log + count and still allow; when EnforceAgentCertBinding
	// is on we reject and the agent must use direct mTLS. This runs before any
	// auto-enroll / cert-issuance side effects so an enforced reject is clean.
	if evaluateAgentCertBinding(context.Background(), r.db.Pool(), authPayload.AgentID, c.ClientIP(), r.config.EnforceAgentCertBinding) {
		log.Printf("[CERT-BINDING] REJECTED agent %s from %s: enforcement on and agent holds an active client cert — must use direct mTLS", authPayload.AgentID, c.ClientIP())
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Certificate-bound agent must authenticate via direct mTLS"}`)})
		conn.Close()
		return
	}

	// Get device ID - or auto-enroll if device was deleted
	ctx := context.Background()
	var deviceID uuid.UUID
	var isDisabled bool
	err = r.db.Pool().QueryRow(ctx, "SELECT id, COALESCE(is_disabled, false) FROM devices WHERE agent_id = $1 AND organization_id = $2", authPayload.AgentID, constants.CurrentOrganizationID).Scan(&deviceID, &isDisabled)
	if err != nil {
		// Device not found by agent_id - try MAC address fallback for hardware fingerprint migration
		if authPayload.DeviceInfo != nil && authPayload.DeviceInfo.MACAddress != "" {
			macErr := r.db.Pool().QueryRow(ctx,
				"SELECT id, COALESCE(is_disabled, false) FROM devices WHERE mac_address = $1",
				authPayload.DeviceInfo.MACAddress).Scan(&deviceID, &isDisabled)
			if macErr == nil {
				// Found device by MAC - update agent_id to new hardware fingerprint
				log.Printf("Migrating device %s from old agent_id to new fingerprint %s (matched by MAC %s)",
					deviceID, authPayload.AgentID, authPayload.DeviceInfo.MACAddress)
				_, updateErr := r.db.Pool().Exec(ctx,
					"UPDATE devices SET agent_id = $1 WHERE id = $2 AND organization_id = $3",
					authPayload.AgentID, deviceID, constants.CurrentOrganizationID)
				if updateErr != nil {
					log.Printf("Warning: failed to update agent_id: %v", updateErr)
				}
				err = nil // Clear error - device found
			}
		}
	}
	if err != nil {
		// Device not found - auto-enroll as a new device
		log.Printf("Device not found for agent %s, auto-enrolling...", authPayload.AgentID)
		deviceID = uuid.New()
		var insertErr error
		if authPayload.DeviceInfo != nil {
			// Use device info from agent for proper auto-enrollment
			log.Printf("Auto-enrolling with device info: hostname=%s, platform=%s",
				authPayload.DeviceInfo.Hostname, authPayload.DeviceInfo.Platform)

			// Convert GPU and Storage to JSON for database storage
			gpuJSON, _ := json.Marshal(authPayload.DeviceInfo.GPU)
			storageJSON, _ := json.Marshal(authPayload.DeviceInfo.Storage)

			_, insertErr = r.db.Pool().Exec(ctx, `
				INSERT INTO devices (id, agent_id, hostname, platform, os_type, os_version,
					architecture, cpu_model, cpu_cores, total_memory, serial_number,
					manufacturer, model, ip_address, mac_address, gpu, storage, status, created_at, last_seen,
					ca_cert_hash, ca_cert_updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 'online', NOW(), NOW(),
					NULLIF($18, ''), CASE WHEN $18 != '' THEN NOW() ELSE NULL END)
			`, deviceID, authPayload.AgentID, authPayload.DeviceInfo.Hostname,
				authPayload.DeviceInfo.Platform, authPayload.DeviceInfo.OSType, authPayload.DeviceInfo.OSVersion,
				authPayload.DeviceInfo.Architecture, authPayload.DeviceInfo.CPUModel, authPayload.DeviceInfo.CPUCores,
				authPayload.DeviceInfo.TotalMemory, authPayload.DeviceInfo.SerialNumber,
				authPayload.DeviceInfo.Manufacturer, authPayload.DeviceInfo.Model,
				authPayload.DeviceInfo.IPAddress, authPayload.DeviceInfo.MACAddress,
				gpuJSON, storageJSON, authPayload.CACertHash)
		} else {
			// Fallback to minimal enrollment
			_, insertErr = r.db.Pool().Exec(ctx, `
				INSERT INTO devices (id, agent_id, hostname, status, created_at, last_seen,
					ca_cert_hash, ca_cert_updated_at)
				VALUES ($1, $2, $3, 'online', NOW(), NOW(),
					NULLIF($4, ''), CASE WHEN $4 != '' THEN NOW() ELSE NULL END)
			`, deviceID, authPayload.AgentID, "Auto-enrolled-"+authPayload.AgentID[:8], authPayload.CACertHash)
		}
		if insertErr != nil {
			log.Printf("Failed to auto-enroll device: %v", insertErr)
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Failed to auto-enroll device"}`)})
			conn.Close()
			return
		}
		log.Printf("Auto-enrolled device %s with ID %s", authPayload.AgentID, deviceID)
	}

	// Check if device is disabled
	if isDisabled {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Device is disabled"}`)})
		conn.Close()
		return
	}

	// Build auth response - include certificate if PKI is available and agent doesn't have one
	authRespPayload := map[string]interface{}{
		"success": true,
	}

	// Issue client certificate if PKI is available and agent indicates it needs one.
	// Per-agent issuance is rate-limited (#22): bounds the cert population a
	// single agent_id can spawn, preventing a stolen-token attacker from
	// repeatedly claiming HasClientCert=false to churn fresh certs.
	if r.pki != nil && !authPayload.HasClientCert {
		allowed, retryAfter, rateErr := r.pki.CheckIssuanceRate(ctx, authPayload.AgentID)
		switch {
		case rateErr != nil:
			// Fail-closed: rate limit DB lookup failure means we can't prove
			// the agent is under budget. Don't issue. Agent falls back to
			// existing token auth which already proved valid above.
			log.Printf("[PKI] Rate limit check failed for agent %s: %v — skipping cert issuance", authPayload.AgentID, rateErr)
		case !allowed:
			log.Printf("[PKI] Agent %s over cert issuance rate limit (retry after %s) — skipping issuance, token auth still valid",
				authPayload.AgentID, retryAfter.Round(time.Second))
		default:
			log.Printf("[PKI] Issuing client certificate for agent %s", authPayload.AgentID)
			bundle, err := r.pki.IssueClientCertificate(
				ctx,
				authPayload.AgentID,
				deviceID,
				constants.CurrentOrganizationID,
				r.config.CertValidityYears,
			)
			if err != nil {
				log.Printf("[PKI] Failed to issue certificate for agent %s: %v", authPayload.AgentID, err)
				// Continue without certificate - agent can still function with token auth
			} else {
				authRespPayload["clientCert"] = bundle.ClientCert
				authRespPayload["clientKey"] = bundle.ClientKey
				authRespPayload["caCert"] = bundle.CACert
				authRespPayload["certExpiresAt"] = bundle.ExpiresAt.Format(time.RFC3339)
				authRespPayload["certSerial"] = bundle.SerialNumber
				// Log payload sizes so we can prove the response went on the wire
				// with cert bytes — used to diagnose silent agent-side install
				// failures (incident 2026-05-22, PS-BSIKORA-LT: cert recorded in DB
				// but client-cert.pem never landed on disk).
				log.Printf("[PKI] Issued certificate for agent %s, serial=%s, expires=%s, certBytes=%d, keyBytes=%d, caBytes=%d",
					authPayload.AgentID, bundle.SerialNumber, bundle.ExpiresAt.Format(time.RFC3339),
					len(bundle.ClientCert), len(bundle.ClientKey), len(bundle.CACert))
			}
		}
	}

	// Send auth response
	authRespJSON, _ := json.Marshal(authRespPayload)
	conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(authRespJSON)})

	// Register client
	client := r.hub.RegisterAgent(conn, authPayload.AgentID, deviceID)

	// Update device status, certificate hash, and hardware info (GPU/Storage) if provided
	// This ensures devices that enrolled before GPU/Storage support get updated
	if authPayload.DeviceInfo != nil && (len(authPayload.DeviceInfo.GPU) > 0 || len(authPayload.DeviceInfo.Storage) > 0) {
		gpuJSON, _ := json.Marshal(authPayload.DeviceInfo.GPU)
		storageJSON, _ := json.Marshal(authPayload.DeviceInfo.Storage)
		if authPayload.CACertHash != "" {
			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE devices SET status = 'online', last_seen = NOW(),
				ca_cert_hash = $2, ca_cert_updated_at = NOW(),
				gpu = COALESCE($3, gpu), storage = COALESCE($4, storage),
				hidden_at = NULL, hidden_by = NULL
				WHERE id = $1`, deviceID, authPayload.CACertHash, gpuJSON, storageJSON); err != nil {
				log.Printf("Error updating device %s status to online: %v", deviceID, err)
			}
		} else {
			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE devices SET status = 'online', last_seen = NOW(),
				gpu = COALESCE($2, gpu), storage = COALESCE($3, storage),
				hidden_at = NULL, hidden_by = NULL
				WHERE id = $1`, deviceID, gpuJSON, storageJSON); err != nil {
				log.Printf("Error updating device %s status to online: %v", deviceID, err)
			}
		}
	} else if authPayload.CACertHash != "" {
		if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW(), ca_cert_hash = $2, ca_cert_updated_at = NOW(), hidden_at = NULL, hidden_by = NULL WHERE id = $1 AND organization_id = $3", deviceID, authPayload.CACertHash, constants.CurrentOrganizationID); err != nil {
			log.Printf("Error updating device %s status to online: %v", deviceID, err)
		}
	} else {
		if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW(), hidden_at = NULL, hidden_by = NULL WHERE id = $1 AND organization_id = $2", deviceID, constants.CurrentOrganizationID); err != nil {
			log.Printf("Error updating device %s status to online: %v", deviceID, err)
		}
	}

	// Broadcast online status to dashboards
	onlineMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": deviceID,
		"status":   "online",
	})
	r.hub.BroadcastToDashboards(onlineMsg)

	// Auto-resolve open "Device Offline" alerts now that device is back online
	if result, err := r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'resolved', resolved_at = NOW()
		WHERE device_id = $1 AND status = 'open'
		  AND (title LIKE '%Device Offline%')
	`, deviceID); err == nil && result.RowsAffected() > 0 {
		log.Printf("Auto-resolved %d offline alert(s) for device %s on reconnect", result.RowsAffected(), deviceID)
	}

	// Auto-distribute CA certificate if agent doesn't have it or has outdated version
	go r.autoDistributeCertificate(client, authPayload.AgentID, authPayload.CACertHash)

	// Proactively notify agent of available updates on connect.
	// Agents with long heartbeat intervals may disconnect before the first heartbeat fires,
	// so we check the device's stored version and send update info immediately.
	go func() {
		var agentVersion, osType string
		if verErr := r.db.Pool().QueryRow(ctx,
			"SELECT COALESCE(agent_version, ''), COALESCE(os_type, '') FROM devices WHERE id = $1",
			deviceID).Scan(&agentVersion, &osType); verErr == nil && agentVersion != "" {
			latestVersion := getCurrentAgentVersion()
			if isNewerVersion(latestVersion, agentVersion) {
				// Wave 1 hotfix (incident df7a7ff8): gate announcement on
				// agent_releases having a row for latestVersion. Without this,
				// the v1.77.10 advert sent agents into a 401 retry-storm because
				// the release pipeline never populated agent_releases.
				releaseStatus := r.getAgentReleaseStatus(ctx)
				if !releaseStatus.HasReleaseRow {
					log.Printf("Suppressing update notification for agent %s (no agent_releases row for %s — release pipeline gap)", authPayload.AgentID, latestVersion)
				} else if osType == "linux" && !isNewerVersion(agentVersion, "1.71.99") {
					// Linux agents below v1.72.0 have completely broken self-update
					// (hardcoded Windows paths, no execute_command support). Don't send
					// update notifications — they just trigger a useless download storm.
					log.Printf("Suppressing update notification for agent %s (Linux v%s < v1.72.0, requires manual update)", authPayload.AgentID, agentVersion)
				} else if r.hasRecentUpdateFailure(ctx, authPayload.AgentID, latestVersion) {
					log.Printf("Suppressing update notification for agent %s (recent update failure, 30min cooldown)", authPayload.AgentID)
				} else {
					ackMsg, _ := json.Marshal(map[string]interface{}{
						"type": ws.MsgTypeHeartbeatAck,
						"payload": map[string]interface{}{
							"updateAvailable": true,
							"latestVersion":   latestVersion,
						},
					})
					r.hub.SendToAgent(authPayload.AgentID, ackMsg)
					log.Printf("Proactive update notification for agent %s: %s -> %s", authPayload.AgentID, agentVersion, latestVersion)

					// SEC-007: the former server-pushed force-update path
					// (sendForceUpdateCommand) has been removed. It emitted a
					// chained shell invocation (curl.exe / bash) that did NOT
					// carry the X-Enrollment-Token header. Agents discover
					// updates via their own CheckForUpdate poll cycle.
				}
			}
		}
	}()

	// Start read/write pumps
	go client.WritePump(ctx)
	client.ReadPump(ctx, func(msg []byte) {
		r.handleAgentMessage(authPayload.AgentID, deviceID, msg)
	})

	// Update device status on disconnect
	if _, err := r.db.Pool().Exec(context.Background(), "UPDATE devices SET status = 'offline' WHERE id = $1 AND organization_id = $2", deviceID, constants.CurrentOrganizationID); err != nil {
		log.Printf("Error updating device %s status to offline: %v", deviceID, err)
	}

	// Broadcast offline status to dashboards
	offlineMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": deviceID,
		"status":   "offline",
	})
	r.hub.BroadcastToDashboards(offlineMsg)
}

// SEC-007: sendForceUpdateCommand / sendWindowsForceUpdate / sendLinuxForceUpdate
// / buildLinuxForceUpdateScript were removed. They built RCE-shaped chained-shell
// update commands (curl.exe|bash → rename → copy → restart) that never carried the
// X-Enrollment-Token header, and their only call site was already disabled. The
// live, admin-gated force-update path is handleForceUpdate in devices.go, which
// sends a WebSocket "force_update" message the agent's in-process updater handles.

func (r *Router) handleAgentMessage(agentID string, deviceID uuid.UUID, message []byte) {
	var msg ws.Message
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	ctx := context.Background()

	switch msg.Type {
	case ws.MsgTypeHeartbeat:
		MetricsIncHeartbeat()
		// Parse heartbeat to get agent version + optional layer_state (recovery
		// posture self-report introduced in PR #18 for the silent-agent detector).
		var heartbeat struct {
			AgentVersion string      `json:"agentVersion"`
			LayerState   *LayerState `json:"layer_state,omitempty"`
		}
		if err := json.Unmarshal(message, &heartbeat); err != nil {
			log.Printf("Failed to unmarshal heartbeat: %v, raw: %s", err, string(message))
		}
		log.Printf("Heartbeat from %s: version=%q", agentID, heartbeat.AgentVersion)

		// Best-effort agent_health update — failures are logged but don't gate
		// the heartbeat path. Even when LayerState is nil this populates
		// last_check_in so the recovery-feed dashboard has fresh data.
		upsertAgentHealth(ctx, r.db.Pool(), agentID, deviceID, "online", heartbeat.LayerState)

		// Update last seen (and agent version if provided)
		if heartbeat.AgentVersion != "" {
			// Check for version rollback before updating
			var previousVersion string
			var hostname string
			if err := r.db.Pool().QueryRow(ctx, "SELECT COALESCE(agent_version, ''), COALESCE(hostname, '') FROM devices WHERE id = $1", deviceID).Scan(&previousVersion, &hostname); err == nil {
				// Detect rollback: if previous version is newer than current, it's a rollback
				if previousVersion != "" && isNewerVersion(previousVersion, heartbeat.AgentVersion) {
					log.Printf("ALERT: Agent rollback detected on %s (%s): %s -> %s", hostname, agentID, previousVersion, heartbeat.AgentVersion)
					r.createAgentRollbackAlert(ctx, deviceID, agentID, hostname, previousVersion, heartbeat.AgentVersion)
				}
			}

			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW(), agent_version = $1 WHERE id = $2 AND organization_id = $3", heartbeat.AgentVersion, deviceID, constants.CurrentOrganizationID); err != nil {
				log.Printf("Error updating device %s last_seen with version: %v", deviceID, err)
			}
		} else {
			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW() WHERE id = $1 AND organization_id = $2", deviceID, constants.CurrentOrganizationID); err != nil {
				log.Printf("Error updating device %s last_seen: %v", deviceID, err)
			}
		}

		// Build heartbeat ack - include update info if newer version available
		ackPayload := map[string]interface{}{}

		// Phase 6 (v1.77.30): rollout-aware dispatch overrides the global
		// "latest version" notion. If this device has a pending row in an
		// active rollout, offer that rollout's release_version regardless of
		// global latestVersion or recent-failure suppression — admin chose
		// this on purpose. Skip the legacy comparison block entirely.
		if releaseVer, rolloutID, dispatched := r.dispatchPendingRolloutForDevice(ctx, agentID, deviceID, heartbeat.AgentVersion); dispatched {
			ackPayload["updateAvailable"] = true
			ackPayload["latestVersion"] = releaseVer
			ackPayload["rolloutId"] = rolloutID
			log.Printf("[Rollouts] dispatched rollout %s -> agent %s (%s -> %s)", rolloutID, agentID, heartbeat.AgentVersion, releaseVer)
			ackMsg, _ := json.Marshal(map[string]interface{}{
				"type":    ws.MsgTypeHeartbeatAck,
				"payload": ackPayload,
			})
			r.hub.SendToAgent(agentID, ackMsg)
			return
		}

		latestVersion := getCurrentAgentVersion()
		if heartbeat.AgentVersion != "" && isNewerVersion(latestVersion, heartbeat.AgentVersion) {
			// Suppress update notification for Linux agents below v1.72.0 — their
			// self-update is broken and notifications just cause download storms.
			var deviceOSType string
			_ = r.db.Pool().QueryRow(ctx, "SELECT COALESCE(os_type, '') FROM devices WHERE id = $1", deviceID).Scan(&deviceOSType)

			// Suppress update notification for 30 minutes after a failed update attempt.
			// Without this, agents that fail an update get told to try again every heartbeat
			// (~10s), hammering the download endpoint with 429s in an endless loop.
			recentFailure := r.hasRecentUpdateFailure(ctx, agentID, latestVersion)

			// Wave 1 hotfix (incident df7a7ff8): gate on agent_releases row exists.
			releaseStatus := r.getAgentReleaseStatus(ctx)
			noRelease := !releaseStatus.HasReleaseRow

			if deviceOSType == "linux" && !isNewerVersion(heartbeat.AgentVersion, "1.71.99") {
				// Don't tell this agent about updates — it can't apply them
			} else if recentFailure {
				// Don't tell this agent about updates — it recently failed, back off
				log.Printf("Agent %s update suppressed for 30min (recent failure)", agentID)
			} else if noRelease {
				// Don't tell this agent about updates — server has no published release to serve
				log.Printf("Agent %s update suppressed (no agent_releases row for %s — release pipeline gap)", agentID, latestVersion)
			} else {
				ackPayload["updateAvailable"] = true
				ackPayload["latestVersion"] = latestVersion
			}
			suppressed := (deviceOSType == "linux" && !isNewerVersion(heartbeat.AgentVersion, "1.71.99")) || recentFailure || noRelease
			log.Printf("Agent %s has update available: %s -> %s (os=%s, suppressed=%v)", agentID, heartbeat.AgentVersion, latestVersion, deviceOSType, suppressed)
		} else if heartbeat.AgentVersion != "" {
			// Agent is at latest version — auto-resolve any stale update failure alerts
			if result, err := r.db.Pool().Exec(ctx, `
				UPDATE alerts SET status = 'resolved', resolved_at = NOW()
				WHERE device_id = $1 AND status = 'open'
				  AND (title LIKE '%Download Failed%' OR title LIKE '%Update Loop%' OR title LIKE '%Rolled Back%')
			`, deviceID); err == nil && result.RowsAffected() > 0 {
				log.Printf("Auto-resolved %d update alert(s) for agent %s (now at latest %s)", result.RowsAffected(), agentID, heartbeat.AgentVersion)
			}
		}

		// Send ack back to agent
		ackMsg, _ := json.Marshal(map[string]interface{}{
			"type":    ws.MsgTypeHeartbeatAck,
			"payload": ackPayload,
		})
		r.hub.SendToAgent(agentID, ackMsg)

	case ws.MsgTypeMetrics:
		// Agent sends metrics in "data" field with snake_case keys
		// Parse core fields for DB storage, but forward ALL fields to dashboards
		var metricsMsg struct {
			Data struct {
				CPUPercent           float64         `json:"cpu_percent"`
				CPUPerCore           []float64       `json:"cpu_per_core,omitempty"`
				MemoryPercent        float64         `json:"memory_percent"`
				MemoryUsed           uint64          `json:"memory_used"`
				MemoryAvailable      uint64          `json:"memory_available"`
				MemoryCommitted      uint64          `json:"memory_committed"`
				MemoryCached         uint64          `json:"memory_cached"`
				MemoryPagedPool      uint64          `json:"memory_paged_pool"`
				MemoryNonPagedPool   uint64          `json:"memory_non_paged_pool"`
				DiskPercent          float64         `json:"disk_percent"`
				DiskUsed             uint64          `json:"disk_used"`
				DiskTotal            uint64          `json:"disk_total"`
				DiskReadBytesPerSec  uint64          `json:"disk_read_bytes_sec"`
				DiskWriteBytesPerSec uint64          `json:"disk_write_bytes_sec"`
				NetworkRxBytes       uint64          `json:"network_rx_bytes"`
				NetworkTxBytes       uint64          `json:"network_tx_bytes"`
				ProcessCount         int             `json:"process_count"`
				Uptime               uint64          `json:"uptime"`
				TopProcesses         json.RawMessage `json:"top_processes,omitempty"`
				GPUMetrics           json.RawMessage `json:"gpu_metrics,omitempty"`
				NetworkInterfaces    json.RawMessage `json:"network_interfaces,omitempty"`
				Storage              json.RawMessage `json:"storage,omitempty"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &metricsMsg); err != nil {
			log.Printf("Error parsing metrics from %s: %v", agentID, err)
			return
		}
		m := metricsMsg.Data
		// Compute total memory from used + available
		memoryTotalBytes := int64(m.MemoryUsed + m.MemoryAvailable)

		// Insert core metrics to DB
		if _, err := r.db.Pool().Exec(ctx, `
			INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, memory_used_bytes,
				memory_total_bytes, disk_percent, disk_used_bytes, disk_total_bytes,
				network_rx_bytes, network_tx_bytes, process_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, deviceID, m.CPUPercent, m.MemoryPercent, int64(m.MemoryUsed),
			memoryTotalBytes, m.DiskPercent, int64(m.DiskUsed),
			int64(m.DiskTotal), int64(m.NetworkRxBytes), int64(m.NetworkTxBytes),
			m.ProcessCount); err != nil {
			log.Printf("Error inserting metrics for device %s: %v", deviceID, err)
		}

		// Broadcast ALL metrics to dashboards (convert to camelCase for frontend)
		metricsMap := map[string]interface{}{
			"cpuPercent":          m.CPUPercent,
			"memoryPercent":       m.MemoryPercent,
			"memoryUsedBytes":     m.MemoryUsed,
			"memoryTotalBytes":    memoryTotalBytes,
			"memoryCommitted":     m.MemoryCommitted,
			"memoryCached":        m.MemoryCached,
			"memoryPagedPool":     m.MemoryPagedPool,
			"memoryNonPagedPool":  m.MemoryNonPagedPool,
			"diskPercent":         m.DiskPercent,
			"diskUsedBytes":       m.DiskUsed,
			"diskTotalBytes":      m.DiskTotal,
			"diskReadBytesPerSec": m.DiskReadBytesPerSec,
			"diskWriteBytesPerSec": m.DiskWriteBytesPerSec,
			"networkRxBytes":      m.NetworkRxBytes,
			"networkTxBytes":      m.NetworkTxBytes,
			"processCount":        m.ProcessCount,
			"uptime":              m.Uptime,
		}
		// Include optional complex fields if present
		if len(m.CPUPerCore) > 0 {
			metricsMap["cpuPerCore"] = m.CPUPerCore
		}
		if len(m.TopProcesses) > 0 {
			metricsMap["topProcesses"] = m.TopProcesses
		}
		if len(m.GPUMetrics) > 0 {
			metricsMap["gpuMetrics"] = m.GPUMetrics
		}
		if len(m.NetworkInterfaces) > 0 {
			metricsMap["networkInterfaces"] = m.NetworkInterfaces
		}
		if len(m.Storage) > 0 {
			metricsMap["storage"] = m.Storage
		}

		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_metrics",
			"deviceId": deviceID,
			"metrics":  metricsMap,
		})
		log.Printf("[Metrics] Broadcasting device_metrics for device %s: CPU=%.1f%% MEM=%.1f%%", deviceID, m.CPUPercent, m.MemoryPercent)
		r.hub.BroadcastToDashboards(broadcastMsg)

		// Check alert rules
		r.checkAlertRules(deviceID, m.CPUPercent, m.MemoryPercent, m.DiskPercent)

	case ws.MsgTypeResponse:
		// Agent sends response data at root level, not in payload field
		// Parse from raw message instead of msg.Payload
		log.Printf("[Agent] Response received from %s: %s", agentID, string(message))
		var response struct {
			Type      string          `json:"type"`
			RequestID string          `json:"requestId"`
			Success   bool            `json:"success"`
			Data      json.RawMessage `json:"data"`
			Error     string          `json:"error"`
		}
		if err := json.Unmarshal(message, &response); err != nil {
			log.Printf("[Handler] Failed to parse response: %v", err)
			return
		}

		// Get command ID from request tracking (simplified - just extract from data)
		var data struct {
			CommandID string `json:"commandId"`
			Output    string `json:"output"`
		}
		json.Unmarshal(response.Data, &data)

		if data.CommandID != "" {
			status := "completed"
			if !response.Success {
				status = "failed"
			}

			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE commands SET status = $1, output = $2, error_message = $3, completed_at = NOW()
				WHERE id = $4
			`, status, data.Output, response.Error, data.CommandID); err != nil {
				log.Printf("Error updating command %s status: %v", data.CommandID, err)
			}
		}

		// Forward response to dashboards with requestId preserved
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeResponse,
			"requestId": response.RequestID,
			"success":   response.Success,
			"data":      response.Data,
			"error":     response.Error,
			"deviceId":  deviceID.String(),
			"agentId":   agentID,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeTerminalOutput:
		// Agent sends sessionId and data at root level, not in payload
		var termOut struct {
			SessionID string `json:"sessionId"`
			Data      string `json:"data"`
		}
		if err := json.Unmarshal(message, &termOut); err != nil {
			log.Printf("[Handler] Failed to parse terminal output: %v", err)
			return
		}

		// Forward terminal output to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeTerminalOutput,
			"deviceId":  deviceID,
			"agentId":   agentID,
			"sessionId": termOut.SessionID,
			"data":      termOut.Data,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeFileContent:
		// Forward file content to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     ws.MsgTypeFileContent,
			"deviceId": deviceID,
			"agentId":  agentID,
			"payload":  msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeScanProgress:
		// Forward scan progress to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeScanProgress,
			"deviceId":  deviceID,
			"agentId":   agentID,
			"requestId": msg.RequestID,
			"payload":   msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeWebRTCSignal:
		// Agent sends WebRTC signaling messages (ICE candidates, etc.) - forward to dashboards
		var signal struct {
			SessionID string          `json:"sessionId"`
			Signal    json.RawMessage `json:"signal"`
		}
		if err := json.Unmarshal(message, &signal); err != nil {
			log.Printf("[WebRTC] Failed to parse webrtc_signal from %s: %v", agentID, err)
			return
		}
		log.Printf("[WebRTC] Signal from agent %s for session %s", agentID, signal.SessionID)
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeWebRTCSignal,
			"deviceId":  deviceID,
			"agentId":   agentID,
			"sessionId": signal.SessionID,
			"signal":    signal.Signal,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeCertUpdateAck:
		// Agent acknowledged certificate update
		var ackData struct {
			CertHash string `json:"certHash"`
			Success  bool   `json:"success"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(message, &ackData); err != nil {
			log.Printf("[Certs] Failed to parse cert_update_ack from %s: %v", agentID, err)
			return
		}

		// Try parsing from data field (standard format)
		var dataWrapper struct {
			Data struct {
				CertHash string `json:"certHash"`
				Success  bool   `json:"success"`
				Error    string `json:"error"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &dataWrapper); err == nil && dataWrapper.Data.CertHash != "" {
			ackData.CertHash = dataWrapper.Data.CertHash
			ackData.Success = dataWrapper.Data.Success
			ackData.Error = dataWrapper.Data.Error
		}

		if ackData.Success {
			log.Printf("[Certs] Agent %s confirmed certificate update (hash: %s...)", agentID, ackData.CertHash[:8])
			// Update database to record distribution
			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE devices SET
					ca_cert_hash = $1,
					ca_cert_distributed_at = NOW(),
					ca_cert_updated_at = NOW()
				WHERE agent_id = $2
			`, ackData.CertHash, agentID); err != nil {
				log.Printf("[Certs] Failed to update device cert status for %s: %v", agentID, err)
			}
		} else {
			log.Printf("[Certs] Agent %s failed to update certificate: %s", agentID, ackData.Error)
		}

	case ws.MsgTypeUSBDeviceEvent:
		// Agent reports USB device connection/disconnection event
		r.handleUSBDeviceEvent(ctx, deviceID, message)

	case ws.MsgTypeUSBDeviceList:
		// Agent reports full list of connected USB devices
		r.handleUSBDeviceList(ctx, deviceID, message)

	case ws.MsgTypeUSBSessionComplete:
		// Agent reports USB session with file transfers
		r.handleUSBSessionComplete(ctx, deviceID, message)

	case "event", ws.MsgTypeAgentAlert, "tamper_alert":
		r.handleAgentAlert(ctx, deviceID, agentID, message)

	case ws.MsgTypeAgentLogs:
		r.handleAgentLogs(ctx, deviceID, message)

	case "update_status":
		// Agent reports self-update progress via WebSocket (also reported via REST)
		var statusMsg struct {
			Data struct {
				State   string `json:"state"`
				Version string `json:"version"`
				Error   string `json:"error"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &statusMsg); err == nil {
			log.Printf("[UpdateStatus] Agent %s: state=%s version=%s error=%q",
				agentID, statusMsg.Data.State, statusMsg.Data.Version, statusMsg.Data.Error)
		}

	default:
		log.Printf("[Agent] Unhandled message type from %s: %s", agentID, msg.Type)
	}
}

func (r *Router) checkAlertRules(deviceID uuid.UUID, cpu, memory, disk float64) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, metric, operator, threshold, severity FROM alert_rules WHERE enabled = true
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rule struct {
			ID        uuid.UUID
			Name      string
			Metric    string
			Operator  string
			Threshold float64
			Severity  string
		}
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.Severity); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}

		var value float64
		switch rule.Metric {
		case "cpu_percent":
			value = cpu
		case "memory_percent":
			value = memory
		case "disk_percent":
			value = disk
		default:
			continue
		}

		triggered := false
		switch rule.Operator {
		case "gt":
			triggered = value > rule.Threshold
		case "gte":
			triggered = value >= rule.Threshold
		case "lt":
			triggered = value < rule.Threshold
		case "lte":
			triggered = value <= rule.Threshold
		}

		if triggered {
			// Check cooldown (don't create duplicate alerts)
			var count int
			if err := r.db.Pool().QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
						WHERE device_id = $1 AND rule_id = $2 AND status != 'resolved' AND organization_id = $3
				AND created_at > NOW() - INTERVAL '15 minutes'
			`, deviceID, rule.ID, constants.CurrentOrganizationID).Scan(&count); err != nil {
				log.Printf("Error checking alert cooldown for device %s: %v", deviceID, err)
				continue
			}

			if count == 0 {
				if _, err := r.db.Pool().Exec(ctx, `
					INSERT INTO alerts (device_id, rule_id, severity, title, message, organization_id)
							VALUES ($1, $2, $3, $4, $5, $6)
				`, deviceID, rule.ID, rule.Severity, rule.Name,
					rule.Metric+" is "+rule.Operator+" "+fmt.Sprintf("%.2f", rule.Threshold),
					constants.CurrentOrganizationID); err != nil {
					log.Printf("Error creating alert for device %s: %v", deviceID, err)
				}
			}
		}
	}
}

// handleAgentAlert processes alert messages from agents (update failures, tamper events, etc.)
func (r *Router) handleAgentAlert(ctx context.Context, deviceID uuid.UUID, agentID string, message []byte) {
	// Support multiple alert formats from different agent message types
	var alertMsg struct {
		Type  string `json:"type"`
		Event struct {
			Severity string `json:"severity"`
			Title    string `json:"title"`
			Message  string `json:"message"`
		} `json:"event"`
		Data struct {
			Message   string `json:"message"`
			Timestamp string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(message, &alertMsg); err != nil {
		log.Printf("[AgentAlert] Failed to parse alert from %s: %v", agentID, err)
		return
	}

	// Normalize: tamper_alert uses data.message, event/agent_alert uses event fields
	severity := alertMsg.Event.Severity
	title := alertMsg.Event.Title
	alertMessage := alertMsg.Event.Message

	if alertMsg.Type == "tamper_alert" {
		severity = "critical"
		title = "Tamper Detection"
		alertMessage = alertMsg.Data.Message
	}

	if severity == "" {
		severity = "warning"
	}
	if title == "" {
		title = "Agent Alert"
	}
	if alertMessage == "" {
		log.Printf("[AgentAlert] Empty alert message from %s, ignoring", agentID)
		return
	}

	log.Printf("[AgentAlert] %s from %s: [%s] %s - %s", alertMsg.Type, agentID, severity, title, alertMessage)

	// Get hostname for alert context
	var hostname string
	r.db.Pool().QueryRow(ctx, "SELECT COALESCE(hostname, '') FROM devices WHERE id = $1", deviceID).Scan(&hostname)

	// Deduplicate: skip if identical open alert exists
	var existingCount int
	r.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM alerts
		WHERE device_id = $1 AND title = $2 AND status != 'resolved'
		AND created_at > NOW() - INTERVAL '15 minutes'
	`, deviceID, title).Scan(&existingCount)
	if existingCount > 0 {
		log.Printf("[AgentAlert] Duplicate alert suppressed for %s: %s", agentID, title)
		return
	}

	// Create alert in DB
	alertID := uuid.New()
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO alerts (id, device_id, severity, title, message, status, organization_id, created_at)
		VALUES ($1, $2, $3, $4, $5, 'open', $6, NOW())
	`, alertID, deviceID, severity, title, alertMessage, constants.CurrentOrganizationID)
	if err != nil {
		log.Printf("[AgentAlert] Failed to insert alert for %s: %v", agentID, err)
		return
	}

	// Broadcast to dashboards
	if r.hub != nil {
		dashMsg, _ := json.Marshal(map[string]interface{}{
			"type": "new_alert",
			"alert": map[string]interface{}{
				"id":        alertID,
				"deviceId":  deviceID,
				"hostname":  hostname,
				"severity":  severity,
				"title":     title,
				"message":   alertMessage,
				"status":    "open",
				"createdAt": time.Now(),
			},
		})
		r.hub.BroadcastToDashboards(dashMsg)
	}
}

// handleAgentLogs processes batched log entries from agents
func (r *Router) handleAgentLogs(ctx context.Context, deviceID uuid.UUID, message []byte) {
	var logMsg struct {
		Type string `json:"type"`
		Logs []struct {
			Level    string                 `json:"level"`
			Source   string                 `json:"source"`
			Message  string                 `json:"message"`
			Metadata map[string]interface{} `json:"metadata,omitempty"`
			LoggedAt time.Time              `json:"loggedAt"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(message, &logMsg); err != nil {
		log.Printf("[AgentLogs] Failed to parse logs from device %s: %v", deviceID, err)
		return
	}

	// Limit to 100 entries per message to prevent abuse
	if len(logMsg.Logs) > 100 {
		logMsg.Logs = logMsg.Logs[:100]
	}

	for _, entry := range logMsg.Logs {
		metadataJSON, _ := json.Marshal(entry.Metadata)
		level := entry.Level
		if level == "" {
			level = "info"
		}
		source := entry.Source
		if source == "" {
			source = "agent"
		}
		loggedAt := entry.LoggedAt
		if loggedAt.IsZero() {
			loggedAt = time.Now()
		}

		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO agent_logs (device_id, level, source, message, metadata, logged_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, deviceID, level, source, entry.Message, metadataJSON, loggedAt)
		if err != nil {
			log.Printf("[AgentLogs] Failed to insert log for device %s: %v", deviceID, err)
			break // Stop on first error to avoid flooding logs
		}
	}
}

// getDeviceLogs returns paginated agent logs for a device
func (r *Router) getDeviceLogs(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	// Parse query parameters
	level := c.Query("level")
	source := c.Query("source")
	since := c.Query("since")
	until := c.Query("until")
	limitStr := c.DefaultQuery("limit", "100")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 1000 {
		limit = 100
	}

	// Build query dynamically
	query := `SELECT id, device_id, level, source, message, metadata, logged_at, received_at
		FROM agent_logs WHERE device_id = $1`
	args := []interface{}{deviceID}
	argIdx := 2

	if level != "" {
		query += fmt.Sprintf(" AND level = $%d", argIdx)
		args = append(args, level)
		argIdx++
	}
	if source != "" {
		query += fmt.Sprintf(" AND source = $%d", argIdx)
		args = append(args, source)
		argIdx++
	}
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err == nil {
			query += fmt.Sprintf(" AND logged_at >= $%d", argIdx)
			args = append(args, t)
			argIdx++
		}
	}
	if until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err == nil {
			query += fmt.Sprintf(" AND logged_at <= $%d", argIdx)
			args = append(args, t)
			argIdx++
		}
	}

	query += fmt.Sprintf(" ORDER BY logged_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.db.Pool().Query(c.Request.Context(), query, args...)
	if err != nil {
		log.Printf("[AgentLogs] Query failed for device %s: %v", deviceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query logs"})
		return
	}
	defer rows.Close()

	type LogEntry struct {
		ID         uuid.UUID              `json:"id"`
		DeviceID   uuid.UUID              `json:"deviceId"`
		Level      string                 `json:"level"`
		Source     string                 `json:"source"`
		Message    string                 `json:"message"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
		LoggedAt   time.Time              `json:"loggedAt"`
		ReceivedAt time.Time              `json:"receivedAt"`
	}

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		var metadataBytes []byte
		if err := rows.Scan(&entry.ID, &entry.DeviceID, &entry.Level, &entry.Source,
			&entry.Message, &metadataBytes, &entry.LoggedAt, &entry.ReceivedAt); err != nil {
			log.Printf("[AgentLogs] Scan error: %v", err)
			continue
		}
		if len(metadataBytes) > 0 {
			json.Unmarshal(metadataBytes, &entry.Metadata)
		}
		logs = append(logs, entry)
	}

	if logs == nil {
		logs = []LogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  logs,
		"count": len(logs),
	})
}

// handleUSBDeviceEvent processes USB device connection/disconnection events from agents
func (r *Router) handleUSBDeviceEvent(ctx context.Context, deviceID uuid.UUID, message []byte) {
	var eventMsg struct {
		Data struct {
			EventType string `json:"eventType"`
			Device    struct {
				DeviceID       string    `json:"deviceId"`
				InstancePath   string    `json:"instancePath"`
				VendorID       string    `json:"vendorId"`
				ProductID      string    `json:"productId"`
				SerialNumber   string    `json:"serialNumber"`
				Manufacturer   string    `json:"manufacturer"`
				ProductName    string    `json:"productName"`
				DeviceClass    string    `json:"deviceClass"`
				ClassCode      int       `json:"classCode"`
				SubclassCode   int       `json:"subclassCode"`
				ProtocolCode   int       `json:"protocolCode"`
				BusNumber      int       `json:"busNumber"`
				PortNumber     int       `json:"portNumber"`
				DeviceSpeed    string    `json:"deviceSpeed"`
				ParentDevice   string    `json:"parentDevice"`
				DriveLetter    string    `json:"driveLetter"`
				MountPoint     string    `json:"mountPoint"`
				VolumeLabel    string    `json:"volumeLabel"`
				FileSystem     string    `json:"fileSystem"`
				TotalSize      int64     `json:"totalSize"`
				FreeSpace      int64     `json:"freeSpace"`
				IsConnected    bool      `json:"isConnected"`
				ConnectionTime string    `json:"connectionTime"`
				IsRemovable    bool      `json:"isRemovable"`
				IsBootable     bool      `json:"isBootable"`
				IsEncrypted    bool      `json:"isEncrypted"`
			} `json:"device"`
			Timestamp    string `json:"timestamp"`
			SecurityRisk string `json:"securityRisk"`
		} `json:"data"`
	}

	if err := json.Unmarshal(message, &eventMsg); err != nil {
		log.Printf("[USB] Failed to parse USB device event: %v", err)
		return
	}

	device := eventMsg.Data.Device
	log.Printf("[USB] Event %s from device %s: %s (%s %s) - Risk: %s",
		eventMsg.Data.EventType, deviceID, device.DeviceID,
		device.Manufacturer, device.ProductName, eventMsg.Data.SecurityRisk)

	// Parse connection time
	connTime, _ := time.Parse(time.RFC3339, device.ConnectionTime)
	if connTime.IsZero() {
		connTime = time.Now()
	}

	// Upsert device record
	if eventMsg.Data.EventType == "connected" {
		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO usb_devices (
				device_id, usb_device_id, instance_path,
				vendor_id, product_id, serial_number,
				manufacturer, product_name, device_class,
				class_code, subclass_code, protocol_code,
				bus_number, port_number, device_speed, parent_device,
				drive_letter, mount_point, volume_label, file_system,
				total_size, free_space,
				is_connected, connection_time,
				is_removable, is_bootable, is_encrypted
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
			ON CONFLICT (device_id, usb_device_id) DO UPDATE SET
				manufacturer = EXCLUDED.manufacturer,
				product_name = EXCLUDED.product_name,
				drive_letter = EXCLUDED.drive_letter,
				mount_point = EXCLUDED.mount_point,
				volume_label = EXCLUDED.volume_label,
				file_system = EXCLUDED.file_system,
				total_size = EXCLUDED.total_size,
				free_space = EXCLUDED.free_space,
				is_connected = true,
				connection_time = EXCLUDED.connection_time,
				disconnection_time = NULL,
				updated_at = NOW()
		`,
			deviceID, device.DeviceID, device.InstancePath,
			device.VendorID, device.ProductID, device.SerialNumber,
			device.Manufacturer, device.ProductName, device.DeviceClass,
			device.ClassCode, device.SubclassCode, device.ProtocolCode,
			device.BusNumber, device.PortNumber, device.DeviceSpeed, device.ParentDevice,
			device.DriveLetter, device.MountPoint, device.VolumeLabel, device.FileSystem,
			device.TotalSize, device.FreeSpace,
			true, connTime,
			device.IsRemovable, device.IsBootable, device.IsEncrypted,
		)
		if err != nil {
			log.Printf("[USB] Failed to upsert USB device: %v", err)
		}
	} else if eventMsg.Data.EventType == "disconnected" {
		_, err := r.db.Pool().Exec(ctx, `
			UPDATE usb_devices SET
				is_connected = false,
				disconnection_time = NOW(),
				updated_at = NOW()
			WHERE device_id = $1 AND usb_device_id = $2
		`, deviceID, device.DeviceID)
		if err != nil {
			log.Printf("[USB] Failed to update USB device disconnection: %v", err)
		}
	}

	// Record event in audit log
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO usb_device_events (
			device_id, event_type,
			vendor_id, product_id, serial_number,
			manufacturer, product_name, device_class,
			drive_letter, mount_point, volume_label, total_size
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		deviceID, eventMsg.Data.EventType,
		device.VendorID, device.ProductID, device.SerialNumber,
		device.Manufacturer, device.ProductName, device.DeviceClass,
		device.DriveLetter, device.MountPoint, device.VolumeLabel, device.TotalSize,
	)
	if err != nil {
		log.Printf("[USB] Failed to record USB device event: %v", err)
	}

	// Check if device is approved
	isApproved := r.checkUSBDeviceApproval(ctx, deviceID, device.VendorID, device.ProductID, device.SerialNumber)

	// Generate alert for mass storage devices or high-risk events
	shouldAlert := false
	alertSeverity := "info"
	alertTitle := ""
	alertMessage := ""

	if eventMsg.Data.EventType == "connected" {
		switch eventMsg.Data.SecurityRisk {
		case "critical":
			shouldAlert = true
			alertSeverity = "critical"
			alertTitle = "Bootable USB Device Connected"
			alertMessage = fmt.Sprintf("Bootable USB storage device connected: %s (%s). This poses a high security risk.",
				device.ProductName, device.VolumeLabel)
		case "high":
			shouldAlert = !isApproved
			alertSeverity = "warning"
			alertTitle = "USB Storage Device Connected"
			alertMessage = fmt.Sprintf("USB storage device connected: %s (%s) - %s",
				device.ProductName, device.VolumeLabel, device.DriveLetter)
		case "medium":
			shouldAlert = !isApproved
			alertSeverity = "warning"
			alertTitle = "USB Network Adapter Connected"
			alertMessage = fmt.Sprintf("USB network device connected: %s (%s)",
				device.Manufacturer, device.ProductName)
		}
	}

	if shouldAlert {
		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO alerts (device_id, severity, title, message, organization_id)
			VALUES ($1, $2, $3, $4, (SELECT organization_id FROM devices WHERE id = $1))
		`, deviceID, alertSeverity, alertTitle, alertMessage)
		if err != nil {
			log.Printf("[USB] Failed to create alert: %v", err)
		}
	}

	// Broadcast to dashboards
	broadcastMsg, _ := json.Marshal(map[string]interface{}{
		"type":     ws.MsgTypeUSBDeviceEvent,
		"deviceId": deviceID,
		"data":     eventMsg.Data,
	})
	r.hub.BroadcastToDashboards(broadcastMsg)
}

// handleUSBDeviceList processes full USB device list from agents
func (r *Router) handleUSBDeviceList(ctx context.Context, deviceID uuid.UUID, message []byte) {
	var listMsg struct {
		RequestID string `json:"requestId"`
		Data      struct {
			Devices   []json.RawMessage `json:"devices"`
			Count     int               `json:"count"`
			Timestamp string            `json:"timestamp"`
		} `json:"data"`
	}

	if err := json.Unmarshal(message, &listMsg); err != nil {
		log.Printf("[USB] Failed to parse USB device list: %v", err)
		return
	}

	log.Printf("[USB] Received device list from %s: %d devices", deviceID, listMsg.Data.Count)

	// Forward to dashboards (the request may have come from a dashboard)
	broadcastMsg, _ := json.Marshal(map[string]interface{}{
		"type":      ws.MsgTypeUSBDeviceList,
		"deviceId":  deviceID,
		"requestId": listMsg.RequestID,
		"data":      listMsg.Data,
	})
	r.hub.BroadcastToDashboards(broadcastMsg)
}

// checkUSBDeviceApproval checks if a USB device is in the approved list
func (r *Router) checkUSBDeviceApproval(ctx context.Context, deviceID uuid.UUID, vendorID, productID, serialNumber string) bool {
	var count int
	err := r.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM usb_approved_devices
		WHERE (expires_at IS NULL OR expires_at > NOW())
		AND (
			(serial_number IS NOT NULL AND serial_number = $1)
			OR (vendor_id = $2 AND product_id = $3)
			OR (vendor_id = $2 AND product_id IS NULL)
		)
		AND (device_id IS NULL OR device_id = $4)
	`, serialNumber, vendorID, productID, deviceID).Scan(&count)

	if err != nil {
		log.Printf("[USB] Failed to check device approval: %v", err)
		return false
	}

	return count > 0
}

// handleUSBSessionComplete processes USB session complete with file transfers
func (r *Router) handleUSBSessionComplete(ctx context.Context, deviceID uuid.UUID, message []byte) {
	var sessionMsg struct {
		Data struct {
			SessionID      string `json:"sessionId"`
			USBDeviceID    string `json:"usbDeviceId"`
			DisconnectTime string `json:"disconnectTime"`
			FileCount      int    `json:"fileCount"`
			FileTransfers  []struct {
				FileName     string `json:"fileName"`
				FilePath     string `json:"filePath"`
				FileSize     int64  `json:"fileSize"`
				TransferTime string `json:"transferTime"`
				Operation    string `json:"operation"`
			} `json:"fileTransfers"`
		} `json:"data"`
	}

	if err := json.Unmarshal(message, &sessionMsg); err != nil {
		log.Printf("[USB] Failed to parse USB session complete: %v", err)
		return
	}

	data := sessionMsg.Data
	log.Printf("[USB] Session %s complete for device %s: %d file transfers",
		data.SessionID, deviceID, data.FileCount)

	// Parse session ID
	sessionUUID, err := uuid.Parse(data.SessionID)
	if err != nil {
		log.Printf("[USB] Invalid session ID: %v", err)
		return
	}

	// Insert file transfers into database
	for _, transfer := range data.FileTransfers {
		transferTime, _ := time.Parse(time.RFC3339, transfer.TransferTime)
		if transferTime.IsZero() {
			transferTime = time.Now()
		}

		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO usb_file_transfers (
				device_id, usb_device_id, session_id,
				file_name, file_path, file_size, transfer_time, operation
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
			deviceID, data.USBDeviceID, sessionUUID,
			transfer.FileName, transfer.FilePath, transfer.FileSize,
			transferTime, transfer.Operation,
		)
		if err != nil {
			log.Printf("[USB] Failed to insert file transfer record: %v", err)
		}
	}

	// Update the most recent USB alert for this device with sessionId in metadata
	if data.FileCount > 0 {
		metadata := map[string]interface{}{
			"sessionId":   data.SessionID,
			"fileCount":   data.FileCount,
			"usbDeviceId": data.USBDeviceID,
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Find the most recent USB-related alert for this device and update its metadata
		_, err := r.db.Pool().Exec(ctx, `
			UPDATE alerts
			SET metadata = $1
			WHERE device_id = $2
			AND title LIKE '%USB%'
			AND status != 'resolved'
			AND created_at > NOW() - INTERVAL '1 hour'
			AND (metadata IS NULL OR metadata = '{}'::jsonb)
		`, metadataJSON, deviceID)
		if err != nil {
			log.Printf("[USB] Failed to update alert metadata: %v", err)
		}
	}

	// Broadcast to dashboards
	broadcastMsg, _ := json.Marshal(map[string]interface{}{
		"type":           ws.MsgTypeUSBSessionComplete,
		"deviceId":       deviceID,
		"sessionId":      data.SessionID,
		"usbDeviceId":    data.USBDeviceID,
		"fileCount":      data.FileCount,
		"disconnectTime": data.DisconnectTime,
	})
	r.hub.BroadcastToDashboards(broadcastMsg)
}

func (r *Router) handleDashboardWebSocket(c *gin.Context) {
	log.Printf("[DashboardWS] New connection attempt from %s", c.ClientIP())

	var userID uuid.UUID
	authenticated := false

	// Try Authorization header first (preferred)
	var tokenString string
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			tokenString = parts[1]
		}
	}
	// Fall back to query parameter (deprecated)
	if tokenString == "" {
		tokenString = c.Query("token")
		if tokenString != "" {
			log.Printf("[DEPRECATION] Token in query string from %s path=%s — use Authorization header", c.ClientIP(), c.Request.URL.Path)
		}
	}

	if tokenString != "" {
		claims, err := middleware.ValidateJWT(tokenString, r.config.JWTSecret)
		if err != nil {
			log.Printf("[DashboardWS] Invalid token from query/header: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}
		userID = claims.UserID
		authenticated = true
		log.Printf("[DashboardWS] Authenticated via query/header for user %s", userID)
	}

	// Upgrade to WebSocket
	upgrader := r.getUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[DashboardWS] Upgrade failed: %v", err)
		return
	}

	// If not authenticated via query/header, wait for first-message auth
	if !authenticated {
		log.Printf("[DashboardWS] No token in query/header, waiting for first-message auth from %s", c.ClientIP())

		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[DashboardWS] Failed to read auth message: %v", err)
			conn.Close()
			return
		}

		var authMsg struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(msg, &authMsg); err != nil || authMsg.Type != "auth" || authMsg.Token == "" {
			log.Printf("[DashboardWS] Invalid auth message from %s", c.ClientIP())
			conn.WriteJSON(map[string]string{"type": "auth_error", "error": "Invalid auth message"})
			conn.Close()
			return
		}

		claims, err := middleware.ValidateJWT(authMsg.Token, r.config.JWTSecret)
		if err != nil {
			log.Printf("[DashboardWS] Invalid token in auth message: %v", err)
			conn.WriteJSON(map[string]string{"type": "auth_error", "error": "Invalid token"})
			conn.Close()
			return
		}

		userID = claims.UserID
		authenticated = true
		log.Printf("[DashboardWS] Authenticated via first-message for user %s", userID)

		// Reset read deadline
		conn.SetReadDeadline(time.Time{})
	}

	log.Printf("[DashboardWS] Connection established for user %s", userID)
	ctx := context.Background()

	client := r.hub.RegisterDashboard(conn, userID)

	go client.WritePump(ctx)
	client.ReadPump(ctx, func(msg []byte) {
		r.handleDashboardMessage(userID, msg)
	})
	log.Printf("[DashboardWS] Connection closed for user %s", userID)
}

// Scripts handlers
func (r *Router) listScripts(c *gin.Context) {
	ctx := context.Background()

	log.Printf("[DEBUG] listScripts called, organization_id=%v, type=%T", constants.CurrentOrganizationID, constants.CurrentOrganizationID)

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, description, language, content, os_types, created_at, updated_at
		FROM scripts WHERE organization_id = $1 ORDER BY name
	`, constants.CurrentOrganizationID)
	if err != nil {
		log.Printf("[ERROR] listScripts query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scripts"})
		return
	}
	defer rows.Close()

	scripts := make([]map[string]interface{}, 0)
	for rows.Next() {
		var s struct {
			ID          uuid.UUID
			Name        string
			Description *string
			Language    string
			Content     string
			OSTypes     []string
			CreatedAt   time.Time
			UpdatedAt   time.Time
		}
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt); err != nil {
			log.Printf("Error scanning script row: %v", err)
			continue
		}
		scripts = append(scripts, map[string]interface{}{
			"id":          s.ID,
			"name":        s.Name,
			"description": s.Description,
			"language":    s.Language,
			"content":     s.Content,
			"osTypes":     s.OSTypes,
			"createdAt":   s.CreatedAt,
			"updatedAt":   s.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, scripts)
}

func (r *Router) createScript(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Description string   `json:"description"`
		Language    string   `json:"language" binding:"required"`
		Content     string   `json:"content" binding:"required"`
		OSTypes     []string `json:"osTypes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	validLanguages := map[string]bool{"powershell": true, "bash": true, "python": true, "batch": true}
	if !validLanguages[req.Language] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language"})
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO scripts (name, description, language, content, os_types, created_by, organization_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, req.Name, req.Description, req.Language, req.Content, req.OSTypes, userID, constants.CurrentOrganizationID).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create script"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "language": req.Language})
}

func (r *Router) getScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	ctx := context.Background()
	var s struct {
		ID          uuid.UUID
		Name        string
		Description *string
		Language    string
		Content     string
		OSTypes     []string
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, language, content, os_types, created_at, updated_at
		FROM scripts WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": s.ID, "name": s.Name, "description": s.Description,
		"language": s.Language, "content": s.Content, "osTypes": s.OSTypes,
		"createdAt": s.CreatedAt, "updatedAt": s.UpdatedAt,
	})
}

func (r *Router) updateScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Language    string   `json:"language"`
		Content     string   `json:"content"`
		OSTypes     []string `json:"osTypes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE scripts SET name = COALESCE(NULLIF($1, ''), name),
		description = COALESCE(NULLIF($2, ''), description),
		language = COALESCE(NULLIF($3, ''), language),
		content = COALESCE(NULLIF($4, ''), content),
		os_types = COALESCE($5, os_types), updated_at = NOW()
		WHERE id = $6
	`, req.Name, req.Description, req.Language, req.Content, req.OSTypes, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update script"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Script updated successfully"})
}

func (r *Router) deleteScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM scripts WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete script"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Script deleted successfully"})
}

func (r *Router) executeScript(c *gin.Context) {
	scriptID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceId" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()
	var script struct {
		Language string
		Content  string
	}

	err = r.db.Pool().QueryRow(ctx, "SELECT language, content FROM scripts WHERE id = $1 AND organization_id = $2", scriptID, constants.CurrentOrganizationID).Scan(&script.Language, &script.Content)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	var commandID uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO commands (device_id, created_by, command_type, command, status, organization_id)
			VALUES ($1, $2, $3, $4, 'pending', $5) RETURNING id
	`, deviceID, userID, script.Language, script.Content, constants.CurrentOrganizationID).Scan(&commandID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create command"})
		return
	}

	// Agents register a handler on MsgTypeExecuteScript ("execute_script") that reads
	// data["script"] and data["language"] (see agent/cmd/sentinel-agent/main.go).
	// The previous payload here used type="execute" with a payload-wrapped shape that
	// no agent handler ever consumed — every dispatch was a silent no-op until 2026-05-06.
	msg, _ := json.Marshal(map[string]interface{}{
		"type":      "execute_script",
		"requestId": commandID.String(),
		"data": map[string]interface{}{
			"commandId": commandID.String(),
			"script":    script.Content,
			"language":  script.Language,
		},
	})

	var agentID string
	if err := r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", deviceID, constants.CurrentOrganizationID).Scan(&agentID); err != nil {
		log.Printf("Error looking up agent ID for device %s: %v", deviceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find device agent"})
		return
	}
	if err := r.hub.SendToAgent(agentID, msg); err != nil {
		log.Printf("Error sending script to agent %s: %v", agentID, err)
	}

	c.JSON(http.StatusOK, gin.H{"commandId": commandID, "message": "Script execution started"})
}

// Alerts handlers
func (r *Router) listAlerts(c *gin.Context) {
	ctx := context.Background()
	status := c.Query("status")

	query := `
		SELECT a.id, a.device_id, d.hostname, a.rule_id, a.severity, a.title, a.message,
			   a.status, a.acknowledged_at, a.resolved_at, a.created_at
		FROM alerts a
		LEFT JOIN devices d ON a.device_id = d.id
		WHERE a.organization_id = $1
	`
	args := []interface{}{constants.CurrentOrganizationID}

	if status != "" {
		query += " AND a.status = $2"
		args = append(args, status)
	}
	query += " ORDER BY a.created_at DESC LIMIT 100"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alerts"})
		return
	}
	defer rows.Close()

	alerts := make([]map[string]interface{}, 0)
	for rows.Next() {
		var a struct {
			ID             uuid.UUID
			DeviceID       uuid.UUID
			DeviceName     *string
			RuleID         *uuid.UUID
			Severity       string
			Title          string
			Message        *string
			Status         string
			AcknowledgedAt *time.Time
			ResolvedAt     *time.Time
			CreatedAt      time.Time
		}
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity,
			&a.Title, &a.Message, &a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt); err != nil {
			log.Printf("Error scanning alert row: %v", err)
			continue
		}

		alert := map[string]interface{}{
			"id":        a.ID,
			"deviceId":  a.DeviceID,
			"severity":  a.Severity,
			"title":     a.Title,
			"status":    a.Status,
			"createdAt": a.CreatedAt,
		}
		if a.DeviceName != nil {
			alert["deviceName"] = *a.DeviceName
		}
		if a.Message != nil {
			alert["message"] = *a.Message
		}
		alerts = append(alerts, alert)
	}

	c.JSON(http.StatusOK, alerts)
}

func (r *Router) getAlert(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID"})
		return
	}

	ctx := context.Background()
	var a struct {
		ID             uuid.UUID
		DeviceID       uuid.UUID
		DeviceName     *string
		RuleID         *uuid.UUID
		Severity       string
		Title          string
		Message        *string
		Status         string
		AcknowledgedAt *time.Time
		ResolvedAt     *time.Time
		CreatedAt      time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT a.id, a.device_id, d.hostname, a.rule_id, a.severity, a.title, a.message,
			   a.status, a.acknowledged_at, a.resolved_at, a.created_at
		FROM alerts a LEFT JOIN devices d ON a.device_id = d.id WHERE a.id = $1 AND a.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity, &a.Title, &a.Message,
		&a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": a.ID, "deviceId": a.DeviceID, "deviceName": a.DeviceName,
		"severity": a.Severity, "title": a.Title, "message": a.Message,
		"status": a.Status, "acknowledgedAt": a.AcknowledgedAt,
		"resolvedAt": a.ResolvedAt, "createdAt": a.CreatedAt,
	})
}

func (r *Router) acknowledgeAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2 AND organization_id = $3
		`, userID, id, constants.CurrentOrganizationID); err != nil {
		log.Printf("Error acknowledging alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to acknowledge alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert acknowledged"})
}

func (r *Router) resolveAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	ctx := context.Background()

	if _, err := r.db.Pool().Exec(ctx, "UPDATE alerts SET status = 'resolved', resolved_at = NOW() WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID); err != nil {
		log.Printf("Error resolving alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert resolved"})
}

// Alert rules handlers
func (r *Router) listAlertRules(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, description, enabled, metric, operator, threshold, severity,
			   cooldown_minutes, created_at
		FROM alert_rules WHERE organization_id = $1 ORDER BY name
	`, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alert rules"})
		return
	}
	defer rows.Close()

	rules := make([]map[string]interface{}, 0)
	for rows.Next() {
		var rule struct {
			ID              uuid.UUID
			Name            string
			Description     *string
			Enabled         bool
			Metric          string
			Operator        string
			Threshold       float64
			Severity        string
			CooldownMinutes int
			CreatedAt       time.Time
		}
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
			&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}
		rules = append(rules, map[string]interface{}{
			"id":              rule.ID,
			"name":            rule.Name,
			"description":     rule.Description,
			"enabled":         rule.Enabled,
			"metric":          rule.Metric,
			"operator":        rule.Operator,
			"threshold":       rule.Threshold,
			"severity":        rule.Severity,
			"cooldownMinutes": rule.CooldownMinutes,
			"createdAt":       rule.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, rules)
}

func (r *Router) createAlertRule(c *gin.Context) {
	var req struct {
		Name            string  `json:"name" binding:"required"`
		Description     string  `json:"description"`
		Metric          string  `json:"metric" binding:"required"`
		Operator        string  `json:"operator" binding:"required"`
		Threshold       float64 `json:"threshold" binding:"required"`
		Severity        string  `json:"severity" binding:"required"`
		CooldownMinutes int     `json:"cooldownMinutes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO alert_rules (name, description, metric, operator, threshold, severity, cooldown_minutes, organization_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id
	`, req.Name, req.Description, req.Metric, req.Operator, req.Threshold, req.Severity, req.CooldownMinutes, constants.CurrentOrganizationID).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create alert rule"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) getAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	ctx := context.Background()
	var rule struct {
		ID              uuid.UUID
		Name            string
		Description     *string
		Enabled         bool
		Metric          string
		Operator        string
		Threshold       float64
		Severity        string
		CooldownMinutes int
		CreatedAt       time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, enabled, metric, operator, threshold, severity, cooldown_minutes, created_at
		FROM alert_rules WHERE id = $1 AND organization_id = $2
	`, id, constants.CurrentOrganizationID).Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
		&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert rule not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": rule.ID, "name": rule.Name, "description": rule.Description,
		"enabled": rule.Enabled, "metric": rule.Metric, "operator": rule.Operator,
		"threshold": rule.Threshold, "severity": rule.Severity,
		"cooldownMinutes": rule.CooldownMinutes, "createdAt": rule.CreatedAt,
	})
}

func (r *Router) updateAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	var req struct {
		Name            string  `json:"name"`
		Description     string  `json:"description"`
		Enabled         *bool   `json:"enabled"`
		Metric          string  `json:"metric"`
		Operator        string  `json:"operator"`
		Threshold       float64 `json:"threshold"`
		Severity        string  `json:"severity"`
		CooldownMinutes int     `json:"cooldownMinutes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE alert_rules SET
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			metric = COALESCE(NULLIF($3, ''), metric),
			operator = COALESCE(NULLIF($4, ''), operator),
			severity = COALESCE(NULLIF($5, ''), severity),
			updated_at = NOW()
		WHERE id = $6 AND organization_id = $7
	`, req.Name, req.Description, req.Metric, req.Operator, req.Severity, id, constants.CurrentOrganizationID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update alert rule"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert rule updated successfully"})
}

func (r *Router) deleteAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM alert_rules WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete alert rule"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert rule not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert rule deleted successfully"})
}

// Settings handlers
func (r *Router) getSettings(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			log.Printf("Error scanning settings row: %v", err)
			continue
		}
		settings[key] = value
	}

	c.JSON(http.StatusOK, settings)
}

func (r *Router) updateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := context.Background()
	for key, value := range req {
		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
		`, key, value)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

// Users handlers
func (r *Router) listUsers(c *gin.Context) {
	ctx := context.Background()

	log.Printf("[DEBUG] listUsers called, organization_id=%v, type=%T", constants.CurrentOrganizationID, constants.CurrentOrganizationID)

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, email, first_name, last_name, role, is_active, last_login, created_at
		FROM users WHERE organization_id = $1 ORDER BY email
	`, constants.CurrentOrganizationID)
	if err != nil {
		log.Printf("[ERROR] listUsers query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var u struct {
			ID        uuid.UUID
			Email     string
			FirstName *string
			LastName  *string
			Role      string
			IsActive  bool
			LastLogin *time.Time
			CreatedAt time.Time
		}
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role,
			&u.IsActive, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Printf("Error scanning user row: %v", err)
			continue
		}
		users = append(users, map[string]interface{}{
			"id":        u.ID,
			"email":     u.Email,
			"firstName": u.FirstName,
			"lastName":  u.LastName,
			"role":      u.Role,
			"isActive":  u.IsActive,
			"lastLogin": u.LastLogin,
			"createdAt": u.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, users)
}

func (r *Router) createUser(c *gin.Context) {
	var req struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Role      string `json:"role"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	// Validate password complexity
	if err := validatePassword(req.Password); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Hash password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `INSERT INTO users (email, password_hash, first_name, last_name, role, organization_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, req.Email, hashedPassword, req.FirstName, req.LastName, req.Role).Scan(&id)

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":        id,
		"email":     req.Email,
		"firstName": req.FirstName,
		"lastName":  req.LastName,
		"role":      req.Role,
	})
}

func (r *Router) updateUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req struct {
		Email     string `json:"email"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Role      string `json:"role"`
		Password  string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := context.Background()

	// DC-001 FIX: Get old role before update to detect privilege changes
	var oldRole string
	if req.Role != "" {
		err = r.db.Pool().QueryRow(ctx, "SELECT role FROM users WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&oldRole)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
	}

	// Build dynamic update query
	updates := make([]string, 0)
	args := make([]interface{}, 0)
	argNum := 1

	if req.Email != "" {
		updates = append(updates, "email = $"+strconv.Itoa(argNum))
		args = append(args, req.Email)
		argNum++
	}
	if req.FirstName != "" {
		updates = append(updates, "first_name = $"+strconv.Itoa(argNum))
		args = append(args, req.FirstName)
		argNum++
	}
	if req.LastName != "" {
		updates = append(updates, "last_name = $"+strconv.Itoa(argNum))
		args = append(args, req.LastName)
		argNum++
	}
	if req.Role != "" {
		updates = append(updates, "role = $"+strconv.Itoa(argNum))
		args = append(args, req.Role)
		argNum++
	}
	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}
		hashedPassword, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		updates = append(updates, "password_hash = $"+strconv.Itoa(argNum))
		args = append(args, hashedPassword)
		argNum++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(updates, ", ") + ", updated_at = NOW() WHERE id = $" + strconv.Itoa(argNum)

	_, err = r.db.Pool().Exec(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	// DC-001 FIX: Rotate CSRF token on privilege escalation
	// This prevents session fixation attacks when user privileges change
	if req.Role != "" && req.Role != oldRole {
		newCSRFToken := middleware.RotateCSRFToken(c)
		log.Printf("Rotated CSRF token for user %s due to role change: %s -> %s", id, oldRole, req.Role)
		c.JSON(http.StatusOK, gin.H{
			"message":   "User updated successfully",
			"csrfToken": newCSRFToken,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})
}

func (r *Router) deleteUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	ctx := context.Background()

	// Soft delete by setting is_active to false
	_, err = r.db.Pool().Exec(ctx, "UPDATE users SET is_active = false, updated_at = NOW() WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// Dashboard stats handler
func (r *Router) getDashboardStats(c *gin.Context) {
	ctx := context.Background()

	stats := make(map[string]interface{})

	// Total devices
	var totalDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE organization_id = $1", constants.CurrentOrganizationID).Scan(&totalDevices); err != nil {
		log.Printf("Error getting total devices count: %v", err)
	}
	stats["totalDevices"] = totalDevices

	// Online devices
	var onlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'online'").Scan(&onlineDevices); err != nil {
		log.Printf("Error getting online devices count: %v", err)
	}
	stats["onlineDevices"] = onlineDevices

	// Offline devices
	var offlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'offline'").Scan(&offlineDevices); err != nil {
		log.Printf("Error getting offline devices count: %v", err)
	}
	stats["offlineDevices"] = offlineDevices

	// Critical alerts
	var criticalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'critical' AND status = 'open'").Scan(&criticalAlerts); err != nil {
		log.Printf("Error getting critical alerts count: %v", err)
	}
	stats["criticalAlerts"] = criticalAlerts

	// Warning alerts
	var warningAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'warning' AND status = 'open'").Scan(&warningAlerts); err != nil {
		log.Printf("Error getting warning alerts count: %v", err)
	}
	stats["warningAlerts"] = warningAlerts

	// Total alerts
	var totalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE status = 'open'").Scan(&totalAlerts); err != nil {
		log.Printf("Error getting total alerts count: %v", err)
	}
	stats["totalAlerts"] = totalAlerts

	// Total scripts
	var totalScripts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM scripts").Scan(&totalScripts); err != nil {
		log.Printf("Error getting total scripts count: %v", err)
	}
	stats["totalScripts"] = totalScripts

	// Total users
	var totalUsers int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&totalUsers); err != nil {
		log.Printf("Error getting total users count: %v", err)
	}
	stats["totalUsers"] = totalUsers

	c.JSON(http.StatusOK, stats)
}

// getCertificateInfo returns information about the server's TLS certificates
func (r *Router) getCertificateInfo(c *gin.Context) {
	type CertInfo struct {
		Name            string  `json:"name"`
		Type            string  `json:"type"`
		Path            string  `json:"path"`
		Exists          bool    `json:"exists"`
		Subject         *string `json:"subject,omitempty"`
		Issuer          *string `json:"issuer,omitempty"`
		ValidFrom       *string `json:"validFrom,omitempty"`
		ValidTo         *string `json:"validTo,omitempty"`
		Fingerprint     *string `json:"fingerprint,omitempty"`
		SerialNumber    *string `json:"serialNumber,omitempty"`
		DaysUntilExpiry *int    `json:"daysUntilExpiry,omitempty"`
		Status          string  `json:"status"`
	}

	type CertListResult struct {
		Certificates []CertInfo `json:"certificates"`
		CertsDir     string     `json:"certsDir"`
		CACertHash   *string    `json:"caCertHash,omitempty"`
	}

	// Get certs directory - check Docker mount first, then relative path
	certsDir := "/certs"
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		certsDir = "./certs" // Fallback for local development
	}

	certificates := []CertInfo{}
	var caCertHash *string

	// Define certificate files to check
	certFiles := []struct {
		name     string
		certType string
		filename string
	}{
		{"CA Certificate", "ca", "ca-cert.pem"},
		{"Server Certificate", "server", "server-cert.pem"},
	}

	for _, cf := range certFiles {
		certPath := certsDir + "/" + cf.filename
		cert := CertInfo{
			Name:   cf.name,
			Type:   cf.certType,
			Path:   certPath,
			Exists: false,
			Status: "missing",
		}

		// Try to read and parse the certificate
		pemData, err := readCertFile(certPath)
		if err == nil && len(pemData) > 0 {
			cert.Exists = true
			info := parseCertInfo(pemData)
			if info != nil {
				cert.Subject = info.Subject
				cert.Issuer = info.Issuer
				cert.ValidFrom = info.ValidFrom
				cert.ValidTo = info.ValidTo
				cert.Fingerprint = info.Fingerprint
				cert.SerialNumber = info.SerialNumber
				cert.DaysUntilExpiry = info.DaysUntilExpiry

				// Determine status
				if info.DaysUntilExpiry != nil {
					if *info.DaysUntilExpiry <= 0 {
						cert.Status = "expired"
					} else if *info.DaysUntilExpiry <= 30 {
						cert.Status = "expiring_soon"
					} else {
						cert.Status = "valid"
					}
				}

				// Get CA cert hash for comparison with agents
				if cf.certType == "ca" && info.Fingerprint != nil {
					caCertHash = info.Fingerprint
				}
			}
		}

		certificates = append(certificates, cert)
	}

	c.JSON(http.StatusOK, CertListResult{
		Certificates: certificates,
		CertsDir:     certsDir,
		CACertHash:   caCertHash,
	})
}

// Helper struct for parsed certificate info
type parsedCertInfo struct {
	Subject         *string
	Issuer          *string
	ValidFrom       *string
	ValidTo         *string
	Fingerprint     *string
	SerialNumber    *string
	DaysUntilExpiry *int
}

// readCertFile reads a PEM certificate file
func readCertFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// parseCertInfo parses PEM data and extracts certificate information
func parseCertInfo(pemData []byte) *parsedCertInfo {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	// Calculate fingerprint (SHA256)
	hash := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(hash[:])

	// Calculate days until expiry
	daysUntil := int(time.Until(cert.NotAfter).Hours() / 24)

	// Format dates
	validFrom := cert.NotBefore.Format(time.RFC3339)
	validTo := cert.NotAfter.Format(time.RFC3339)

	// Format subject and issuer
	subject := cert.Subject.String()
	issuer := cert.Issuer.String()

	// Format serial number
	serial := cert.SerialNumber.Text(16)

	return &parsedCertInfo{
		Subject:         &subject,
		Issuer:          &issuer,
		ValidFrom:       &validFrom,
		ValidTo:         &validTo,
		Fingerprint:     &fingerprint,
		SerialNumber:    &serial,
		DaysUntilExpiry: &daysUntil,
	}
}

// autoDistributeCertificate sends CA cert to agent if they don't have it or it's outdated
func (r *Router) autoDistributeCertificate(client *ws.Client, agentID string, agentCertHash string) {
	// Read current CA certificate
	certsDir := "/certs"
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		certsDir = "./certs"
	}

	caCertPath := certsDir + "/ca-cert.pem"
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		log.Printf("[AutoCert] Failed to read CA cert for agent %s: %v", agentID, err)
		return
	}

	// Calculate current CA cert hash
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		log.Printf("[AutoCert] Failed to decode CA cert PEM for agent %s", agentID)
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		log.Printf("[AutoCert] Failed to parse CA cert for agent %s: %v", agentID, err)
		return
	}

	hash := sha256.Sum256(cert.Raw)
	currentHash := hex.EncodeToString(hash[:])

	// Compare with agent's cert hash
	if agentCertHash == currentHash {
		log.Printf("[AutoCert] Agent %s already has current CA cert (hash: %s...)", agentID, currentHash[:8])
		return
	}

	// Agent needs the certificate - send it
	if agentCertHash == "" {
		log.Printf("[AutoCert] Agent %s has no CA cert, distributing (hash: %s...)", agentID, currentHash[:8])
	} else {
		log.Printf("[AutoCert] Agent %s has outdated CA cert (%s...), updating to %s...",
			agentID, agentCertHash[:8], currentHash[:8])
	}

	// Send update_certificate message
	updateMsg := map[string]interface{}{
		"type": "update_certificate",
		"data": map[string]interface{}{
			"certType":    "ca",
			"certContent": string(caCertPEM),
			"certHash":    currentHash,
		},
	}

	msgBytes, err := json.Marshal(updateMsg)
	if err != nil {
		log.Printf("[AutoCert] Failed to marshal cert update message for agent %s: %v", agentID, err)
		return
	}

	if err := client.Send(msgBytes); err != nil {
		log.Printf("[AutoCert] Failed to send cert to agent %s: %v", agentID, err)
		return
	}

	log.Printf("[AutoCert] CA certificate sent to agent %s", agentID)
}

// createAgentRollbackAlert creates a critical alert when an agent version rolls back
// and automatically sends a remediation command to fix the watchdog
func (r *Router) createAgentRollbackAlert(ctx context.Context, deviceID uuid.UUID, agentID, hostname, previousVersion, currentVersion string) {
	alertID := uuid.New()
	title := "Agent Update Rolled Back"
	message := fmt.Sprintf("Agent on %s rolled back from version %s to %s. This may indicate an update failure or compatibility issue.", hostname, previousVersion, currentVersion)

	// Check if there's already an open rollback alert for this device
	var existingCount int
	if err := r.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM alerts
		WHERE device_id = $1 AND title = $2 AND status = 'open'
	`, deviceID, title).Scan(&existingCount); err == nil && existingCount > 0 {
		// Already have an open rollback alert for this device
		// But still try to send the remediation command
		r.sendWatchdogFixCommand(ctx, deviceID, agentID, hostname)
		return
	}

	// Create the alert
	if _, err := r.db.Pool().Exec(ctx, `
		INSERT INTO alerts (id, device_id, severity, title, message, status, created_at)
		VALUES ($1, $2, 'critical', $3, $4, 'open', NOW())
	`, alertID, deviceID, title, message); err != nil {
		log.Printf("Error creating rollback alert for device %s: %v", deviceID, err)
		return
	}

	log.Printf("Created rollback alert for device %s: %s -> %s", hostname, previousVersion, currentVersion)

	// AUTO-REMEDIATION: Send command to update the watchdog
	r.sendWatchdogFixCommand(ctx, deviceID, agentID, hostname)

	// Broadcast the alert to dashboards
	if r.hub != nil {
		alertMsg, _ := json.Marshal(map[string]interface{}{
			"type": "new_alert",
			"alert": map[string]interface{}{
				"id":        alertID,
				"deviceId":  deviceID,
				"hostname":  hostname,
				"severity":  "critical",
				"title":     title,
				"message":   message,
				"status":    "open",
				"createdAt": time.Now(),
			},
		})
		r.hub.BroadcastToDashboards(alertMsg)
	}
}

// sendWatchdogFixCommand sends a platform-appropriate command to fix the agent on a device
// This is auto-remediation for the rollback issue caused by old watchdog settings
func (r *Router) sendWatchdogFixCommand(ctx context.Context, deviceID uuid.UUID, agentID, hostname string) {
	if r.hub == nil || !r.hub.IsAgentOnline(agentID) {
		log.Printf("[AutoFix] Cannot send watchdog fix to %s - agent offline", hostname)
		return
	}

	// Check device platform to send appropriate command
	var platform string
	r.db.Pool().QueryRow(ctx, "SELECT COALESCE(platform, '') FROM devices WHERE id = $1", deviceID).Scan(&platform)

	var fixCommand, commandType string
	platformLower := strings.ToLower(platform)
	switch {
	case strings.Contains(platformLower, "linux") || strings.Contains(platformLower, "ubuntu") ||
		strings.Contains(platformLower, "debian") || strings.Contains(platformLower, "centos"):
		// Linux: restart the agent service
		fixCommand = `systemctl restart sentinel-agent`
		commandType = "bash"
	case platform == "":
		// Unknown platform - skip auto-fix to avoid sending wrong command
		log.Printf("[AutoFix] Skipping fix for %s - unknown platform (cannot determine OS)", hostname)
		return
	default:
		// Windows: PowerShell command to update the watchdog
		fixCommand = `net stop SentinelWatchdog; Start-Sleep 2; $p='C:\Program Files\Sentinel Agent\sentinel-watchdog.exe'; if(Test-Path 'C:\Program Files\Sentinel\sentinel-watchdog.exe'){$p='C:\Program Files\Sentinel\sentinel-watchdog.exe'}; Invoke-WebRequest 'https://sentinelrmm.us/installers/sentinel-watchdog-windows-amd64.exe' -OutFile $p; net start SentinelWatchdog; echo $p`
		commandType = "powershell"
	}

	commandID := uuid.New()
	requestID := uuid.New().String()

	// Record the command in the database
	if _, err := r.db.Pool().Exec(ctx, `
		INSERT INTO commands (id, device_id, command_type, command, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', NULL)
	`, commandID, deviceID, commandType, fixCommand); err != nil {
		log.Printf("[AutoFix] Failed to record fix command for %s: %v", hostname, err)
	}

	// Send the command to the agent
	msg := ws.Message{
		Type:      ws.MsgTypeCommand,
		RequestID: requestID,
		Payload: json.RawMessage(mustMarshal(map[string]interface{}{
			"commandId":   commandID.String(),
			"command":     fixCommand,
			"commandType": commandType,
		})),
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		log.Printf("[AutoFix] Failed to send watchdog fix command to %s: %v", hostname, err)
		return
	}

	log.Printf("[AutoFix] Sent watchdog fix command to %s (commandId: %s)", hostname, commandID)

	// Update command status to running
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
	`, commandID); err != nil {
		log.Printf("[AutoFix] Error updating command %s status: %v", commandID, err)
	}
}
