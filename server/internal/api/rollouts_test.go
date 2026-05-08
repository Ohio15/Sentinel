package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/pkg/database"
)

// rolloutsTestRelease is a fixed agent_releases version we insert before each
// test. Tests delete it on teardown.
const rolloutsTestRelease = "9.99.99-rollout-test"

// setupRolloutTest brings up a real test DB and seeds an agent_releases row
// for the test version. It does NOT delete pre-existing rollouts/devices —
// individual tests own their own cleanup of rows they insert.
func setupRolloutTest(t *testing.T) (*database.DB, *Services, func()) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	// Seed agent_releases row so createRolloutHandler's release validation passes.
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO agent_releases (version, release_date, changelog, is_required, platforms)
		VALUES ($1, NOW(), 'rollout-test', false, ARRAY['windows','linux','darwin'])
		ON CONFLICT (version) DO NOTHING
	`, rolloutsTestRelease)
	if err != nil {
		db.Close()
		t.Fatalf("seed agent_releases failed: %v", err)
	}

	services := &Services{DB: db}

	cleanup := func() {
		// Clean per-test rows. Order matters: events -> stages -> devices -> rollouts.
		db.Pool().Exec(ctx, `DELETE FROM rollout_events WHERE rollout_id IN (SELECT id FROM rollouts WHERE release_version = $1)`, rolloutsTestRelease)
		db.Pool().Exec(ctx, `DELETE FROM rollout_devices WHERE rollout_id IN (SELECT id FROM rollouts WHERE release_version = $1)`, rolloutsTestRelease)
		db.Pool().Exec(ctx, `DELETE FROM rollout_stages WHERE rollout_id IN (SELECT id FROM rollouts WHERE release_version = $1)`, rolloutsTestRelease)
		db.Pool().Exec(ctx, `DELETE FROM rollouts WHERE release_version = $1`, rolloutsTestRelease)
		db.Pool().Exec(ctx, `DELETE FROM agent_releases WHERE version = $1`, rolloutsTestRelease)
		db.Close()
	}
	return db, services, cleanup
}

// makeRolloutTestDevice inserts a device with a known agent_version into the
// devices table. Marked online so all-online targeting picks it up.
func makeRolloutTestDevice(t *testing.T, db *database.DB, hostname string, online bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	deviceID := uuid.New()
	agentID := uuid.New().String()
	status := "offline"
	if online {
		status = "online"
	}
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO devices (
			id, agent_id, hostname, os_type, status, agent_version,
			last_seen, organization_id, is_disabled, created_at, updated_at
		) VALUES ($1, $2, $3, 'linux', $4, '1.77.0', NOW(), 1, false, NOW(), NOW())
	`, deviceID, agentID, hostname, status)
	if err != nil {
		t.Fatalf("create rollout test device: %v", err)
	}
	return deviceID
}

// rolloutTestRouter wraps a Services into a gin engine with admin context
// pre-set. Handlers under test register against this engine directly.
func rolloutTestRouter(s *Services) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userId", uuid.New())
		c.Set("email", "admin@test.example.com")
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/api/rollouts", createRolloutHandler(s))
	r.GET("/api/rollouts", listRolloutsHandler(s))
	r.GET("/api/rollouts/:id", getRolloutHandler(s))
	r.POST("/api/rollouts/:id/cancel", cancelRolloutHandler(s))
	return r
}

