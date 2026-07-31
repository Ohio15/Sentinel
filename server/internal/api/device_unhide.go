// Package api provides HTTP handlers for the Sentinel server API.
// This file contains the auto-unhide-on-reconnect path shared by every
// connection-establishment handler (token-auth WebSocket, mTLS WebSocket and
// HTTP enrollment).
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinel/server/internal/audit"
)

// Triggers identifying which connection-establishment path performed an
// auto-unhide. Recorded in the audit entry details so operators can tell how a
// hidden device came back.
const (
	unhideTriggerWSAuth = "ws-auth"
	unhideTriggerMTLS   = "mtls-auth"
	unhideTriggerEnroll = "enroll"
)

// unhideNotifyTimeout bounds the out-of-band alert/audit work so a slow or
// unreachable database can never leave a goroutine pinned to a dead connection.
const unhideNotifyTimeout = 15 * time.Second

// alertSeverityInfo is the alerts.severity value for informational alerts. Kept
// distinct from audit.SeverityInfo: the two columns are unrelated vocabularies
// that merely happen to share this value today.
const alertSeverityInfo = "info"

// unhideOnReconnect clears the hidden flag for a device that has just
// re-established an authenticated connection, returning true only when the
// device was actually hidden beforehand.
//
// The `hidden_at IS NOT NULL` predicate makes this a row-wise no-op for the
// overwhelmingly common case of a visible device reconnecting, so the true
// return doubles as the "this was a hidden device coming back" signal that
// drives the alert and audit entry.
func unhideOnReconnect(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID) bool {
	tag, err := pool.Exec(ctx, `
		UPDATE devices SET
			hidden_at = NULL,
			hidden_by = NULL,
			updated_at = NOW()
		WHERE id = $1 AND hidden_at IS NOT NULL
	`, deviceID)
	if err != nil {
		// Never fail the connection over this: the device is authenticated and
		// online regardless of whether we managed to restore it to the list.
		log.Printf("[Unhide] Error clearing hidden flag for device %s: %v", deviceID, err)
		return false
	}
	return tag.RowsAffected() > 0
}

// autoUnhideOnReconnect performs the unhide and, when the device really was
// hidden, surfaces the event as an alert plus an audit entry so the restore is
// never silent.
//
// The unhide itself is synchronous — it is a single indexed UPDATE issued right
// after the online-status update the caller already performed, and callers rely
// on the device being visible by the time the connection is live. The
// notification work is dispatched to a background goroutine on a detached,
// timeout-bounded context so it can neither block nor fail the connection.
func autoUnhideOnReconnect(ctx context.Context, pool *pgxpool.Pool, hub WebSocketHub, deviceID uuid.UUID, trigger string) {
	if pool == nil {
		return
	}
	if !unhideOnReconnect(ctx, pool, deviceID) {
		return
	}

	go func() {
		notifyCtx, cancel := context.WithTimeout(context.Background(), unhideNotifyTimeout)
		defer cancel()
		notifyAutoUnhide(notifyCtx, pool, hub, deviceID, trigger)
	}()
}

// notifyAutoUnhide records the auto-unhide as an alert and an audit entry.
// Every failure is logged and swallowed — this runs detached from the
// connection path and must not panic or propagate.
func notifyAutoUnhide(ctx context.Context, pool *pgxpool.Pool, hub WebSocketHub, deviceID uuid.UUID, trigger string) {
	var hostname, agentID string
	if err := pool.QueryRow(ctx,
		"SELECT COALESCE(hostname, ''), COALESCE(agent_id, '') FROM devices WHERE id = $1",
		deviceID,
	).Scan(&hostname, &agentID); err != nil {
		// Fall through with empty identifiers rather than dropping the event:
		// a nameless alert still beats a silent restore.
		log.Printf("[Unhide] Error loading device %s details for auto-unhide notification: %v", deviceID, err)
	}

	displayName := hostname
	if displayName == "" {
		displayName = agentID
	}
	if displayName == "" {
		displayName = deviceID.String()
	}

	log.Printf("[Unhide] Device %s (%s) reconnected via %s while hidden — automatically restored to the device list",
		displayName, deviceID, trigger)

	const title = "Hidden device back online"
	message := fmt.Sprintf(
		"%s reconnected while hidden and was automatically restored to the device list.",
		displayName,
	)

	createAutoUnhideAlert(ctx, pool, hub, deviceID, hostname, title, message)

	// Agent connections carry no user identity, so the audit entry is attributed
	// to the system actor (nil UserID) with no source IP.
	if err := audit.LogEvent(
		ctx,
		pool,
		audit.ActionDeviceAutoUnhide,
		audit.ResourceTypeDevice,
		&deviceID,
		nil,
		"",
		audit.SeverityInfo,
		map[string]any{
			"hostname": hostname,
			"agent_id": agentID,
			"trigger":  trigger,
		},
	); err != nil {
		log.Printf("[Unhide] Error writing auto-unhide audit entry for device %s: %v", deviceID, err)
	}
}

// createAutoUnhideAlert inserts the alert row and broadcasts it to connected
// dashboards, mirroring the agent-alert path in handlers.go so the event lands
// on the Alerts page like any other server-generated alert.
func createAutoUnhideAlert(ctx context.Context, pool *pgxpool.Pool, hub WebSocketHub, deviceID uuid.UUID, hostname, title, message string) {
	alertID := uuid.New()
	createdAt := time.Now()

	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, device_id, severity, title, message, status, organization_id, created_at)
		VALUES ($1, $2, $3, $4, $5, 'open', (SELECT organization_id FROM devices WHERE id = $2), $6)
	`, alertID, deviceID, alertSeverityInfo, title, message, createdAt); err != nil {
		log.Printf("[Unhide] Error creating auto-unhide alert for device %s: %v", deviceID, err)
		return
	}

	if hub == nil {
		return
	}
	dashMsg, err := json.Marshal(map[string]any{
		"type": "new_alert",
		"alert": map[string]any{
			"id":        alertID,
			"deviceId":  deviceID,
			"hostname":  hostname,
			"severity":  alertSeverityInfo,
			"title":     title,
			"message":   message,
			"status":    "open",
			"createdAt": createdAt,
		},
	})
	if err != nil {
		log.Printf("[Unhide] Error encoding auto-unhide alert broadcast for device %s: %v", deviceID, err)
		return
	}
	hub.BroadcastToDashboards(dashMsg)
}
