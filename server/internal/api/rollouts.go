package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sentinel/server/internal/constants"
)

// Phase 6 of the agent-update saga (v1.77.30): rollout pipeline MVP.
//
// MVP scope: mode='immediate', target_type ∈ {'all-online','device-list'},
// channel='stable'. Reserved values for staged rollouts (mode='staged',
// target_type='update-group', channel ∈ {'beta','canary'}) are accepted by
// the schema but rejected at the handler with 400; the validation gate is
// the single thing the future stream will flip when devices are assigned to
// update_groups and stage promotion logic exists.

const (
	rolloutModeImmediate    = "immediate"
	rolloutChannelStable    = "stable"
	rolloutTargetAllOnline  = "all-online"
	rolloutTargetDeviceList = "device-list"
	rolloutTargetUpdateGrp  = "update-group"

	// Online threshold mirrors the rest of the codebase: a device is "online"
	// if status='online' AND last_seen within the last 15 minutes. Reusing this
	// here keeps "all-online" rollouts consistent with the dashboard view.
	onlineLastSeenWindow = 15 * time.Minute

	rolloutListDefaultLimit = 50
	rolloutListMaxLimit     = 200
	// Per-rollout device cap on GET /api/rollouts/:id — for MVP we don't
	// paginate the device array; a fleet >500 in one rollout is out-of-scope.
	rolloutDeviceDetailMax = 500
)

// createRolloutRequest is the POST /api/rollouts body.
type createRolloutRequest struct {
	ReleaseVersion          string  `json:"release_version" binding:"required"`
	Name                    string  `json:"name"`
	Description             string  `json:"description"`
	Mode                    string  `json:"mode" binding:"required"`
	Channel                 string  `json:"channel"`
	Target                  rolloutTarget `json:"target" binding:"required"`
	FailureThresholdPercent *float64 `json:"failure_threshold_percent"`
}

type rolloutTarget struct {
	Type      string   `json:"type" binding:"required"`
	DeviceIDs []string `json:"device_ids"`
}

// targetCanonical is the JSON-serialised form of (type, sorted device IDs)
// used to compute target_hash. Sorting device IDs makes the hash insensitive
// to client-side ordering.
type targetCanonical struct {
	Type      string   `json:"type"`
	DeviceIDs []string `json:"device_ids"`
}

