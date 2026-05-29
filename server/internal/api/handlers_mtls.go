// Package api provides HTTP handlers for the Sentinel server API.
// This file contains handlers for mTLS-authenticated agent connections.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/pki"
	ws "github.com/sentinel/server/internal/websocket"
)

// handleAgentWebSocketMTLS handles WebSocket connections from agents using mTLS.
// The agent's identity is extracted from the client certificate's Common Name.
func handleAgentWebSocketMTLS(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract client certificate from TLS connection
		if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
			log.Printf("[mTLS] Connection rejected: no client certificate")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client certificate required"})
			return
		}

		clientCert := c.Request.TLS.PeerCertificates[0]
		agentID := pki.GetAgentIDFromCert(clientCert)
		serialNumber := pki.GetSerialFromCert(clientCert)

		if agentID == "" {
			log.Printf("[mTLS] Connection rejected: empty agent ID in certificate CN")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid certificate: missing agent ID"})
			return
		}

		log.Printf("[mTLS] Agent %s connecting with certificate serial=%s", agentID, serialNumber)

		// Check if certificate is revoked
		if services.PKI != nil {
			revoked, err := services.PKI.IsCertificateRevoked(c.Request.Context(), serialNumber)
			if err != nil {
				log.Printf("[mTLS] Warning: Failed to check certificate revocation: %v", err)
				// Continue - database lookup failure shouldn't block connection
			} else if revoked {
				log.Printf("[mTLS] Connection rejected: certificate revoked for agent %s", agentID)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Certificate has been revoked"})
				return
			}
		}

		// Look up device by agent_id
		ctx := context.Background()
		var deviceID uuid.UUID
		var isDisabled bool
		err := services.DB.Pool().QueryRow(ctx,
			"SELECT id, COALESCE(is_disabled, false) FROM devices WHERE agent_id = $1 AND organization_id = $2",
			agentID, constants.CurrentOrganizationID,
		).Scan(&deviceID, &isDisabled)

		if err != nil {
			log.Printf("[mTLS] Device not found for agent %s: %v", agentID, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown device"})
			return
		}

		if isDisabled {
			log.Printf("[mTLS] Connection rejected: device disabled for agent %s", agentID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Device is disabled"})
			return
		}

		// Upgrade to WebSocket
		upgrader := websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(req *http.Request) bool {
				return true // Agents don't send Origin headers
			},
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[mTLS] WebSocket upgrade failed for agent %s: %v", agentID, err)
			return
		}

		// Skip auth message exchange - certificate IS the authentication
		// Send immediate auth success
		authResponse := map[string]interface{}{
			"type": ws.MsgTypeAuthResponse,
			"payload": map[string]interface{}{
				"success":     true,
				"mtlsAuth":    true,
				"certSerial":  serialNumber,
				"certExpires": clientCert.NotAfter.Format(time.RFC3339),
			},
		}
		if err := conn.WriteJSON(authResponse); err != nil {
			log.Printf("[mTLS] Failed to send auth response: %v", err)
			conn.Close()
			return
		}

		log.Printf("[mTLS] Agent %s authenticated via certificate", agentID)

		// Register client with hub
		client := services.Hub.RegisterAgent(conn, agentID, deviceID)

		// Update device status to online
		if _, err := services.DB.Pool().Exec(ctx,
			"UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1",
			deviceID,
		); err != nil {
			log.Printf("[mTLS] Error updating device %s status: %v", deviceID, err)
		}

		// Broadcast online status to dashboards
		onlineMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_status",
			"deviceId": deviceID,
			"status":   "online",
		})
		services.Hub.BroadcastToDashboards(onlineMsg)

		// Create router for message handling
		router := &Router{
			config: services.Config,
			db:     services.DB.AsDB(),
			cache:  services.Redis,
			hub:    services.Hub,
		}

		// Start read/write pumps
		go client.WritePump(ctx)
		client.ReadPump(ctx, func(msg []byte) {
			router.handleAgentMessage(agentID, deviceID, msg)
		})

		// Update device status on disconnect
		if _, err := services.DB.Pool().Exec(context.Background(),
			"UPDATE devices SET status = 'offline' WHERE id = $1",
			deviceID,
		); err != nil {
			log.Printf("[mTLS] Error updating device %s status to offline: %v", deviceID, err)
		}

		// Broadcast offline status
		offlineMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_status",
			"deviceId": deviceID,
			"status":   "offline",
		})
		services.Hub.BroadcastToDashboards(offlineMsg)
	}
}

