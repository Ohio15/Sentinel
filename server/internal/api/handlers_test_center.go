package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// TestRun represents a single execution of a test suite
type TestRun struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID int        `json:"organizationId"`
	Project        string     `json:"project"`
	Branch         string     `json:"branch"`
	CommitSHA      *string    `json:"commitSha"`
	TriggerType    string     `json:"triggerType"`
	Status         string     `json:"status"`
	TotalTests     int        `json:"totalTests"`
	Passed         int        `json:"passed"`
	Failed         int        `json:"failed"`
	Skipped        int        `json:"skipped"`
	DurationMs     *int       `json:"durationMs"`
	Environment    *string    `json:"environment"`
	Runner         *string    `json:"runner"`
	Summary        *string    `json:"summary"`
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// TestResult represents an individual test result within a run
type TestResult struct {
	ID           uuid.UUID `json:"id"`
	RunID        uuid.UUID `json:"runId"`
	TestName     string    `json:"testName"`
	Suite        *string   `json:"suite"`
	Status       string    `json:"status"`
	DurationMs   *int      `json:"durationMs"`
	ErrorMessage *string   `json:"errorMessage"`
	StackTrace   *string   `json:"stackTrace"`
	RetryCount   int       `json:"retryCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// TestIssue represents an aggregated issue tracked across runs
type TestIssue struct {
	ID              uuid.UUID  `json:"id"`
	OrganizationID  int        `json:"organizationId"`
	Project         string     `json:"project"`
	TestName        string     `json:"testName"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	Severity        string     `json:"severity"`
	FirstSeenAt     time.Time  `json:"firstSeenAt"`
	LastSeenAt      time.Time  `json:"lastSeenAt"`
	OccurrenceCount int        `json:"occurrenceCount"`
	FirstRunID      *uuid.UUID `json:"firstRunId"`
	LastRunID       *uuid.UUID `json:"lastRunId"`
	AssignedTo      *uuid.UUID `json:"assignedTo"`
	ResolvedBy      *uuid.UUID `json:"resolvedBy"`
	ResolvedAt      *time.Time `json:"resolvedAt"`
	Notes           *string    `json:"notes"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	// Joined fields
	AssignedToEmail string `json:"assignedToEmail,omitempty"`
	ResolvedByEmail string `json:"resolvedByEmail,omitempty"`
}

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// SubmitTestRunRequest is the payload from the cron job / CI runner
type SubmitTestRunRequest struct {
	Project     string                  `json:"project" binding:"required"`
	Branch      string                  `json:"branch"`
	CommitSHA   string                  `json:"commitSha"`
	TriggerType string                  `json:"triggerType"`
	Status      string                  `json:"status" binding:"required"`
	TotalTests  int                     `json:"totalTests"`
	Passed      int                     `json:"passed"`
	Failed      int                     `json:"failed"`
	Skipped     int                     `json:"skipped"`
	DurationMs  *int                    `json:"durationMs"`
	Environment string                  `json:"environment"`
	Runner      string                  `json:"runner"`
	Summary     string                  `json:"summary"`
	StartedAt   *time.Time              `json:"startedAt"`
	FinishedAt  *time.Time              `json:"finishedAt"`
	Results     []SubmitTestResultEntry `json:"results"`
}

// SubmitTestResultEntry is one test result inside a submission
type SubmitTestResultEntry struct {
	TestName     string `json:"testName" binding:"required"`
	Suite        string `json:"suite"`
	Status       string `json:"status" binding:"required"`
	DurationMs   *int   `json:"durationMs"`
	ErrorMessage string `json:"errorMessage"`
	StackTrace   string `json:"stackTrace"`
	RetryCount   int    `json:"retryCount"`
}

// UpdateTestIssueRequest is the payload for PATCH /issues/:id
type UpdateTestIssueRequest struct {
	Status     *string    `json:"status"`
	Severity   *string    `json:"severity"`
	AssignedTo *uuid.UUID `json:"assignedTo"`
	Notes      *string    `json:"notes"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// submitTestResultsHandler ingests a complete test run with individual results.
// POST /api/admin/test-results
func submitTestResultsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req SubmitTestRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		// Defaults
		if req.Branch == "" {
			req.Branch = "master"
		}
		if req.TriggerType == "" {
			req.TriggerType = "cron"
		}

		// Validate status
		validRunStatuses := map[string]bool{"running": true, "passed": true, "failed": true, "error": true}
		if !validRunStatuses[req.Status] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status; must be one of: running, passed, failed, error"})
			return
		}

		ctx := context.Background()
		pool := services.DB.Pool()
		orgID := constants.CurrentOrganizationID

		// Begin transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("[TestCenter] Failed to begin transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		defer tx.Rollback(ctx)

		// Insert test_run
		startedAt := time.Now()
		if req.StartedAt != nil {
			startedAt = *req.StartedAt
		}

		var runID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO test_runs (
				organization_id, project, branch, commit_sha, trigger_type,
				status, total_tests, passed, failed, skipped,
				duration_ms, environment, runner, summary, started_at, finished_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			RETURNING id
		`,
			orgID, req.Project, req.Branch, nilIfEmpty(req.CommitSHA), req.TriggerType,
			req.Status, req.TotalTests, req.Passed, req.Failed, req.Skipped,
			req.DurationMs, nilIfEmpty(req.Environment), nilIfEmpty(req.Runner),
			nilIfEmpty(req.Summary), startedAt, req.FinishedAt,
		).Scan(&runID)
		if err != nil {
			log.Printf("[TestCenter] Failed to insert test_run: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create test run"})
			return
		}

		// Bulk insert test_results
		validResultStatuses := map[string]bool{"passed": true, "failed": true, "skipped": true, "error": true}
		for _, r := range req.Results {
			if !validResultStatuses[r.Status] {
				continue
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO test_results (run_id, test_name, suite, status, duration_ms, error_message, stack_trace, retry_count)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			`,
				runID, r.TestName, nilIfEmpty(r.Suite), r.Status,
				r.DurationMs, nilIfEmpty(r.ErrorMessage), nilIfEmpty(r.StackTrace), r.RetryCount,
			)
			if err != nil {
				log.Printf("[TestCenter] Failed to insert test_result for '%s': %v", r.TestName, err)
				// Continue — partial insertion is acceptable
			}
		}

		// Upsert test_issues for each failed result
		for _, r := range req.Results {
			if r.Status != "failed" && r.Status != "error" {
				continue
			}
			title := r.TestName
			if r.ErrorMessage != "" && len(r.ErrorMessage) < 500 {
				title = r.ErrorMessage
			}

			// Try to find existing open issue for this test
			var existingID uuid.UUID
			err = tx.QueryRow(ctx, `
				SELECT id FROM test_issues
				WHERE organization_id = $1 AND project = $2 AND test_name = $3
				  AND status IN ('open', 'acknowledged')
			`, orgID, req.Project, r.TestName).Scan(&existingID)

			if err == nil {
				// Update existing issue
				_, err = tx.Exec(ctx, `
					UPDATE test_issues
					SET last_seen_at = NOW(),
					    occurrence_count = occurrence_count + 1,
					    last_run_id = $1,
					    updated_at = NOW()
					WHERE id = $2
				`, runID, existingID)
				if err != nil {
					log.Printf("[TestCenter] Failed to update test_issue %s: %v", existingID, err)
				}
			} else {
				// Create new issue
				_, err = tx.Exec(ctx, `
					INSERT INTO test_issues (
						organization_id, project, test_name, title, status, severity,
						first_seen_at, last_seen_at, occurrence_count,
						first_run_id, last_run_id
					) VALUES ($1,$2,$3,$4,'open','medium',NOW(),NOW(),1,$5,$5)
					ON CONFLICT (organization_id, project, test_name) WHERE status IN ('open', 'acknowledged')
					DO UPDATE SET
						last_seen_at = NOW(),
						occurrence_count = test_issues.occurrence_count + 1,
						last_run_id = $5,
						updated_at = NOW()
				`, orgID, req.Project, r.TestName, title, runID)
				if err != nil {
					log.Printf("[TestCenter] Failed to upsert test_issue for '%s': %v", r.TestName, err)
				}
			}
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("[TestCenter] Failed to commit transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit test results"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":      runID,
			"message": "Test run recorded",
			"results": len(req.Results),
		})
	}
}