func computeTargetHash(targetType string, deviceIDs []uuid.UUID) string {
	ids := make([]string, len(deviceIDs))
	for i, id := range deviceIDs {
		ids[i] = id.String()
	}
	sort.Strings(ids)
	canonical := targetCanonical{Type: targetType, DeviceIDs: ids}
	if len(ids) == 0 {
		// Force [] over null so JSON canonicalisation is stable across encoders.
		canonical.DeviceIDs = []string{}
	}
	buf, _ := json.Marshal(canonical)
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// resolveTargetDevices returns the device IDs the rollout will dispatch to,
// applying multi-tenant scoping. Caller MUST pass an organization_id.
func resolveTargetDevices(ctx context.Context, tx pgx.Tx, orgID int, t rolloutTarget) ([]uuid.UUID, error) {
	switch t.Type {
	case rolloutTargetAllOnline:
		// Online window matches dashboard semantics (15 minutes). Hardcoded
		// interval — onlineLastSeenWindow is a const, so changing it requires
		// touching this query too (caught at code review, not runtime).
		rows, err := tx.Query(ctx, `
			SELECT id FROM devices
			WHERE organization_id = $1
			  AND status = 'online'
			  AND is_disabled = false
			  AND last_seen IS NOT NULL
			  AND last_seen > NOW() - INTERVAL '15 minutes'
		`, orgID)
		if err != nil {
			return nil, fmt.Errorf("query online devices: %w", err)
		}
		defer rows.Close()
		ids := []uuid.UUID{}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("scan online device: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return ids, nil

	case rolloutTargetDeviceList:
		// Validate every requested device exists in this org. Reject the whole
		// request if any ID is missing/cross-org rather than silently dropping.
		want := make([]uuid.UUID, 0, len(t.DeviceIDs))
		for _, raw := range t.DeviceIDs {
			id, err := uuid.Parse(strings.TrimSpace(raw))
			if err != nil {
				return nil, fmt.Errorf("invalid device id %q: %w", raw, err)
			}
			want = append(want, id)
		}
		if len(want) == 0 {
			return nil, errors.New("device_ids required for target.type=device-list")
		}
		rows, err := tx.Query(ctx, `
			SELECT id FROM devices
			WHERE organization_id = $1 AND id = ANY($2)
		`, orgID, want)
		if err != nil {
			return nil, fmt.Errorf("query target devices: %w", err)
		}
		defer rows.Close()
		found := make(map[uuid.UUID]struct{}, len(want))
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("scan target device: %w", err)
			}
			found[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(found) != len(want) {
			missing := []string{}
			for _, id := range want {
				if _, ok := found[id]; !ok {
					missing = append(missing, id.String())
				}
			}
			return nil, fmt.Errorf("device(s) not found in organization: %s", strings.Join(missing, ","))
		}
		// Preserve caller order (deduped).
		seen := map[uuid.UUID]struct{}{}
		out := make([]uuid.UUID, 0, len(want))
		for _, id := range want {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unsupported target type %q", t.Type)
	}
}

// callerEmail extracts the email of the authenticated user from the gin
// context. Falls back to "unknown" rather than failing — created_by is
// audit-only, never load-bearing.
func callerEmail(c *gin.Context) string {
	if v, ok := c.Get("email"); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return "unknown"
}

// createRolloutHandler — POST /api/rollouts
//
// Creates an immediate rollout to a list of online devices or an explicit
// device list, dispatches updates via the heartbeat-ack path on the next
// heartbeat per device, and tracks per-device + aggregate state through the
// rollouts/rollout_stages/rollout_devices tables.
//
// Idempotent on (organization_id, release_version, target_hash) where
// status IN (pending,active,paused) — repeating the same call returns 409
// with the existing rollout id.
func createRolloutHandler(s *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createRolloutRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}

		// Mode validation. Only 'immediate' is wired up in MVP.
		if req.Mode != rolloutModeImmediate {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("mode %q not yet supported", req.Mode)})
			return
		}

		// Channel: default to stable; reject reserved channels until they're real.
		channel := req.Channel
		if channel == "" {
			channel = rolloutChannelStable
		}
		if channel != rolloutChannelStable {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("channel %q reserved", channel)})
			return
		}

		// Target type validation.
		switch req.Target.Type {
		case rolloutTargetAllOnline, rolloutTargetDeviceList:
			// ok
		case rolloutTargetUpdateGrp:
			c.JSON(http.StatusBadRequest, gin.H{"error": "target type 'update-group' reserved — assign devices first"})
			return
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid target.type %q", req.Target.Type)})
			return
		}

		if req.Target.Type == rolloutTargetDeviceList && len(req.Target.DeviceIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "target.device_ids must be non-empty when target.type='device-list'"})
			return
		}

		// Failure threshold default + bounds.
		failurePct := 20.0
		if req.FailureThresholdPercent != nil {
			failurePct = *req.FailureThresholdPercent
		}
		if failurePct < 0 || failurePct > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failure_threshold_percent must be in [0,100]"})
			return
		}

		ctx := c.Request.Context()

		// Validate release_version exists in agent_releases. Without this we
		// risk dispatching a version the server can't actually serve, repeating
		// the v1.77.10 outage (incident df7a7ff8).
		var releaseExists bool
		if err := s.DB.Pool().QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM agent_releases WHERE version = $1)`, req.ReleaseVersion,
		).Scan(&releaseExists); err != nil {
			log.Printf("[Rollouts] release lookup failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "release lookup failed"})
			return
		}
		if !releaseExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("release_version %q not published in agent_releases", req.ReleaseVersion)})
			return
		}

		orgID := constants.CurrentOrganizationID
		creator := callerEmail(c)

		tx, err := s.DB.Pool().Begin(ctx)
		if err != nil {
			log.Printf("[Rollouts] begin tx failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback(ctx)

		// Resolve target → list of device UUIDs.
		deviceIDs, err := resolveTargetDevices(ctx, tx, orgID, req.Target)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Compute target_hash on the resolved+sorted list. For all-online this
		// captures the exact device set at creation time, so "create at 10:00"
		// and "create at 10:05 with one new online device" produce different
		// hashes and don't collide.
		targetHash := computeTargetHash(req.Target.Type, deviceIDs)

		// Idempotency: existing in-flight rollout with same (org, version, hash)?
		var existingID uuid.UUID
		var existingStatus string
		err = tx.QueryRow(ctx, `
			SELECT id, status FROM rollouts
			WHERE organization_id = $1 AND release_version = $2 AND target_hash = $3
			  AND status IN ('pending','active','paused')
			LIMIT 1
		`, orgID, req.ReleaseVersion, targetHash).Scan(&existingID, &existingStatus)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error":              "rollout with same target already in flight",
				"existing_rollout_id": existingID,
				"status":             existingStatus,
			})
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("[Rollouts] idempotency lookup failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "idempotency lookup failed"})
			return
		}

		// Build target_spec JSON for storage. For all-online we record the
		// resolved snapshot; for device-list we echo the input (already validated).
		specBytes, _ := json.Marshal(map[string]interface{}{
			"device_ids": uuidsToStrings(deviceIDs),
		})

		// targetCount=0 path: still create the rollout, mark it completed
		// immediately. This gives the caller a stable receipt and an audit row.
		targetCount := len(deviceIDs)
		rolloutStatus := "active"
		stageStatus := "active"
		var completedAt *time.Time
		if targetCount == 0 {
			rolloutStatus = "completed"
			stageStatus = "completed"
			now := time.Now().UTC()
			completedAt = &now
		}

		// rollouts row
		rolloutID := uuid.New()
		name := req.Name
		if name == "" {
			name = fmt.Sprintf("Rollout %s -> %s", req.Target.Type, req.ReleaseVersion)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO rollouts (
				id, organization_id, release_version, name, description, status,
				mode, channel, target_type, target_spec, target_hash,
				failure_threshold_percent, created_by, started_at, completed_at,
				created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),$14,NOW(),NOW())
		`, rolloutID, orgID, req.ReleaseVersion, name, req.Description, rolloutStatus,
			rolloutModeImmediate, channel, req.Target.Type, specBytes, targetHash,
			failurePct, creator, completedAt)
		if err != nil {
			// Concurrent racer raced past the SELECT and inserted first; the
			// rollouts_idem_active unique partial index turns this into a 23505.
			// Surface as 409 to match the explicit-check path above.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "rollouts_idem_active" {
				c.JSON(http.StatusConflict, gin.H{"error": "rollout with same target already in flight"})
				return
			}
			log.Printf("[Rollouts] insert rollout failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rollout"})
			return
		}

		// One ad-hoc stage per MVP rollout (group_id NULL).
		stageID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO rollout_stages (
				id, rollout_id, group_id, status, total_devices,
				completed_devices, failed_devices, started_at, completed_at, created_at
			) VALUES ($1, $2, NULL, $3, $4, 0, 0, NOW(), $5, NOW())
		`, stageID, rolloutID, stageStatus, targetCount, completedAt)
		if err != nil {
			log.Printf("[Rollouts] insert stage failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rollout stage"})
			return
		}

		// rollout_devices: one row per resolved device, all 'pending'.
		for _, deviceID := range deviceIDs {
			var fromVersion string
			_ = tx.QueryRow(ctx, `SELECT COALESCE(agent_version, '') FROM devices WHERE id = $1`, deviceID).Scan(&fromVersion)
			_, err = tx.Exec(ctx, `
				INSERT INTO rollout_devices (
					id, rollout_id, stage_id, device_id, status,
					from_version, to_version, attempts, created_at
				) VALUES ($1, $2, $3, $4, 'pending', $5, $6, 0, NOW())
			`, uuid.New(), rolloutID, stageID, deviceID, fromVersion, req.ReleaseVersion)
			if err != nil {
				log.Printf("[Rollouts] insert rollout_device failed: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rollout devices"})
				return
			}
		}

		// rollout_events: created. metadata captures the resolution snapshot.
		eventMeta, _ := json.Marshal(map[string]interface{}{
			"target_type":  req.Target.Type,
			"target_count": targetCount,
			"channel":      channel,
		})
		_, err = tx.Exec(ctx, `
			INSERT INTO rollout_events (id, rollout_id, stage_id, event_type, message, metadata, created_at)
			VALUES ($1, $2, $3, 'rollout_created', $4, $5, NOW())
		`, uuid.New(), rolloutID, stageID,
			fmt.Sprintf("Created by %s targeting %d devices", creator, targetCount), eventMeta)
		if err != nil {
			log.Printf("[Rollouts] insert event failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write rollout event"})
			return
		}

		// If we auto-completed (targetCount=0), emit the completion event too.
		if rolloutStatus == "completed" {
			_, err = tx.Exec(ctx, `
				INSERT INTO rollout_events (id, rollout_id, stage_id, event_type, message, metadata, created_at)
				VALUES ($1, $2, $3, 'rollout_completed', 'No devices to dispatch — rollout completed immediately', '{}'::jsonb, NOW())
			`, uuid.New(), rolloutID, stageID)
			if err != nil {
				log.Printf("[Rollouts] insert auto-complete event failed: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to finalise empty rollout"})
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("[Rollouts] commit failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit rollout"})
			return
		}

		log.Printf("[Rollouts] created %s release=%s target=%s count=%d status=%s creator=%s",
			rolloutID, req.ReleaseVersion, req.Target.Type, targetCount, rolloutStatus, creator)

		c.JSON(http.StatusCreated, gin.H{
			"id":              rolloutID,
			"status":          rolloutStatus,
			"target_count":    targetCount,
			"release_version": req.ReleaseVersion,
			"created_at":      time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// listRolloutsHandler — GET /api/rollouts?status=active,paused&limit=50
func listRolloutsHandler(s *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		orgID := constants.CurrentOrganizationID

		limit := rolloutListDefaultLimit
		if v := c.Query("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
			limit = n
		}
		if limit > rolloutListMaxLimit {
			limit = rolloutListMaxLimit
		}

		// status=active OR status=active,paused
		var statusFilter []string
		if v := c.Query("status"); v != "" {
			for _, s := range strings.Split(v, ",") {
				t := strings.TrimSpace(s)
				if t != "" {
					statusFilter = append(statusFilter, t)
				}
			}
		}

		query := `
			SELECT r.id, r.release_version, r.name, r.description, r.status,
			       r.mode, r.channel, r.target_type, r.failure_threshold_percent,
			       r.created_by, r.created_at, r.started_at, r.completed_at,
			       COALESCE(SUM(CASE WHEN rd.status = 'pending'    THEN 1 ELSE 0 END),0) AS pending_count,
			       COALESCE(SUM(CASE WHEN rd.status = 'dispatched' THEN 1 ELSE 0 END),0) AS dispatched_count,
			       COALESCE(SUM(CASE WHEN rd.status = 'succeeded'  THEN 1 ELSE 0 END),0) AS succeeded_count,
			       COALESCE(SUM(CASE WHEN rd.status = 'failed'     THEN 1 ELSE 0 END),0) AS failed_count,
			       COALESCE(COUNT(rd.id), 0) AS target_count
			FROM rollouts r
			LEFT JOIN rollout_devices rd ON rd.rollout_id = r.id
			WHERE r.organization_id = $1
		`
		args := []interface{}{orgID}
		if len(statusFilter) > 0 {
			query += " AND r.status = ANY($2)"
			args = append(args, statusFilter)
		}
		query += " GROUP BY r.id ORDER BY r.created_at DESC LIMIT " + strconv.Itoa(limit)

		rows, err := s.DB.Pool().Query(ctx, query, args...)
		if err != nil {
			log.Printf("[Rollouts] list query failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rollouts"})
			return
		}
		defer rows.Close()

		out := []map[string]interface{}{}
		for rows.Next() {
			var (
				id                                                              uuid.UUID
				releaseVersion, status, mode, channel, targetType, createdBy    string
				name, description                                               *string
				failurePct                                                      float64
				createdAt                                                       time.Time
				startedAt, completedAt                                          *time.Time
				pendingCount, dispatchedCount, succeededCount, failedCount      int
				targetCount                                                     int
			)
			if err := rows.Scan(&id, &releaseVersion, &name, &description, &status,
				&mode, &channel, &targetType, &failurePct, &createdBy, &createdAt,
				&startedAt, &completedAt,
				&pendingCount, &dispatchedCount, &succeededCount, &failedCount, &targetCount); err != nil {
				log.Printf("[Rollouts] list scan failed: %v", err)
				continue
			}
			row := map[string]interface{}{
				"id":                        id,
				"release_version":           releaseVersion,
				"name":                      derefString(name),
				"description":               derefString(description),
				"status":                    status,
				"mode":                      mode,
				"channel":                   channel,
				"target_type":               targetType,
				"failure_threshold_percent": failurePct,
				"created_by":                createdBy,
				"created_at":                createdAt.UTC().Format(time.RFC3339),
				"target_count":              targetCount,
				"pending_count":             pendingCount,
				"dispatched_count":          dispatchedCount,
				"succeeded_count":           succeededCount,
				"failed_count":              failedCount,
			}
			if startedAt != nil {
				row["started_at"] = startedAt.UTC().Format(time.RFC3339)
			}
			if completedAt != nil {
				row["completed_at"] = completedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Rollouts] list iter failed: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{
			"rollouts": out,
			"total":    len(out),
		})
	}
}

// getRolloutHandler — GET /api/rollouts/:id
func getRolloutHandler(s *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		orgID := constants.CurrentOrganizationID

		idStr := c.Param("id")
		rolloutID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rollout id"})
			return
		}

		var (
			releaseVersion, status, mode, channel, targetType, createdBy string
			name, description                                            *string
			failurePct                                                   float64
			createdAt                                                    time.Time
			startedAt, completedAt                                       *time.Time
			targetSpec                                                   []byte
		)
		err = s.DB.Pool().QueryRow(ctx, `
			SELECT release_version, name, description, status, mode, channel,
			       target_type, target_spec, failure_threshold_percent,
			       created_by, created_at, started_at, completed_at
			FROM rollouts
			WHERE id = $1 AND organization_id = $2
		`, rolloutID, orgID).Scan(&releaseVersion, &name, &description, &status,
			&mode, &channel, &targetType, &targetSpec, &failurePct,
			&createdBy, &createdAt, &startedAt, &completedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
			return
		}
		if err != nil {
			log.Printf("[Rollouts] get rollout failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rollout"})
			return
		}

		// Stages
		stageRows, err := s.DB.Pool().Query(ctx, `
			SELECT id, group_id, status, total_devices, completed_devices,
			       failed_devices, success_rate, started_at, completed_at, created_at
			FROM rollout_stages
			WHERE rollout_id = $1
			ORDER BY created_at ASC
		`, rolloutID)
		if err != nil {
			log.Printf("[Rollouts] get stages failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load stages"})
			return
		}
		stages := []map[string]interface{}{}
		for stageRows.Next() {
			var (
				stageID                                          uuid.UUID
				groupID                                          *uuid.UUID
				stageStatus                                      string
				totalDev, doneDev, failDev                       int
				successRate                                      *float64
				stStartedAt, stCompletedAt, stCreatedAt          *time.Time
			)
			if err := stageRows.Scan(&stageID, &groupID, &stageStatus, &totalDev, &doneDev,
				&failDev, &successRate, &stStartedAt, &stCompletedAt, &stCreatedAt); err != nil {
				continue
			}
			stage := map[string]interface{}{
				"id":                stageID,
				"status":            stageStatus,
				"total_devices":     totalDev,
				"completed_devices": doneDev,
				"failed_devices":    failDev,
			}
			if groupID != nil {
				stage["group_id"] = *groupID
			}
			if successRate != nil {
				stage["success_rate"] = *successRate
			}
			if stStartedAt != nil {
				stage["started_at"] = stStartedAt.UTC().Format(time.RFC3339)
			}
			if stCompletedAt != nil {
				stage["completed_at"] = stCompletedAt.UTC().Format(time.RFC3339)
			}
			stages = append(stages, stage)
		}
		stageRows.Close()

		// Per-device rows (capped).
		devRows, err := s.DB.Pool().Query(ctx, `
			SELECT rd.id, rd.device_id, COALESCE(d.hostname,''), rd.status,
			       rd.from_version, rd.to_version, rd.error_message,
			       rd.dispatched_at, rd.started_at, rd.completed_at, rd.attempts
			FROM rollout_devices rd
			LEFT JOIN devices d ON d.id = rd.device_id
			WHERE rd.rollout_id = $1
			ORDER BY rd.created_at ASC
			LIMIT $2
		`, rolloutID, rolloutDeviceDetailMax)
		if err != nil {
			log.Printf("[Rollouts] get devices failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rollout devices"})
			return
		}
		devices := []map[string]interface{}{}
		for devRows.Next() {
			var (
				rdID, devID                              uuid.UUID
				hostname, devStatus                      string
				fromV, toV                               *string
				errMsg                                   *string
				dispatchedAt, devStartedAt, devCompleted *time.Time
				attempts                                 int
			)
			if err := devRows.Scan(&rdID, &devID, &hostname, &devStatus, &fromV, &toV,
				&errMsg, &dispatchedAt, &devStartedAt, &devCompleted, &attempts); err != nil {
				continue
			}
			row := map[string]interface{}{
				"id":          rdID,
				"device_id":   devID,
				"hostname":    hostname,
				"status":      devStatus,
				"from_version": derefString(fromV),
				"to_version":   derefString(toV),
				"attempts":    attempts,
			}
			if errMsg != nil {
				row["error_message"] = *errMsg
			}
			if dispatchedAt != nil {
				row["dispatched_at"] = dispatchedAt.UTC().Format(time.RFC3339)
			}
			if devStartedAt != nil {
				row["started_at"] = devStartedAt.UTC().Format(time.RFC3339)
			}
			if devCompleted != nil {
				row["completed_at"] = devCompleted.UTC().Format(time.RFC3339)
			}
			devices = append(devices, row)
		}
		devRows.Close()

		// Aggregate counts (cheap, indexed).
		var pendingC, dispatchedC, succeededC, failedC, totalC int
		_ = s.DB.Pool().QueryRow(ctx, `
			SELECT
			  SUM(CASE WHEN status='pending'    THEN 1 ELSE 0 END),
			  SUM(CASE WHEN status='dispatched' THEN 1 ELSE 0 END),
			  SUM(CASE WHEN status='succeeded'  THEN 1 ELSE 0 END),
			  SUM(CASE WHEN status='failed'     THEN 1 ELSE 0 END),
			  COUNT(*)
			FROM rollout_devices WHERE rollout_id = $1
		`, rolloutID).Scan(&pendingC, &dispatchedC, &succeededC, &failedC, &totalC)

		var specObj interface{}
		if len(targetSpec) > 0 {
			_ = json.Unmarshal(targetSpec, &specObj)
		}

		resp := gin.H{
			"id":                        rolloutID,
			"release_version":           releaseVersion,
			"name":                      derefString(name),
			"description":               derefString(description),
			"status":                    status,
			"mode":                      mode,
			"channel":                   channel,
			"target_type":               targetType,
			"target_spec":               specObj,
			"failure_threshold_percent": failurePct,
			"created_by":                createdBy,
			"created_at":                createdAt.UTC().Format(time.RFC3339),
			"target_count":              totalC,
			"pending_count":             pendingC,
			"dispatched_count":          dispatchedC,
			"succeeded_count":           succeededC,
			"failed_count":              failedC,
			"stages":                    stages,
			"devices":                   devices,
		}
		if startedAt != nil {
			resp["started_at"] = startedAt.UTC().Format(time.RFC3339)
		}
		if completedAt != nil {
			resp["completed_at"] = completedAt.UTC().Format(time.RFC3339)
		}
		c.JSON(http.StatusOK, resp)
	}
}

// cancelRolloutHandler — POST /api/rollouts/:id/cancel
//
// Cancels a rollout if it is currently in (pending,active,paused). Devices
// already dispatched continue independently — this only stops the heartbeat
// path from offering the update to remaining 'pending' devices, because the
// heartbeat lookup filters by r.status='active'.
func cancelRolloutHandler(s *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		orgID := constants.CurrentOrganizationID

		idStr := c.Param("id")
		rolloutID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rollout id"})
			return
		}

		tx, err := s.DB.Pool().Begin(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback(ctx)

		// Inspect current status; reject cancel-of-cancel and cancel-of-finished.
		var status string
		err = tx.QueryRow(ctx, `
			SELECT status FROM rollouts WHERE id = $1 AND organization_id = $2 FOR UPDATE
		`, rolloutID, orgID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
			return
		}
		if err != nil {
			log.Printf("[Rollouts] cancel lookup failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rollout"})
			return
		}
		if status != "pending" && status != "active" && status != "paused" {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("cannot cancel rollout in status %q", status)})
			return
		}

		if _, err := tx.Exec(ctx, `
			UPDATE rollouts SET status='cancelled', completed_at=NOW(), updated_at=NOW()
			WHERE id = $1
		`, rolloutID); err != nil {
			log.Printf("[Rollouts] cancel update rollout failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel rollout"})
			return
		}

		if _, err := tx.Exec(ctx, `
			UPDATE rollout_stages SET status='cancelled', completed_at=NOW()
			WHERE rollout_id = $1 AND status IN ('pending','active')
		`, rolloutID); err != nil {
			log.Printf("[Rollouts] cancel update stages failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel rollout stages"})
			return
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO rollout_events (id, rollout_id, event_type, message, metadata, created_at)
			VALUES ($1, $2, 'rollout_cancelled', $3, '{}'::jsonb, NOW())
		`, uuid.New(), rolloutID,
			fmt.Sprintf("Cancelled by %s", callerEmail(c))); err != nil {
			log.Printf("[Rollouts] cancel insert event failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write event"})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("[Rollouts] cancel commit failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit cancel"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"id": rolloutID, "status": "cancelled"})
	}
}