// handleAgentWebSocketWithCerts wraps the standard agent WebSocket handler with
// certificate issuance support. When PKI is enabled and an agent doesn't have
// a certificate, one will be issued in the auth response.
func handleAgentWebSocketWithCerts(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		upgrader := websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(req *http.Request) bool {
				if services.Config.Environment != "production" {
					return true
				}
				origin := req.Header.Get("Origin")
				if origin == "" {
					return true
				}
				for _, allowed := range services.Config.AllowedOrigins {
					if origin == allowed {
						return true
					}
				}
				return false
			},
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}

		// Wait for auth message
		_, message, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			return
		}

		var authMsg ws.Message
		if err := json.Unmarshal(message, &authMsg); err != nil || authMsg.Type != ws.MsgTypeAuth {
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth message"}`)})
			conn.Close()
			return
		}

		var authPayload struct {
			AgentID    string `json:"agentId"`
			Token      string `json:"token"`
			CACertHash string `json:"caCertHash"`
			DeviceInfo *struct {
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
			} `json:"deviceInfo,omitempty"`
			HasClientCert bool `json:"hasClientCert"` // Agent indicates if it already has a cert
		}
		if err := json.Unmarshal(authMsg.Payload, &authPayload); err != nil {
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth payload"}`)})
			conn.Close()
			return
		}

		// Verify token against database
		tokenValid := false
		var tokenID uuid.UUID
		var isActive bool
		var expiresAt *time.Time

		err = services.DB.Pool().QueryRow(context.Background(), `
			SELECT id, is_active, expires_at
			FROM enrollment_tokens WHERE token = $1
		`, authPayload.Token).Scan(&tokenID, &isActive, &expiresAt)

		if err == nil {
			if !isActive {
				conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Token is disabled"}`)})
				conn.Close()
				return
			}
			if expiresAt != nil && time.Now().After(*expiresAt) {
				conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Token has expired"}`)})
				conn.Close()
				return
			}
			tokenValid = true
		} else {
			// Check legacy env var
			if services.Config.EnrollmentToken != "" && subtle.ConstantTimeCompare([]byte(authPayload.Token), []byte(services.Config.EnrollmentToken)) == 1 {
				tokenValid = true
			}
		}

		if !tokenValid {
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid token"}`)})
			conn.Close()
			return
		}

		// Get or create device
		ctx := context.Background()
		var deviceID uuid.UUID
		var isDisabled bool
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT id, COALESCE(is_disabled, false) FROM devices WHERE agent_id = $1 AND organization_id = $2",
			authPayload.AgentID, constants.CurrentOrganizationID,
		).Scan(&deviceID, &isDisabled)

		if err != nil {
			// Auto-enroll new device
			log.Printf("Device not found for agent %s, auto-enrolling...", authPayload.AgentID)
			deviceID = uuid.New()
			var insertErr error
			if authPayload.DeviceInfo != nil {
				_, insertErr = services.DB.Pool().Exec(ctx, `
					INSERT INTO devices (id, agent_id, hostname, platform, os_type, os_version,
						architecture, cpu_model, cpu_cores, total_memory, serial_number,
						manufacturer, model, ip_address, mac_address, status, created_at, last_seen,
						ca_cert_hash, ca_cert_updated_at, organization_id)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'online', NOW(), NOW(),
						NULLIF($16, ''), CASE WHEN $16 != '' THEN NOW() ELSE NULL END, $17)
				`, deviceID, authPayload.AgentID, authPayload.DeviceInfo.Hostname,
					authPayload.DeviceInfo.Platform, authPayload.DeviceInfo.OSType, authPayload.DeviceInfo.OSVersion,
					authPayload.DeviceInfo.Architecture, authPayload.DeviceInfo.CPUModel, authPayload.DeviceInfo.CPUCores,
					authPayload.DeviceInfo.TotalMemory, authPayload.DeviceInfo.SerialNumber,
					authPayload.DeviceInfo.Manufacturer, authPayload.DeviceInfo.Model,
					authPayload.DeviceInfo.IPAddress, authPayload.DeviceInfo.MACAddress,
					authPayload.CACertHash, constants.CurrentOrganizationID)
			} else {
				_, insertErr = services.DB.Pool().Exec(ctx, `
					INSERT INTO devices (id, agent_id, hostname, status, created_at, last_seen,
						ca_cert_hash, ca_cert_updated_at, organization_id)
					VALUES ($1, $2, $3, 'online', NOW(), NOW(),
						NULLIF($4, ''), CASE WHEN $4 != '' THEN NOW() ELSE NULL END, $5)
				`, deviceID, authPayload.AgentID, "Auto-enrolled-"+authPayload.AgentID[:8],
					authPayload.CACertHash, constants.CurrentOrganizationID)
			}
			if insertErr != nil {
				log.Printf("Failed to auto-enroll device: %v", insertErr)
				conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Failed to auto-enroll device"}`)})
				conn.Close()
				return
			}
			log.Printf("Auto-enrolled device %s with ID %s", authPayload.AgentID, deviceID)
		}

		if isDisabled {
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Device is disabled"}`)})
			conn.Close()
			return
		}

		// Build auth response
		authRespPayload := map[string]interface{}{
			"success": true,
		}

		// Issue client certificate if PKI is available and agent doesn't have one.
		// Per-agent issuance is rate-limited (#22): bounds the cert population a
		// single agent_id can spawn, preventing a stolen-token attacker from
		// repeatedly claiming HasClientCert=false to churn fresh certs.
		if services.PKI != nil && !authPayload.HasClientCert {
			allowed, retryAfter, rateErr := services.PKI.CheckIssuanceRate(ctx, authPayload.AgentID)
			switch {
			case rateErr != nil:
				// Fail-closed: rate limit DB lookup failure means we can't
				// prove the agent is under budget. Don't issue.
				log.Printf("[PKI] Rate limit check failed for agent %s: %v — skipping cert issuance", authPayload.AgentID, rateErr)
			case !allowed:
				log.Printf("[PKI] Agent %s over cert issuance rate limit (retry after %s) — skipping issuance, token auth still valid",
					authPayload.AgentID, retryAfter.Round(time.Second))
			default:
				log.Printf("[PKI] Issuing client certificate for agent %s", authPayload.AgentID)
				bundle, err := services.PKI.IssueClientCertificate(
					ctx,
					authPayload.AgentID,
					deviceID,
					constants.CurrentOrganizationID,
					services.Config.CertValidityYears,
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
					log.Printf("[PKI] Issued certificate for agent %s, serial=%s, expires=%s",
						authPayload.AgentID, bundle.SerialNumber, bundle.ExpiresAt.Format(time.RFC3339))
				}
			}
		}

		// Send auth response
		authRespJSON, _ := json.Marshal(authRespPayload)
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(authRespJSON)})

		// Register client
		client := services.Hub.RegisterAgent(conn, authPayload.AgentID, deviceID)

		// Update device status
		if authPayload.CACertHash != "" {
			if _, err := services.DB.Pool().Exec(ctx,
				"UPDATE devices SET status = 'online', last_seen = NOW(), ca_cert_hash = $2, ca_cert_updated_at = NOW() WHERE id = $1",
				deviceID, authPayload.CACertHash,
			); err != nil {
				log.Printf("Error updating device %s status: %v", deviceID, err)
			}
		} else {
			if _, err := services.DB.Pool().Exec(ctx,
				"UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1",
				deviceID,
			); err != nil {
				log.Printf("Error updating device %s status: %v", deviceID, err)
			}
		}

		// Broadcast online status
		onlineMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_status",
			"deviceId": deviceID,
			"status":   "online",
		})
		services.Hub.BroadcastToDashboards(onlineMsg)

		// Create router for message handling
		router := &Router{
			config: services.Config,
			db:     services.DB.AsDB(),
			cache:  services.Redis,
			hub:    services.Hub,
		}

		// Auto-distribute CA certificate if needed
		go router.autoDistributeCertificate(client, authPayload.AgentID, authPayload.CACertHash)

		// Start read/write pumps
		go client.WritePump(ctx)
		client.ReadPump(ctx, func(msg []byte) {
			router.handleAgentMessage(authPayload.AgentID, deviceID, msg)
		})

		// Update device status on disconnect
		if _, err := services.DB.Pool().Exec(context.Background(),
			"UPDATE devices SET status = 'offline' WHERE id = $1",
			deviceID,
		); err != nil {
			log.Printf("Error updating device %s status to offline: %v", deviceID, err)
		}

		// Broadcast offline status
		offlineMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_status",
			"deviceId": deviceID,
			"status":   "offline",
		})
		services.Hub.BroadcastToDashboards(offlineMsg)
	}
}