// listTestRunsHandler returns test runs with filters and pagination.
// GET /api/admin/test-center/runs
func listTestRunsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Parse pagination
		page := 1
		pageSize := 25
		const maxPageSize = 100

		if p := c.Query("page"); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
				page = parsed
			}
		}
		if ps := c.Query("pageSize"); ps != "" {
			if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
				if parsed > maxPageSize {
					pageSize = maxPageSize
				} else {
					pageSize = parsed
				}
			}
		}
		offset := (page - 1) * pageSize

		// Build dynamic WHERE
		baseQuery := " FROM test_runs WHERE organization_id = $1"
		args := []interface{}{constants.CurrentOrganizationID}
		argNum := 2

		if project := c.Query("project"); project != "" {
			baseQuery += fmt.Sprintf(" AND project = $%d", argNum)
			args = append(args, project)
			argNum++
		}
		if status := c.Query("status"); status != "" {
			baseQuery += fmt.Sprintf(" AND status = $%d", argNum)
			args = append(args, status)
			argNum++
		}
		if branch := c.Query("branch"); branch != "" {
			baseQuery += fmt.Sprintf(" AND branch = $%d", argNum)
			args = append(args, branch)
			argNum++
		}

		// Count
		var total int
		if err := services.DB.Pool().QueryRow(ctx, "SELECT COUNT(*)"+baseQuery, args...).Scan(&total); err != nil {
			log.Printf("[TestCenter] Error counting runs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count test runs"})
			return
		}

		totalPages := (total + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}

		// Query
		selectQuery := `
			SELECT id, organization_id, project, branch, commit_sha, trigger_type,
			       status, total_tests, passed, failed, skipped,
			       duration_ms, environment, runner, summary,
			       started_at, finished_at, created_at
		` + baseQuery + fmt.Sprintf(" ORDER BY started_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
		args = append(args, pageSize, offset)

		rows, err := services.DB.Pool().Query(ctx, selectQuery, args...)
		if err != nil {
			log.Printf("[TestCenter] Error listing runs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list test runs"})
			return
		}
		defer rows.Close()

		runs := make([]TestRun, 0)
		for rows.Next() {
			var r TestRun
			if err := rows.Scan(
				&r.ID, &r.OrganizationID, &r.Project, &r.Branch, &r.CommitSHA, &r.TriggerType,
				&r.Status, &r.TotalTests, &r.Passed, &r.Failed, &r.Skipped,
				&r.DurationMs, &r.Environment, &r.Runner, &r.Summary,
				&r.StartedAt, &r.FinishedAt, &r.CreatedAt,
			); err != nil {
				log.Printf("[TestCenter] Error scanning run row: %v", err)
				continue
			}
			runs = append(runs, r)
		}

		c.JSON(http.StatusOK, gin.H{
			"runs":       runs,
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		})
	}
}

