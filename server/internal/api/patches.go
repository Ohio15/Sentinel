package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// PatchPolicy represents a patch management policy
type PatchPolicy struct {
	ID                    uuid.UUID  `json:"id"`
	OrganizationID        uuid.UUID  `json:"organizationId"`
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	AutoApproveSecurity   bool       `json:"autoApproveSecurity"`
	AutoApproveCritical   bool       `json:"autoApproveCritical"`
	AutoApproveAfterDays  *int       `json:"autoApproveAfterDays"`
	RequireManualApproval bool       `json:"requireManualApproval"`
	MaintenanceWindowStart *string   `json:"maintenanceWindowStart,omitempty"`
	MaintenanceWindowEnd   *string   `json:"maintenanceWindowEnd,omitempty"`
	MaintenanceDays       []int      `json:"maintenanceDays"`
	IsDefault             bool       `json:"isDefault"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

// PatchApproval represents an individual patch approval record
type PatchApproval struct {
	ID            uuid.UUID  `json:"id"`
	KBArticle     string     `json:"kbArticle"`
	Title         string     `json:"title"`
	Classification string   `json:"classification"`
	Status        string     `json:"status"`
	ApprovedBy    *uuid.UUID `json:"approvedBy,omitempty"`
	ApprovedAt    *time.Time `json:"approvedAt,omitempty"`
	DeniedReason  string     `json:"deniedReason,omitempty"`
	AutoApproved  bool       `json:"autoApproved"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// CreatePatchPolicyRequest is the request body for creating a policy
type CreatePatchPolicyRequest struct {
	Name                  string   `json:"name" binding:"required"`
	Description           string   `json:"description"`
	AutoApproveSecurity   bool     `json:"autoApproveSecurity"`
	AutoApproveCritical   bool     `json:"autoApproveCritical"`
	AutoApproveAfterDays  *int     `json:"autoApproveAfterDays"`
	RequireManualApproval bool     `json:"requireManualApproval"`
	MaintenanceWindowStart string  `json:"maintenanceWindowStart"`
	MaintenanceWindowEnd   string  `json:"maintenanceWindowEnd"`
	MaintenanceDays       []int    `json:"maintenanceDays"`
	IsDefault             bool     `json:"isDefault"`
}

// ApprovalRequest is the request body for approving/denying a patch
type ApprovalRequest struct {
	Status string `json:"status" binding:"required,oneof=approved denied"`
	Reason string `json:"reason"`
}

func listPatchPoliciesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, organization_id, name, COALESCE(description, ''),
			       auto_approve_security, auto_approve_critical, auto_approve_after_days,
			       require_manual_approval, maintenance_window_start, maintenance_window_end,
			       COALESCE(maintenance_days, '{}'), is_default, created_at, updated_at
			FROM patch_policies
			WHERE organization_id = $1
			ORDER BY is_default DESC, name
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Patches] Error listing policies: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list policies"})
			return
		}
		defer rows.Close()

		policies := []PatchPolicy{}
		for rows.Next() {
			var p PatchPolicy
			var windowStart, windowEnd *time.Time

			err := rows.Scan(
				&p.ID, &p.OrganizationID, &p.Name, &p.Description,
				&p.AutoApproveSecurity, &p.AutoApproveCritical, &p.AutoApproveAfterDays,
				&p.RequireManualApproval, &windowStart, &windowEnd,
				&p.MaintenanceDays, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt,
			)
			if err != nil {
				continue
			}

			if windowStart != nil {
				s := windowStart.Format("15:04")
				p.MaintenanceWindowStart = &s
			}
			if windowEnd != nil {
				e := windowEnd.Format("15:04")
				p.MaintenanceWindowEnd = &e
			}

			policies = append(policies, p)
		}

		c.JSON(http.StatusOK, policies)
	}
}

func createPatchPolicyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreatePatchPolicyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		ctx := context.Background()
		userID := c.MustGet("userId").(uuid.UUID)
		policyID := uuid.New()

		// If this is set as default, unset other defaults
		if req.IsDefault {
			services.DB.Pool().Exec(ctx, `
				UPDATE patch_policies SET is_default = false WHERE organization_id = $1
			`, constants.CurrentOrganizationID)
		}

		_, err := services.DB.Pool().Exec(ctx, `
			INSERT INTO patch_policies (
				id, organization_id, name, description,
				auto_approve_security, auto_approve_critical, auto_approve_after_days,
				require_manual_approval, maintenance_window_start, maintenance_window_end,
				maintenance_days, is_default, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::time, $10::time, $11, $12, $13)
		`, policyID, constants.CurrentOrganizationID, req.Name, req.Description,
			req.AutoApproveSecurity, req.AutoApproveCritical, req.AutoApproveAfterDays,
			req.RequireManualApproval, nullIfEmpty(req.MaintenanceWindowStart), nullIfEmpty(req.MaintenanceWindowEnd),
			req.MaintenanceDays, req.IsDefault, userID)

		if err != nil {
			log.Printf("[Patches] Error creating policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create policy"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":      policyID,
			"message": "Policy created successfully",
		})
	}
}

func getPatchPolicyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy ID"})
			return
		}

		ctx := context.Background()
		var p PatchPolicy
		var windowStart, windowEnd *time.Time

		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, organization_id, name, COALESCE(description, ''),
			       auto_approve_security, auto_approve_critical, auto_approve_after_days,
			       require_manual_approval, maintenance_window_start, maintenance_window_end,
			       COALESCE(maintenance_days, '{}'), is_default, created_at, updated_at
			FROM patch_policies
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&p.ID, &p.OrganizationID, &p.Name, &p.Description,
			&p.AutoApproveSecurity, &p.AutoApproveCritical, &p.AutoApproveAfterDays,
			&p.RequireManualApproval, &windowStart, &windowEnd,
			&p.MaintenanceDays, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found"})
			return
		}

		if windowStart != nil {
			s := windowStart.Format("15:04")
			p.MaintenanceWindowStart = &s
		}
		if windowEnd != nil {
			e := windowEnd.Format("15:04")
			p.MaintenanceWindowEnd = &e
		}

		c.JSON(http.StatusOK, p)
	}
}

func updatePatchPolicyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy ID"})
			return
		}

		var req CreatePatchPolicyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()

		// If this is set as default, unset other defaults
		if req.IsDefault {
			services.DB.Pool().Exec(ctx, `
				UPDATE patch_policies SET is_default = false WHERE organization_id = $1 AND id != $2
			`, constants.CurrentOrganizationID, id)
		}

		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE patch_policies SET
				name = $3, description = $4,
				auto_approve_security = $5, auto_approve_critical = $6,
				auto_approve_after_days = $7, require_manual_approval = $8,
				maintenance_window_start = $9::time, maintenance_window_end = $10::time,
				maintenance_days = $11, is_default = $12, updated_at = NOW()
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID, req.Name, req.Description,
			req.AutoApproveSecurity, req.AutoApproveCritical, req.AutoApproveAfterDays,
			req.RequireManualApproval, nullIfEmpty(req.MaintenanceWindowStart), nullIfEmpty(req.MaintenanceWindowEnd),
			req.MaintenanceDays, req.IsDefault)

		if err != nil {
			log.Printf("[Patches] Error updating policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update policy"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Policy updated successfully"})
	}
}

func deletePatchPolicyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy ID"})
			return
		}

		ctx := context.Background()
		result, err := services.DB.Pool().Exec(ctx, `
			DELETE FROM patch_policies WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Patches] Error deleting policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete policy"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Policy deleted successfully"})
	}
}

func listPatchApprovalsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		status := c.Query("status") // Filter by status if provided

		query := `
			SELECT id, kb_article, title, COALESCE(classification, ''),
			       status, approved_by, approved_at, COALESCE(denied_reason, ''),
			       auto_approved, created_at, updated_at
			FROM patch_approvals
			WHERE organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}

		if status != "" {
			query += " AND status = $2"
			args = append(args, status)
		}

		query += " ORDER BY created_at DESC"

		rows, err := services.DB.Pool().Query(ctx, query, args...)
		if err != nil {
			log.Printf("[Patches] Error listing approvals: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list approvals"})
			return
		}
		defer rows.Close()

		approvals := []PatchApproval{}
		for rows.Next() {
			var a PatchApproval
			err := rows.Scan(
				&a.ID, &a.KBArticle, &a.Title, &a.Classification,
				&a.Status, &a.ApprovedBy, &a.ApprovedAt, &a.DeniedReason,
				&a.AutoApproved, &a.CreatedAt, &a.UpdatedAt,
			)
			if err != nil {
				continue
			}
			approvals = append(approvals, a)
		}

		c.JSON(http.StatusOK, approvals)
	}
}

func approvePatchHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid approval ID"})
			return
		}

		var req ApprovalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()
		userID := c.MustGet("userId").(uuid.UUID)

		var query string
		var args []interface{}

		if req.Status == "approved" {
			query = `
				UPDATE patch_approvals SET
					status = 'approved', approved_by = $3, approved_at = NOW(),
					denied_reason = NULL, updated_at = NOW()
				WHERE id = $1 AND organization_id = $2
			`
			args = []interface{}{id, constants.CurrentOrganizationID, userID}
		} else {
			query = `
				UPDATE patch_approvals SET
					status = 'denied', denied_reason = $3, updated_at = NOW()
				WHERE id = $1 AND organization_id = $2
			`
			args = []interface{}{id, constants.CurrentOrganizationID, req.Reason}
		}

		result, err := services.DB.Pool().Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[Patches] Error updating approval: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update approval"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Approval not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Patch " + req.Status + " successfully",
			"status":  req.Status,
		})
	}
}

func bulkApprovePatchesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			IDs    []uuid.UUID `json:"ids" binding:"required"`
			Status string      `json:"status" binding:"required,oneof=approved denied"`
			Reason string      `json:"reason"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()
		userID := c.MustGet("userId").(uuid.UUID)

		var updated int64
		for _, id := range req.IDs {
			var result interface {
				RowsAffected() int64
			}
			var err error

			if req.Status == "approved" {
				result, err = services.DB.Pool().Exec(ctx, `
					UPDATE patch_approvals SET
						status = 'approved', approved_by = $3, approved_at = NOW(), updated_at = NOW()
					WHERE id = $1 AND organization_id = $2
				`, id, constants.CurrentOrganizationID, userID)
			} else {
				result, err = services.DB.Pool().Exec(ctx, `
					UPDATE patch_approvals SET
						status = 'denied', denied_reason = $3, updated_at = NOW()
					WHERE id = $1 AND organization_id = $2
				`, id, constants.CurrentOrganizationID, req.Reason)
			}

			if err == nil {
				updated += result.RowsAffected()
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Patches updated successfully",
			"updated": updated,
		})
	}
}

// getPendingPatchesForDevice returns patches waiting for approval for a device
func getPendingPatchesForDeviceHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		ctx := context.Background()

		// Get device's pending updates from device_updates table
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT u.kb_number, u.title, u.classification,
			       COALESCE(pa.status, 'not_reviewed') as approval_status,
			       pa.id as approval_id
			FROM device_updates du
			CROSS JOIN LATERAL jsonb_array_elements(du.update_details) as u(data)
			LEFT JOIN patch_approvals pa ON pa.kb_article = u.data->>'kb_number'
			    AND pa.organization_id = $2
			WHERE du.device_id = $1
			  AND (u.data->>'is_installed')::boolean = false
			ORDER BY u.data->>'classification', u.data->>'title'
		`, deviceID, constants.CurrentOrganizationID)

		if err != nil {
			// If the query fails (possibly due to schema differences), return empty
			c.JSON(http.StatusOK, []map[string]interface{}{})
			return
		}
		defer rows.Close()

		patches := []map[string]interface{}{}
		for rows.Next() {
			var kb, title, classification, status string
			var approvalID *uuid.UUID

			if err := rows.Scan(&kb, &title, &classification, &status, &approvalID); err != nil {
				continue
			}

			patch := map[string]interface{}{
				"kbArticle":      kb,
				"title":          title,
				"classification": classification,
				"approvalStatus": status,
			}
			if approvalID != nil {
				patch["approvalId"] = approvalID
			}
			patches = append(patches, patch)
		}

		c.JSON(http.StatusOK, patches)
	}
}

// Helper function
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
