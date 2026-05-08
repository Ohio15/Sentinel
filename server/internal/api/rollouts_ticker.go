package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/sentinel/server/pkg/database"
)

// startRolloutTicker periodically inspects active rollouts and finalises
// those whose devices have all reached a terminal state (succeeded|failed).
// Mirrors the metrics refresher's lifecycle: spawned once from
// NewRouterWithServices, owns its own goroutine, exits when ctx is cancelled.
//
// Cadence: 60s. Heartbeat-driven dispatch + agent-driven status reports do
// the work; the ticker only finalises completed rollouts. Per-tick cost is
// O(active_rollouts) — fleet of 10 with one rollout in flight is sub-millisecond.
//
// Failure threshold semantics: if more than failure_threshold_percent of the
// device set ended in 'failed', mark the rollout 'failed'; otherwise 'completed'.
func startRolloutTicker(ctx context.Context, db *database.DB) {
	tick := func() {
		qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		rows, err := db.Pool().Query(qctx, `
			SELECT id, failure_threshold_percent
			FROM rollouts
			WHERE status = 'active'
		`)
		if err != nil {
			log.Printf("[RolloutTicker] active rollouts query failed: %v", err)
			return
		}
		type activeRollout struct {
			id         uuid.UUID
			failurePct float32
		}
		var active []activeRollout
		for rows.Next() {
			var ar activeRollout
			if err := rows.Scan(&ar.id, &ar.failurePct); err != nil {
				log.Printf("[RolloutTicker] scan failed: %v", err)
				continue
			}
			active = append(active, ar)
		}
		rows.Close()

		for _, ar := range active {
			finaliseRolloutIfDone(qctx, db, ar.id, ar.failurePct)
		}
	}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		// Run once immediately so a rollout that auto-completes (e.g. status
		// reports landed before the ticker started) gets finalised without a
		// 60s wait on cold start.
		tick()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tick()
			}
		}
	}()
}

// finaliseRolloutIfDone aggregates rollout_devices statuses; if all devices
// have reached succeeded|failed, marks the rollout completed or failed
// according to threshold.
func finaliseRolloutIfDone(ctx context.Context, db *database.DB, rolloutID uuid.UUID, failurePct float32) {
	var pending, dispatched, succeeded, failed, total int
	err := db.Pool().QueryRow(ctx, `
		SELECT
		  COALESCE(SUM(CASE WHEN status='pending'    THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status='dispatched' THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status='succeeded'  THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status='failed'     THEN 1 ELSE 0 END),0),
		  COALESCE(COUNT(*),0)
		FROM rollout_devices
		WHERE rollout_id = $1
	`, rolloutID).Scan(&pending, &dispatched, &succeeded, &failed, &total)
	if err != nil {
		log.Printf("[RolloutTicker] count failed for rollout %s: %v", rolloutID, err)
		return
	}
	_ = pending
	_ = dispatched

	if total == 0 {
		// No devices ever attached — createRolloutHandler auto-completes
		// empty targets, so this should never fire in practice. Defensive.
		return
	}

	done := succeeded + failed
	if done < total {
		// Still some pending or dispatched-not-yet-reported devices.
		return
	}

	successRate := float32(1.0)
	if total > 0 {
		successRate = float32(succeeded) / float32(total)
	}
	failureRatePct := float32(0)
	if total > 0 {
		failureRatePct = float32(failed) * 100.0 / float32(total)
	}

	newStatus := "completed"
	eventType := "rollout_completed"
	if failureRatePct > failurePct {
		newStatus = "failed"
		eventType = "rollout_failed"
	}

	tag, err := db.Pool().Exec(ctx, `
		UPDATE rollouts
		SET status = $1, completed_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND status = 'active'
	`, newStatus, rolloutID)
	if err != nil {
		log.Printf("[RolloutTicker] rollout finalise update failed for %s: %v", rolloutID, err)
		return
	}
	if tag.RowsAffected() == 0 {
		// Status changed under us (cancel raced the ticker). Skip the audit event.
		return
	}

	if _, err := db.Pool().Exec(ctx, `
		UPDATE rollout_stages
		SET status = $1,
		    completed_at = NOW(),
		    success_rate = $2,
		    completed_devices = $3,
		    failed_devices = $4
		WHERE rollout_id = $5 AND status IN ('pending','active')
	`, newStatus, successRate, succeeded, failed, rolloutID); err != nil {
		log.Printf("[RolloutTicker] stage finalise update failed for %s: %v", rolloutID, err)
	}

	meta, _ := json.Marshal(map[string]interface{}{
		"total":                total,
		"succeeded":            succeeded,
		"failed":               failed,
		"success_rate":         successRate,
		"failure_rate_percent": failureRatePct,
		"threshold_percent":    failurePct,
	})
	var msg string
	if newStatus == "completed" {
		msg = fmt.Sprintf("Rollout completed: %d/%d succeeded (%d failed)", succeeded, total, failed)
	} else {
		msg = fmt.Sprintf("Rollout failed: %d/%d devices failed (%d succeeded)", failed, total, succeeded)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO rollout_events (id, rollout_id, event_type, message, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, uuid.New(), rolloutID, eventType, msg, meta); err != nil {
		log.Printf("[RolloutTicker] finalise event insert failed for %s: %v", rolloutID, err)
	}

	log.Printf("[RolloutTicker] finalised rollout %s status=%s succeeded=%d failed=%d total=%d failure_rate=%.1f%% threshold=%.1f%%",
		rolloutID, newStatus, succeeded, failed, total, failureRatePct, failurePct)
}