// handleCertRenewal handles certificate renewal requests from agents.
func handleCertRenewal(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// This endpoint requires mTLS - verify cert is present
		if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client certificate required"})
			return
		}

		clientCert := c.Request.TLS.PeerCertificates[0]
		agentID := pki.GetAgentIDFromCert(clientCert)
		oldSerial := pki.GetSerialFromCert(clientCert)

		if services.PKI == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PKI service unavailable"})
			return
		}

		// Look up device
		ctx := c.Request.Context()
		var deviceID uuid.UUID
		err := services.DB.Pool().QueryRow(ctx,
			"SELECT id FROM devices WHERE agent_id = $1 AND organization_id = $2",
			agentID, constants.CurrentOrganizationID,
		).Scan(&deviceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
			return
		}

		// Issue renewal
		bundle, err := services.PKI.RenewCertificate(
			ctx,
			agentID,
			deviceID,
			constants.CurrentOrganizationID,
			oldSerial,
			services.Config.CertValidityYears,
		)
		if err != nil {
			log.Printf("[PKI] Certificate renewal failed for agent %s: %v", agentID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to renew certificate"})
			return
		}

		log.Printf("[PKI] Certificate renewed for agent %s, new_serial=%s", agentID, bundle.SerialNumber)

		c.JSON(http.StatusOK, gin.H{
			"clientCert":   bundle.ClientCert,
			"clientKey":    bundle.ClientKey,
			"caCert":       bundle.CACert,
			"certExpiresAt": bundle.ExpiresAt.Format(time.RFC3339),
			"certSerial":   bundle.SerialNumber,
		})
	}
}