func postJSON(t *testing.T, r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestCreateRollout_DeviceList_Succeeds verifies the happy path: rollout +
// stage + per-device + audit-event rows all materialise inside one tx.
func TestCreateRollout_DeviceList_Succeeds(t *testing.T) {
	db, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	d1 := makeRolloutTestDevice(t, db, "rollout-test-1", true)
	d2 := makeRolloutTestDevice(t, db, "rollout-test-2", false)
	defer db.Pool().Exec(context.Background(), "DELETE FROM devices WHERE id IN ($1,$2)", d1, d2)

	r := rolloutTestRouter(services)

	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"name":            "test rollout",
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type":       "device-list",
			"device_ids": []string{d1.String(), d2.String()},
		},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ID             uuid.UUID `json:"id"`
		Status         string    `json:"status"`
		TargetCount    int       `json:"target_count"`
		ReleaseVersion string    `json:"release_version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v body=%s", err, w.Body.String())
	}
	if resp.Status != "active" {
		t.Errorf("expected status=active, got %q", resp.Status)
	}
	if resp.TargetCount != 2 {
		t.Errorf("expected target_count=2, got %d", resp.TargetCount)
	}

	// Verify side effects.
	ctx := context.Background()
	var stageCount, deviceCount, eventCount int
	db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM rollout_stages WHERE rollout_id=$1", resp.ID).Scan(&stageCount)
	db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM rollout_devices WHERE rollout_id=$1", resp.ID).Scan(&deviceCount)
	db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM rollout_events WHERE rollout_id=$1 AND event_type='rollout_created'", resp.ID).Scan(&eventCount)
	if stageCount != 1 {
		t.Errorf("expected 1 stage, got %d", stageCount)
	}
	if deviceCount != 2 {
		t.Errorf("expected 2 rollout_devices, got %d", deviceCount)
	}
	if eventCount != 1 {
		t.Errorf("expected 1 rollout_created event, got %d", eventCount)
	}

	// rollout_stages.group_id MUST be NULL for ad-hoc stages — verifies the
	// 000055 NOT NULL relaxation took effect.
	var groupID *uuid.UUID
	db.Pool().QueryRow(ctx, "SELECT group_id FROM rollout_stages WHERE rollout_id=$1", resp.ID).Scan(&groupID)
	if groupID != nil {
		t.Errorf("expected ad-hoc stage with NULL group_id, got %v", *groupID)
	}
}

// TestCreateRollout_AllOnline_ResolvesOnlineDevices ensures the all-online
// path picks up online + recent-last-seen devices and no others.
func TestCreateRollout_AllOnline_ResolvesOnlineDevices(t *testing.T) {
	db, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	dOnline := makeRolloutTestDevice(t, db, "rollout-online-1", true)
	dOffline := makeRolloutTestDevice(t, db, "rollout-offline-1", false)
	defer db.Pool().Exec(context.Background(), "DELETE FROM devices WHERE id IN ($1,$2)", dOnline, dOffline)

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type": "all-online",
		},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ID          uuid.UUID `json:"id"`
		TargetCount int       `json:"target_count"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	// Resolved set must include the online device. Other tests may run in the
	// same DB and contribute online devices, so we only check that ours is in
	// and the offline one is out.
	ctx := context.Background()
	var hasOnline, hasOffline bool
	db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM rollout_devices WHERE rollout_id=$1 AND device_id=$2)", resp.ID, dOnline).Scan(&hasOnline)
	db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM rollout_devices WHERE rollout_id=$1 AND device_id=$2)", resp.ID, dOffline).Scan(&hasOffline)
	if !hasOnline {
		t.Error("expected online device to be in rollout")
	}
	if hasOffline {
		t.Error("expected offline device NOT to be in rollout")
	}
}

func TestCreateRollout_UpdateGroupTarget_Rejected(t *testing.T) {
	_, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target":          map[string]interface{}{"type": "update-group"},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("update-group")) {
		t.Errorf("expected error mentioning update-group, got %s", w.Body.String())
	}
}

func TestCreateRollout_StagedMode_Rejected(t *testing.T) {
	_, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "staged",
		"target":          map[string]interface{}{"type": "all-online"},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRollout_NonExistentRelease_Rejected(t *testing.T) {
	_, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": "0.0.0-does-not-exist",
		"mode":            "immediate",
		"target":          map[string]interface{}{"type": "all-online"},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRollout_DeviceListEmpty_Rejected(t *testing.T) {
	_, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type":       "device-list",
			"device_ids": []string{},
		},
	}
	w := postJSON(t, r, "/api/rollouts", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCreateRollout_Idempotent_SecondCallReturns409 verifies the partial
// unique index keeps rollouts from double-booking the same target.
func TestCreateRollout_Idempotent_SecondCallReturns409(t *testing.T) {
	db, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	d1 := makeRolloutTestDevice(t, db, "rollout-idem-1", true)
	defer db.Pool().Exec(context.Background(), "DELETE FROM devices WHERE id=$1", d1)

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type":       "device-list",
			"device_ids": []string{d1.String()},
		},
	}
	w1 := postJSON(t, r, "/api/rollouts", body)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create expected 201, got %d body=%s", w1.Code, w1.Body.String())
	}
	w2 := postJSON(t, r, "/api/rollouts", body)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second create expected 409, got %d body=%s", w2.Code, w2.Body.String())
	}
}