// getTestRunHandler returns a single test run with its results.
// GET /api/admin/test-center/runs/:id
func getTestRunHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid run ID"})
			return
		}

		ctx := context.Background()

		// Get run
		var r TestRun
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, organization_id, project, branch, commit_sha, trigger_type,
			       status, total_tests, passed, failed, skipped,
			       duration_ms, environment, runner, summary,
			       started_at, finished_at, created_at
			FROM test_runs WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&r.ID, &r.OrganizationID, &r.Project, &r.Branch, &r.CommitSHA, &r.TriggerType,
			&r.Status, &r.TotalTests, &r.Passed, &r.Failed, &r.Skipped,
			&r.DurationMs, &r.Environment, &r.Runner, &r.Summary,
			&r.StartedAt, &r.FinishedAt, &r.CreatedAt,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Test run not found"})
			return
		}

		// Get results for this run
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, run_id, test_name, suite, status, duration_ms, error_message, stack_trace, retry_count, created_at
			FROM test_results WHERE run_id = $1 ORDER BY status DESC, test_name
		`, id)
		if err != nil {
			log.Printf("[TestCenter] Error getting results for run %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get test results"})
			return
		}
		defer rows.Close()

		results := make([]TestResult, 0)
		for rows.Next() {
			var tr TestResult
			if err := rows.Scan(
				&tr.ID, &tr.RunID, &tr.TestName, &tr.Suite, &tr.Status,
				&tr.DurationMs, &tr.ErrorMessage, &tr.StackTrace, &tr.RetryCount, &tr.CreatedAt,
			); err != nil {
				log.Printf("[TestCenter] Error scanning result row: %v", err)
				continue
			}
			results = append(results, tr)
		}

		c.JSON(http.StatusOK, gin.H{
			"run":     r,
			"results": results,
		})
	}
}

// listTestIssuesHandler returns aggregated test issues with filters.
// GET /api/admin/test-center/issues
func listTestIssuesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		page := 1
		pageSize := 25
		const maxPageSize = 100

		if p := c.Query("page"); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
				page = parsed
			}
		}
		if ps := c.Query("pageSize"); ps != "" {
			if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
				if parsed > maxPageSize {
					pageSize = maxPageSize
				} else {
					pageSize = parsed
				}
			}
		}
		offset := (page - 1) * pageSize

		baseQuery := `
			FROM test_issues ti
			LEFT JOIN users ua ON ti.assigned_to = ua.id
			LEFT JOIN users ur ON ti.resolved_by = ur.id
			WHERE ti.organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}
		argNum := 2

		if project := c.Query("project"); project != "" {
			baseQuery += fmt.Sprintf(" AND ti.project = $%d", argNum)
			args = append(args, project)
			argNum++
		}
		if status := c.Query("status"); status != "" {
			baseQuery += fmt.Sprintf(" AND ti.status = $%d", argNum)
			args = append(args, status)
			argNum++
		}
		if severity := c.Query("severity"); severity != "" {
			baseQuery += fmt.Sprintf(" AND ti.severity = $%d", argNum)
			args = append(args, severity)
			argNum++
		}

		// Count
		var total int
		if err := services.DB.Pool().QueryRow(ctx, "SELECT COUNT(*)"+baseQuery, args...).Scan(&total); err != nil {
			log.Printf("[TestCenter] Error counting issues: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count issues"})
			return
		}

		totalPages := (total + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}

		selectQuery := `
			SELECT ti.id, ti.organization_id, ti.project, ti.test_name, ti.title,
			       ti.status, ti.severity, ti.first_seen_at, ti.last_seen_at,
			       ti.occurrence_count, ti.first_run_id, ti.last_run_id,
			       ti.assigned_to, ti.resolved_by, ti.resolved_at, ti.notes,
			       ti.created_at, ti.updated_at,
			       COALESCE(ua.email, '') AS assigned_to_email,
			       COALESCE(ur.email, '') AS resolved_by_email
		` + baseQuery + fmt.Sprintf(" ORDER BY ti.last_seen_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
		args = append(args, pageSize, offset)

		rows, err := services.DB.Pool().Query(ctx, selectQuery, args...)
		if err != nil {
			log.Printf("[TestCenter] Error listing issues: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list issues"})
			return
		}
		defer rows.Close()

		issues := make([]TestIssue, 0)
		for rows.Next() {
			var i TestIssue
			if err := rows.Scan(
				&i.ID, &i.OrganizationID, &i.Project, &i.TestName, &i.Title,
				&i.Status, &i.Severity, &i.FirstSeenAt, &i.LastSeenAt,
				&i.OccurrenceCount, &i.FirstRunID, &i.LastRunID,
				&i.AssignedTo, &i.ResolvedBy, &i.ResolvedAt, &i.Notes,
				&i.CreatedAt, &i.UpdatedAt,
				&i.AssignedToEmail, &i.ResolvedByEmail,
			); err != nil {
				log.Printf("[TestCenter] Error scanning issue row: %v", err)
				continue
			}
			issues = append(issues, i)
		}

		c.JSON(http.StatusOK, gin.H{
			"issues":     issues,
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		})
	}
}

