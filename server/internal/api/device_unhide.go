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
	"runtime/debug"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinel/server/internal/audit"
)

// Triggers identifying which connection-establishment path performed an
// auto-unhide. Recorded in the audit entry details so operators can tell how a
// hidden device came back.
const (
	unhideTriggerWSAuth  = "ws-auth"
	unhideTriggerMTLS    = "mtls-auth"
	unhideTriggerEnroll  = "enroll"
	unhideTriggerWSCerts = "ws-certs"
)

// maxLoggedNameRunes bounds the agent-controlled identifier written to the
// server log and the audit details, so a device cannot pad log lines with an
// arbitrarily long hostname.
const maxLoggedNameRunes = 64

// sanitizeForLog makes an agent-controlled string safe to interpolate into a
// line-oriented log record. The hostname column is written from data the agent
// supplies, so without this a device could embed CR/LF and forge whole log
// records, or embed ANSI escapes to rewrite an operator's terminal.
//
// This is for log.Printf only. It is deliberately NOT applied to structured
// sinks (audit JSONB details, database columns, JSON broadcasts) where CR/LF
// carry no injection risk and the truncation plus non-injective U+FFFD
// substitution would only destroy forensic fidelity.
//
// Control characters (including CR, LF and ESC) are replaced with U+FFFD rather
// than dropped, so a tampered value stays visibly tampered instead of silently
// collapsing into a plausible-looking name.
func sanitizeForLog(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	count := 0
	for _, r := range s {
		if count >= maxLoggedNameRunes {
			b.WriteString("…")
			break
		}
		// unicode.IsPrint excludes every control character (CR, LF, ESC, NUL)
		// as well as unassigned and format runes, which is exactly the set that
		// can forge a log line or drive a terminal.
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('�')
		}
		count++
	}
	return b.String()
}

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
		// This goroutine has no caller to propagate to: an unrecovered panic
		// here takes down the entire server process. The notification path is
		// best-effort bookkeeping, so contain any panic (including a typed-nil
		// hub whose method dereferences its receiver) and log it instead.
		//
		// This is the outer backstop only. The alert path has its own, narrower
		// recover (see safeCreateAutoUnhideAlert) so a panic there cannot skip
		// the audit entry; reaching this handler means the audit write itself
		// panicked, and there is nothing further to salvage.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[Unhide] panic in auto-unhide notification for device %s: %v\n%s", deviceID, rec, debug.Stack())
			}
		}()

		notifyCtx, cancel := context.WithTimeout(context.Background(), unhideNotifyTimeout)
		defer cancel()
		notifyAutoUnhide(notifyCtx, pool, hub, deviceID, trigger)
	}()
}