func TestListRollouts_FiltersByStatus(t *testing.T) {
	db, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	d1 := makeRolloutTestDevice(t, db, "rollout-list-1", true)
	defer db.Pool().Exec(context.Background(), "DELETE FROM devices WHERE id=$1", d1)

	r := rolloutTestRouter(services)
	body := map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type":       "device-list",
			"device_ids": []string{d1.String()},
		},
	}
	wCreate := postJSON(t, r, "/api/rollouts", body)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}
	var createResp struct {
		ID uuid.UUID `json:"id"`
	}
	json.Unmarshal(wCreate.Body.Bytes(), &createResp)

	// Filter by status=active — should include the new rollout.
	req := httptest.NewRequest(http.MethodGet, "/api/rollouts?status=active", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, req)
	if wList.Code != http.StatusOK {
		t.Fatalf("list failed: %d %s", wList.Code, wList.Body.String())
	}
	var listResp struct {
		Rollouts []map[string]interface{} `json:"rollouts"`
	}
	json.Unmarshal(wList.Body.Bytes(), &listResp)
	found := false
	for _, item := range listResp.Rollouts {
		if id, ok := item["id"].(string); ok && id == createResp.ID.String() {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find the newly-created rollout in active list")
	}

	// Filter by status=completed — should NOT include this active rollout.
	req2 := httptest.NewRequest(http.MethodGet, "/api/rollouts?status=completed", nil)
	wList2 := httptest.NewRecorder()
	r.ServeHTTP(wList2, req2)
	var listResp2 struct {
		Rollouts []map[string]interface{} `json:"rollouts"`
	}
	json.Unmarshal(wList2.Body.Bytes(), &listResp2)
	for _, item := range listResp2.Rollouts {
		if id, ok := item["id"].(string); ok && id == createResp.ID.String() {
			t.Error("active rollout should not appear in completed list")
		}
	}
}

// TestCancelRollout_TransitionsAndHidesFromHeartbeatLookup proves cancel works
// AND that the heartbeat-ack lookup query (filtering on r.status='active')
// returns zero rows for cancelled rollouts. This is the second contract test
// for the override-on-rollout semantics — it must not survive cancellation.
func TestCancelRollout_TransitionsAndHidesFromHeartbeatLookup(t *testing.T) {
	db, services, cleanup := setupRolloutTest(t)
	defer cleanup()

	d1 := makeRolloutTestDevice(t, db, "rollout-cancel-1", true)
	defer db.Pool().Exec(context.Background(), "DELETE FROM devices WHERE id=$1", d1)

	r := rolloutTestRouter(services)
	wCreate := postJSON(t, r, "/api/rollouts", map[string]interface{}{
		"release_version": rolloutsTestRelease,
		"mode":            "immediate",
		"target": map[string]interface{}{
			"type":       "device-list",
			"device_ids": []string{d1.String()},
		},
	})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}
	var createResp struct {
		ID uuid.UUID `json:"id"`
	}
	json.Unmarshal(wCreate.Body.Bytes(), &createResp)

	// Cancel.
	req := httptest.NewRequest(http.MethodPost, "/api/rollouts/"+createResp.ID.String()+"/cancel", nil)
	wCancel := httptest.NewRecorder()
	r.ServeHTTP(wCancel, req)
	if wCancel.Code != http.StatusOK {
		t.Fatalf("cancel expected 200, got %d body=%s", wCancel.Code, wCancel.Body.String())
	}

	ctx := context.Background()
	var rolloutStatus, stageStatus string
	db.Pool().QueryRow(ctx, "SELECT status FROM rollouts WHERE id=$1", createResp.ID).Scan(&rolloutStatus)
	db.Pool().QueryRow(ctx, "SELECT status FROM rollout_stages WHERE rollout_id=$1", createResp.ID).Scan(&stageStatus)
	if rolloutStatus != "cancelled" {
		t.Errorf("expected rollout status=cancelled, got %q", rolloutStatus)
	}
	if stageStatus != "cancelled" {
		t.Errorf("expected stage status=cancelled, got %q", stageStatus)
	}

	// Heartbeat-ack lookup contract: with rollout cancelled, the dispatch
	// query returns zero rows for the device (so it falls back to the legacy
	// global-version path).
	var dispatchHits int
	err := db.Pool().QueryRow(ctx, `
		SELECT COUNT(*)
		FROM rollout_devices rd
		JOIN rollouts r ON r.id = rd.rollout_id
		WHERE rd.device_id = $1
		  AND rd.status = 'pending'
		  AND r.status = 'active'
	`, d1).Scan(&dispatchHits)
	if err != nil {
		t.Fatalf("dispatch lookup query: %v", err)
	}
	if dispatchHits != 0 {
		t.Errorf("expected 0 dispatch hits for cancelled rollout, got %d", dispatchHits)
	}

	// Cancelling an already-cancelled rollout should be 409.
	req2 := httptest.NewRequest(http.MethodPost, "/api/rollouts/"+createResp.ID.String()+"/cancel", nil)
	wCancel2 := httptest.NewRecorder()
	r.ServeHTTP(wCancel2, req2)
	if wCancel2.Code != http.StatusConflict {
		t.Errorf("expected 409 on second cancel, got %d", wCancel2.Code)
	}
}