// updateTestIssueHandler updates an existing test issue.
// PATCH /api/admin/test-center/issues/:id
func updateTestIssueHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid issue ID"})
			return
		}

		var req UpdateTestIssueRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		ctx := context.Background()

		// Verify issue exists and belongs to org
		var exists bool
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM test_issues WHERE id = $1 AND organization_id = $2)",
			id, constants.CurrentOrganizationID,
		).Scan(&exists)
		if err != nil || !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Issue not found"})
			return
		}

		// Build dynamic SET clause
		setClauses := []string{"updated_at = NOW()"}
		args := []interface{}{}
		argNum := 1

		if req.Status != nil {
			validStatuses := map[string]bool{"open": true, "acknowledged": true, "resolved": true, "wontfix": true}
			if !validStatuses[*req.Status] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status; must be one of: open, acknowledged, resolved, wontfix"})
				return
			}
			setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
			args = append(args, *req.Status)
			argNum++

			// If resolving, set resolved_at and resolved_by from JWT
			if *req.Status == "resolved" || *req.Status == "wontfix" {
				setClauses = append(setClauses, "resolved_at = NOW()")
				userIDRaw, _ := c.Get("userId")
				if uid, ok := userIDRaw.(uuid.UUID); ok {
					setClauses = append(setClauses, fmt.Sprintf("resolved_by = $%d", argNum))
					args = append(args, uid)
					argNum++
				}
			}
			// If re-opening, clear resolved fields
			if *req.Status == "open" || *req.Status == "acknowledged" {
				setClauses = append(setClauses, "resolved_at = NULL", "resolved_by = NULL")
			}
		}

		if req.Severity != nil {
			validSeverities := map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
			if !validSeverities[*req.Severity] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid severity; must be one of: critical, high, medium, low"})
				return
			}
			setClauses = append(setClauses, fmt.Sprintf("severity = $%d", argNum))
			args = append(args, *req.Severity)
			argNum++
		}

		if req.AssignedTo != nil {
			setClauses = append(setClauses, fmt.Sprintf("assigned_to = $%d", argNum))
			args = append(args, *req.AssignedTo)
			argNum++
		}

		if req.Notes != nil {
			setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argNum))
			args = append(args, *req.Notes)
			argNum++
		}

		if len(setClauses) == 1 {
			// Only "updated_at = NOW()" — nothing to change
			c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
			return
		}

		query := "UPDATE test_issues SET "
		for i, clause := range setClauses {
			if i > 0 {
				query += ", "
			}
			query += clause
		}
		query += fmt.Sprintf(" WHERE id = $%d AND organization_id = $%d", argNum, argNum+1)
		args = append(args, id, constants.CurrentOrganizationID)

		_, err = services.DB.Pool().Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[TestCenter] Failed to update issue %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update issue"})
			return
		}

		// Return updated issue
		var i TestIssue
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT ti.id, ti.organization_id, ti.project, ti.test_name, ti.title,
			       ti.status, ti.severity, ti.first_seen_at, ti.last_seen_at,
			       ti.occurrence_count, ti.first_run_id, ti.last_run_id,
			       ti.assigned_to, ti.resolved_by, ti.resolved_at, ti.notes,
			       ti.created_at, ti.updated_at,
			       COALESCE(ua.email, '') AS assigned_to_email,
			       COALESCE(ur.email, '') AS resolved_by_email
			FROM test_issues ti
			LEFT JOIN users ua ON ti.assigned_to = ua.id
			LEFT JOIN users ur ON ti.resolved_by = ur.id
			WHERE ti.id = $1
		`, id).Scan(
			&i.ID, &i.OrganizationID, &i.Project, &i.TestName, &i.Title,
			&i.Status, &i.Severity, &i.FirstSeenAt, &i.LastSeenAt,
			&i.OccurrenceCount, &i.FirstRunID, &i.LastRunID,
			&i.AssignedTo, &i.ResolvedBy, &i.ResolvedAt, &i.Notes,
			&i.CreatedAt, &i.UpdatedAt,
			&i.AssignedToEmail, &i.ResolvedByEmail,
		)
		if err != nil {
			log.Printf("[TestCenter] Failed to fetch updated issue %s: %v", id, err)
			c.JSON(http.StatusOK, gin.H{"message": "Issue updated"})
			return
		}

		c.JSON(http.StatusOK, i)
	}
}

// testCenterStatsHandler returns summary statistics for the test center dashboard.
// GET /api/admin/test-center/stats
func testCenterStatsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		orgID := constants.CurrentOrganizationID

		type projectStats struct {
			Project    string `json:"project"`
			TotalRuns  int    `json:"totalRuns"`
			LastStatus string `json:"lastStatus"`
			OpenIssues int    `json:"openIssues"`
			PassRate   *float64 `json:"passRate"`
		}

		rows, err := services.DB.Pool().Query(ctx, `
			WITH latest_runs AS (
				SELECT DISTINCT ON (project) project, status
				FROM test_runs
				WHERE organization_id = $1
				ORDER BY project, started_at DESC
			),
			run_counts AS (
				SELECT project, COUNT(*) AS total_runs,
				       ROUND(AVG(CASE WHEN status = 'passed' THEN 100.0 ELSE 0.0 END), 1) AS pass_rate
				FROM test_runs
				WHERE organization_id = $1
				GROUP BY project
			),
			issue_counts AS (
				SELECT project, COUNT(*) AS open_issues
				FROM test_issues
				WHERE organization_id = $1 AND status IN ('open', 'acknowledged')
				GROUP BY project
			)
			SELECT rc.project, rc.total_runs, COALESCE(lr.status, 'unknown'), COALESCE(ic.open_issues, 0), rc.pass_rate
			FROM run_counts rc
			LEFT JOIN latest_runs lr ON rc.project = lr.project
			LEFT JOIN issue_counts ic ON rc.project = ic.project
			ORDER BY rc.project
		`, orgID)
		if err != nil {
			log.Printf("[TestCenter] Error getting stats: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get stats"})
			return
		}
		defer rows.Close()

		stats := make([]projectStats, 0)
		for rows.Next() {
			var s projectStats
			if err := rows.Scan(&s.Project, &s.TotalRuns, &s.LastStatus, &s.OpenIssues, &s.PassRate); err != nil {
				log.Printf("[TestCenter] Error scanning stats row: %v", err)
				continue
			}
			stats = append(stats, s)
		}

		c.JSON(http.StatusOK, gin.H{"projects": stats})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