// notifyAutoUnhide records the auto-unhide as an alert and an audit entry.
// Every failure is logged and swallowed — this runs detached from the
// connection path and must not propagate. Panic containment is enforced by
// recovers, not by convention: the alert path is wrapped by
// safeCreateAutoUnhideAlert so it cannot skip the audit write, and the
// goroutine autoUnhideOnReconnect starts holds the outer backstop.
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

	// hostname is agent-supplied, so the identifier is sanitized before it
	// reaches the line-oriented server log, which log tooling would otherwise
	// parse as forged records. The audit details keep the raw value (see below).
	safeDisplayName := sanitizeForLog(displayName)

	log.Printf("[Unhide] Device %s (%s) reconnected via %s while hidden — automatically restored to the device list",
		safeDisplayName, deviceID, trigger)

	const title = "Hidden device back online"
	// The alert row keeps the raw hostname so operators see the real name. The
	// value is bound as a SQL parameter and escaped by React on render, so
	// neither the database nor the dashboard is at risk. It is NOT fully safe
	// everywhere: alerts are exported to CSV/XLSX (see export.go exportAlerts /
	// exportCSV), which performs no spreadsheet-formula neutralization, so a
	// hostname beginning with =, +, - or @ is evaluated by Excel on open. That
	// is a known pre-existing gap affecting every export path, tracked
	// separately — sanitizing here would not close it and would only degrade
	// the alert text.
	message := fmt.Sprintf(
		"%s reconnected while hidden and was automatically restored to the device list.",
		displayName,
	)

	alertCreated := safeCreateAutoUnhideAlert(ctx, pool, hub, deviceID, hostname, title, message)

	// Agent connections carry no user identity, so the audit entry is attributed
	// to the system actor (nil UserID) with no source IP.
	//
	// The audit entry is written even when the alert insert failed (e.g. the
	// device was deleted during the detached window, so the FK / organization
	// lookup rejected the row): the audit record is the durable trail, and
	// alert_created records honestly whether the operator-facing alert exists.
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
			// Raw, unsanitized: audit.LogEvent stores details as a JSONB
			// column, a structured sink where CR/LF and ANSI escapes carry
			// no injection risk. Sanitizing here would only cost forensic
			// fidelity — the 64-rune cap truncates and the U+FFFD
			// substitution is non-injective, so the original value could
			// not be recovered from the audit trail. Sanitization belongs
			// on the log.Printf line above, and only there.
			"hostname":      hostname,
			"agent_id":      agentID,
			"trigger":       trigger,
			"alert_created": alertCreated,
		},
	); err != nil {
		log.Printf("[Unhide] Error writing auto-unhide audit entry for device %s: %v", deviceID, err)
	}
}

// safeCreateAutoUnhideAlert runs createAutoUnhideAlert under its own panic
// containment and reports whether the operator-facing alert was created.
//
// The scope matters: without this, a panic anywhere in the alert path (the
// insert, the JSON encode, or a typed-nil hub whose BroadcastToDashboards
// dereferences its receiver) would unwind straight past the audit.LogEvent call
// to the goroutine-level recover. That is the worst outcome available — the
// alert row is already committed by the time the broadcast runs, so the event
// would exist in `alerts` with no corresponding audit record. Containing the
// panic here keeps the audit write, the durable trail, on every path.
func safeCreateAutoUnhideAlert(ctx context.Context, pool *pgxpool.Pool, hub WebSocketHub, deviceID uuid.UUID, hostname, title, message string) (created bool) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[Unhide] panic creating auto-unhide alert for device %s: %v\n%s", deviceID, rec, debug.Stack())
			created = false
		}
	}()
	return createAutoUnhideAlert(ctx, pool, hub, deviceID, hostname, title, message) == nil
}

// createAutoUnhideAlert inserts the alert row and broadcasts it to connected
// dashboards, mirroring the agent-alert path in handlers.go so the event lands
// on the Alerts page like any other server-generated alert.
//
// It returns the insert error (nil on success) so the caller can record whether
// the operator-facing alert actually exists. A failed broadcast is not an error:
// the alert row is durable and the dashboard will pick it up on next fetch.
func createAutoUnhideAlert(ctx context.Context, pool *pgxpool.Pool, hub WebSocketHub, deviceID uuid.UUID, hostname, title, message string) error {
	alertID := uuid.New()
	createdAt := time.Now()

	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, device_id, severity, title, message, status, organization_id, created_at)
		VALUES ($1, $2, $3, $4, $5, 'open', (SELECT organization_id FROM devices WHERE id = $2), $6)
	`, alertID, deviceID, alertSeverityInfo, title, message, createdAt); err != nil {
		// Expected when the device row disappeared during the detached window:
		// the FK / organization_id subquery then rejects the insert.
		log.Printf("[Unhide] Error creating auto-unhide alert for device %s: %v", deviceID, err)
		return err
	}

	// A nil interface is caught here; a typed-nil pointer stored in the
	// interface is not, and is contained by the recover in the immediate
	// caller (see safeCreateAutoUnhideAlert), which keeps the panic from
	// skipping the audit entry.
	if hub == nil {
		return nil
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
		return nil
	}
	hub.BroadcastToDashboards(dashMsg)
	return nil
}