// uuidsToStrings converts a uuid slice to a string slice for JSON storage.
func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// dispatchPendingRolloutForDevice is invoked from the heartbeat-ack path. If
// the device has a pending rollout_devices row whose parent rollout is still
// active, it transitions the row to 'dispatched', writes an audit event, and
// returns (releaseVersion, rolloutID, true). The heartbeat handler then sets
// updateAvailable=true on the ack and SHORT-CIRCUITS the global latestVersion
// comparison.
//
// This is the override-on-rollout semantic: a device in an active rollout is
// always told to update (admin chose this), even if it's already at
// latestVersion or had a recent failure to that version. The release-row gate
// is unnecessary here — createRolloutHandler validates release_version exists
// in agent_releases before the rollout enters 'active'.
//
// On error we return (false) and let the caller fall through to the legacy
// path; this is failure-safe since the rollout will be retried on the next
// heartbeat (10s).
func (r *Router) dispatchPendingRolloutForDevice(ctx context.Context, agentID string, deviceID uuid.UUID, agentVersion string) (string, uuid.UUID, bool) {
	var (
		rdID         uuid.UUID
		rolloutID    uuid.UUID
		releaseVer   string
	)
	err := r.db.Pool().QueryRow(ctx, `
		SELECT rd.id, rd.rollout_id, r.release_version
		FROM rollout_devices rd
		JOIN rollouts r ON r.id = rd.rollout_id
		WHERE rd.device_id = $1
		  AND rd.status = 'pending'
		  AND r.status = 'active'
		ORDER BY rd.created_at ASC
		LIMIT 1
	`, deviceID).Scan(&rdID, &rolloutID, &releaseVer)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("[Rollouts] dispatch lookup failed for device %s: %v", deviceID, err)
		}
		return "", uuid.Nil, false
	}

	// Mark dispatched. UPDATE…RETURNING was avoided to keep this resilient if
	// the row was already updated by a racing heartbeat (rare but possible
	// when two backend instances see the same agent reconnect quickly).
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE rollout_devices
		SET status = 'dispatched',
		    dispatched_at = NOW(),
		    started_at = COALESCE(started_at, NOW()),
		    attempts = attempts + 1
		WHERE id = $1 AND status = 'pending'
	`, rdID)
	if err != nil {
		log.Printf("[Rollouts] dispatch update failed for device %s rollout %s: %v", deviceID, rolloutID, err)
		return "", uuid.Nil, false
	}
	if tag.RowsAffected() == 0 {
		// Another goroutine got there first — don't double-dispatch.
		return "", uuid.Nil, false
	}

	meta, _ := json.Marshal(map[string]interface{}{
		"agent_version": agentVersion,
		"agent_id":      agentID,
	})
	if _, err := r.db.Pool().Exec(ctx, `
		INSERT INTO rollout_events (id, rollout_id, device_id, event_type, message, metadata, created_at)
		VALUES ($1, $2, $3, 'device_dispatched', $4, $5, NOW())
	`, uuid.New(), rolloutID, deviceID,
		fmt.Sprintf("ack delivered to agent %s -> %s", agentVersion, releaseVer), meta); err != nil {
		// Audit event failed but the dispatch itself succeeded — log and continue.
		log.Printf("[Rollouts] dispatch event insert failed: %v", err)
	}

	return releaseVer, rolloutID, true
}

// recordRolloutDeviceOutcome is called from reportUpdateStatus when an agent
// reports completed/failed for a version. It looks up an in-flight
// rollout_devices row in 'dispatched' state for this device and transitions
// it to 'succeeded' or 'failed', writing an audit event. No-ops cleanly if
// the device isn't part of a rollout (legacy force-update path).
func (r *Router) recordRolloutDeviceOutcome(ctx context.Context, deviceID uuid.UUID, success bool, errorMessage string) {
	var (
		rdID      uuid.UUID
		rolloutID uuid.UUID
	)
	err := r.db.Pool().QueryRow(ctx, `
		SELECT rd.id, rd.rollout_id
		FROM rollout_devices rd
		JOIN rollouts r ON r.id = rd.rollout_id
		WHERE rd.device_id = $1
		  AND rd.status = 'dispatched'
		  AND r.status = 'active'
		ORDER BY rd.dispatched_at DESC NULLS LAST
		LIMIT 1
	`, deviceID).Scan(&rdID, &rolloutID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("[Rollouts] outcome lookup failed for device %s: %v", deviceID, err)
		}
		return
	}

	newStatus := "succeeded"
	eventType := "device_succeeded"
	if !success {
		newStatus = "failed"
		eventType = "device_failed"
	}

	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE rollout_devices
		SET status = $1, completed_at = NOW(), error_message = $2
		WHERE id = $3
	`, newStatus, nullableString(errorMessage), rdID); err != nil {
		log.Printf("[Rollouts] outcome update failed for device %s: %v", deviceID, err)
		return
	}

	meta, _ := json.Marshal(map[string]interface{}{
		"error": errorMessage,
	})
	if _, err := r.db.Pool().Exec(ctx, `
		INSERT INTO rollout_events (id, rollout_id, device_id, event_type, message, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, uuid.New(), rolloutID, deviceID, eventType,
		fmt.Sprintf("device %s reported %s", deviceID, newStatus), meta); err != nil {
		log.Printf("[Rollouts] outcome event insert failed: %v", err)
	}
}

// nullableString returns nil for empty strings so the column stays NULL
// rather than storing an empty literal. Used for error_message.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