// TestRolloutTicker_AllSucceeded_MarksCompleted exercises the ticker by
// directly inserting rollout_devices rows in 'succeeded' and invoking
// finaliseRolloutIfDone. Avoids depending on the goroutine timing.
func TestRolloutTicker_AllSucceeded_MarksCompleted(t *testing.T) {
	db, _, cleanup := setupRolloutTest(t)
	defer cleanup()

	ctx := context.Background()
	d1 := makeRolloutTestDevice(t, db, "ticker-success-1", true)
	d2 := makeRolloutTestDevice(t, db, "ticker-success-2", true)
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE id IN ($1,$2)", d1, d2)

	rolloutID := insertRolloutForTicker(t, db, "active", 20.0)
	stageID := insertStageForTicker(t, db, rolloutID, "active", 2)
	insertRolloutDevice(t, db, rolloutID, stageID, d1, "succeeded")
	insertRolloutDevice(t, db, rolloutID, stageID, d2, "succeeded")

	finaliseRolloutIfDone(ctx, db, rolloutID, 20.0)

	var status string
	db.Pool().QueryRow(ctx, "SELECT status FROM rollouts WHERE id=$1", rolloutID).Scan(&status)
	if status != "completed" {
		t.Errorf("expected rollout status=completed, got %q", status)
	}
}

func TestRolloutTicker_OverThreshold_MarksFailed(t *testing.T) {
	db, _, cleanup := setupRolloutTest(t)
	defer cleanup()

	ctx := context.Background()
	d1 := makeRolloutTestDevice(t, db, "ticker-fail-1", true)
	d2 := makeRolloutTestDevice(t, db, "ticker-fail-2", true)
	d3 := makeRolloutTestDevice(t, db, "ticker-fail-3", true)
	d4 := makeRolloutTestDevice(t, db, "ticker-fail-4", true)
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE id IN ($1,$2,$3,$4)", d1, d2, d3, d4)

	rolloutID := insertRolloutForTicker(t, db, "active", 20.0)
	stageID := insertStageForTicker(t, db, rolloutID, "active", 4)
	// 3 succeeded, 1 failed = 25% failure, > 20% threshold → fail.
	insertRolloutDevice(t, db, rolloutID, stageID, d1, "succeeded")
	insertRolloutDevice(t, db, rolloutID, stageID, d2, "succeeded")
	insertRolloutDevice(t, db, rolloutID, stageID, d3, "succeeded")
	insertRolloutDevice(t, db, rolloutID, stageID, d4, "failed")

	finaliseRolloutIfDone(ctx, db, rolloutID, 20.0)

	var status string
	db.Pool().QueryRow(ctx, "SELECT status FROM rollouts WHERE id=$1", rolloutID).Scan(&status)
	if status != "failed" {
		t.Errorf("expected rollout status=failed (failure rate 25%% > 20%% threshold), got %q", status)
	}
}

// TestRolloutTicker_StillPending_NoFinalise verifies a partially-done rollout
// is left alone.
func TestRolloutTicker_StillPending_NoFinalise(t *testing.T) {
	db, _, cleanup := setupRolloutTest(t)
	defer cleanup()

	ctx := context.Background()
	d1 := makeRolloutTestDevice(t, db, "ticker-pending-1", true)
	d2 := makeRolloutTestDevice(t, db, "ticker-pending-2", true)
	defer db.Pool().Exec(ctx, "DELETE FROM devices WHERE id IN ($1,$2)", d1, d2)

	rolloutID := insertRolloutForTicker(t, db, "active", 20.0)
	stageID := insertStageForTicker(t, db, rolloutID, "active", 2)
	insertRolloutDevice(t, db, rolloutID, stageID, d1, "succeeded")
	insertRolloutDevice(t, db, rolloutID, stageID, d2, "dispatched")

	finaliseRolloutIfDone(ctx, db, rolloutID, 20.0)

	var status string
	db.Pool().QueryRow(ctx, "SELECT status FROM rollouts WHERE id=$1", rolloutID).Scan(&status)
	if status != "active" {
		t.Errorf("expected rollout to remain active (1 device still dispatched), got %q", status)
	}
}

// helpers for ticker tests --------------------------------------------------

func insertRolloutForTicker(t *testing.T, db *database.DB, status string, failurePct float32) uuid.UUID {
	t.Helper()
	id := uuid.New()
	hash := fmt.Sprintf("test-%s", id.String())
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollouts (
			id, organization_id, release_version, name, status, mode, channel,
			target_type, target_spec, target_hash, failure_threshold_percent,
			created_by, started_at, created_at, updated_at
		) VALUES ($1, 1, $2, 'ticker-test', $3, 'immediate', 'stable',
		          'device-list', '{}'::jsonb, $4, $5, 'test', NOW(), NOW(), NOW())
	`, id, rolloutsTestRelease, status, hash, failurePct)
	if err != nil {
		t.Fatalf("insert rollout: %v", err)
	}
	return id
}

func insertStageForTicker(t *testing.T, db *database.DB, rolloutID uuid.UUID, status string, total int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollout_stages (id, rollout_id, group_id, status, total_devices, started_at, created_at)
		VALUES ($1, $2, NULL, $3, $4, NOW(), NOW())
	`, id, rolloutID, status, total)
	if err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	return id
}

func insertRolloutDevice(t *testing.T, db *database.DB, rolloutID, stageID, deviceID uuid.UUID, status string) {
	t.Helper()
	var completedAt *time.Time
	if status == "succeeded" || status == "failed" {
		now := time.Now().UTC()
		completedAt = &now
	}
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollout_devices (
			id, rollout_id, stage_id, device_id, status,
			from_version, to_version, attempts, created_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, '1.77.0', $6, 0, NOW(), $7)
	`, uuid.New(), rolloutID, stageID, deviceID, status, rolloutsTestRelease, completedAt)
	if err != nil {
		t.Fatalf("insert rollout_device: %v", err)
	}
}

// TestComputeTargetHash_DeterministicAndOrderInsensitive proves the hash is
// stable across different input orderings — the idempotency contract relies
// on this.
func TestComputeTargetHash_DeterministicAndOrderInsensitive(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	c := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	h1 := computeTargetHash("device-list", []uuid.UUID{a, b, c})
	h2 := computeTargetHash("device-list", []uuid.UUID{c, b, a})
	h3 := computeTargetHash("device-list", []uuid.UUID{b, a, c})
	if h1 != h2 || h2 != h3 {
		t.Errorf("hash should be order-insensitive; got %s / %s / %s", h1, h2, h3)
	}

	// Different type → different hash.
	hOnline := computeTargetHash("all-online", []uuid.UUID{a, b, c})
	if hOnline == h1 {
		t.Error("hash should differ when target type differs")
	}

	// Empty set still hashes (used by all-online with zero matches).
	hEmpty := computeTargetHash("all-online", nil)
	if hEmpty == "" || len(hEmpty) != 64 {
		t.Errorf("empty-set hash should be 64 hex chars, got %q", hEmpty)
	}
}
