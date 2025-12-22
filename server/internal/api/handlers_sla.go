package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// SLA Policy handlers

type SLAPolicy struct {
	ID                      uuid.UUID  `json:"id"`
	Name                    string     `json:"name"`
	Description             *string    `json:"description"`
	Priority                string     `json:"priority"`
	ResponseTargetMinutes   int        `json:"responseTargetMinutes"`
	ResolutionTargetMinutes int        `json:"resolutionTargetMinutes"`
	BusinessHoursOnly       bool       `json:"businessHoursOnly"`
	BusinessHoursStart      string     `json:"businessHoursStart"`
	BusinessHoursEnd        string     `json:"businessHoursEnd"`
	BusinessDays            []int64    `json:"businessDays"`
	ClientID                *uuid.UUID `json:"clientId"`
	IsDefault               bool       `json:"isDefault"`
	IsActive                bool       `json:"isActive"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

func (r *Router) listSLAPolicies(c *gin.Context) {
	ctx := context.Background()
	clientID := c.Query("clientId")

	var rows pgx.Rows
	var err error

	if clientID != "" {
		parsedClientID, parseErr := uuid.Parse(clientID)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}
		rows, err = r.db.Pool().Query(ctx, `
			SELECT id, name, description, priority, response_target_minutes, resolution_target_minutes,
				business_hours_only, business_hours_start, business_hours_end, business_days,
				client_id, is_default, is_active, created_at, updated_at
			FROM sla_policies
			WHERE client_id = $1 OR client_id IS NULL
			ORDER BY priority, name
		`, parsedClientID)
	} else {
		rows, err = r.db.Pool().Query(ctx, `
			SELECT id, name, description, priority, response_target_minutes, resolution_target_minutes,
				business_hours_only, business_hours_start, business_hours_end, business_days,
				client_id, is_default, is_active, created_at, updated_at
			FROM sla_policies
			ORDER BY priority, name
		`)
	}

	if err != nil {
		log.Printf("Error listing SLA policies: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch SLA policies"})
		return
	}
	defer rows.Close()

	policies := make([]SLAPolicy, 0)
	for rows.Next() {
		var policy SLAPolicy
		var businessStart, businessEnd time.Time
		if err := rows.Scan(
			&policy.ID, &policy.Name, &policy.Description, &policy.Priority,
			&policy.ResponseTargetMinutes, &policy.ResolutionTargetMinutes,
			&policy.BusinessHoursOnly, &businessStart, &businessEnd, pq.Array(&policy.BusinessDays),
			&policy.ClientID, &policy.IsDefault, &policy.IsActive,
			&policy.CreatedAt, &policy.UpdatedAt,
		); err != nil {
			log.Printf("Error scanning SLA policy row: %v", err)
			continue
		}
		policy.BusinessHoursStart = businessStart.Format("15:04:05")
		policy.BusinessHoursEnd = businessEnd.Format("15:04:05")
		policies = append(policies, policy)
	}

	c.JSON(http.StatusOK, policies)
}

func (r *Router) getSLAPolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid SLA policy ID"})
		return
	}

	ctx := context.Background()
	var policy SLAPolicy
	var businessStart, businessEnd time.Time

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, priority, response_target_minutes, resolution_target_minutes,
			business_hours_only, business_hours_start, business_hours_end, business_days,
			client_id, is_default, is_active, created_at, updated_at
		FROM sla_policies WHERE id = $1
	`, id).Scan(
		&policy.ID, &policy.Name, &policy.Description, &policy.Priority,
		&policy.ResponseTargetMinutes, &policy.ResolutionTargetMinutes,
		&policy.BusinessHoursOnly, &businessStart, &businessEnd, pq.Array(&policy.BusinessDays),
		&policy.ClientID, &policy.IsDefault, &policy.IsActive,
		&policy.CreatedAt, &policy.UpdatedAt,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SLA policy not found"})
		return
	}

	policy.BusinessHoursStart = businessStart.Format("15:04:05")
	policy.BusinessHoursEnd = businessEnd.Format("15:04:05")
	c.JSON(http.StatusOK, policy)
}

func (r *Router) createSLAPolicy(c *gin.Context) {
	var req struct {
		Name                    string   `json:"name" binding:"required"`
		Description             string   `json:"description"`
		Priority                string   `json:"priority" binding:"required"`
		ResponseTargetMinutes   int      `json:"responseTargetMinutes" binding:"required"`
		ResolutionTargetMinutes int      `json:"resolutionTargetMinutes" binding:"required"`
		BusinessHoursOnly       *bool    `json:"businessHoursOnly"`
		BusinessHoursStart      string   `json:"businessHoursStart"`
		BusinessHoursEnd        string   `json:"businessHoursEnd"`
		BusinessDays            []int    `json:"businessDays"`
		ClientID                *string  `json:"clientId"`
		IsDefault               *bool    `json:"isDefault"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	businessHoursOnly := true
	if req.BusinessHoursOnly != nil {
		businessHoursOnly = *req.BusinessHoursOnly
	}

	businessHoursStart := "09:00:00"
	if req.BusinessHoursStart != "" {
		businessHoursStart = req.BusinessHoursStart
	}

	businessHoursEnd := "17:00:00"
	if req.BusinessHoursEnd != "" {
		businessHoursEnd = req.BusinessHoursEnd
	}

	businessDays := []int{1, 2, 3, 4, 5}
	if len(req.BusinessDays) > 0 {
		businessDays = req.BusinessDays
	}

	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}

	var clientID *uuid.UUID
	if req.ClientID != nil && *req.ClientID != "" {
		parsed, err := uuid.Parse(*req.ClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}
		clientID = &parsed
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO sla_policies (
			name, description, priority, response_target_minutes, resolution_target_minutes,
			business_hours_only, business_hours_start, business_hours_end, business_days,
			client_id, is_default
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, req.Name, req.Description, req.Priority, req.ResponseTargetMinutes, req.ResolutionTargetMinutes,
		businessHoursOnly, businessHoursStart, businessHoursEnd, pq.Array(businessDays),
		clientID, isDefault).Scan(&id)

	if err != nil {
		log.Printf("Error creating SLA policy: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create SLA policy"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) updateSLAPolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid SLA policy ID"})
		return
	}

	var req struct {
		Name                    string  `json:"name"`
		Description             string  `json:"description"`
		Priority                string  `json:"priority"`
		ResponseTargetMinutes   *int    `json:"responseTargetMinutes"`
		ResolutionTargetMinutes *int    `json:"resolutionTargetMinutes"`
		BusinessHoursOnly       *bool   `json:"businessHoursOnly"`
		BusinessHoursStart      string  `json:"businessHoursStart"`
		BusinessHoursEnd        string  `json:"businessHoursEnd"`
		BusinessDays            []int   `json:"businessDays"`
		IsDefault               *bool   `json:"isDefault"`
		IsActive                *bool   `json:"isActive"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE sla_policies SET
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			priority = COALESCE(NULLIF($3, ''), priority),
			response_target_minutes = COALESCE($4, response_target_minutes),
			resolution_target_minutes = COALESCE($5, resolution_target_minutes),
			business_hours_only = COALESCE($6, business_hours_only),
			business_hours_start = COALESCE(NULLIF($7, '')::TIME, business_hours_start),
			business_hours_end = COALESCE(NULLIF($8, '')::TIME, business_hours_end),
			business_days = COALESCE($9, business_days),
			is_default = COALESCE($10, is_default),
			is_active = COALESCE($11, is_active),
			updated_at = NOW()
		WHERE id = $12
	`, req.Name, req.Description, req.Priority, req.ResponseTargetMinutes, req.ResolutionTargetMinutes,
		req.BusinessHoursOnly, req.BusinessHoursStart, req.BusinessHoursEnd,
		pq.Array(req.BusinessDays), req.IsDefault, req.IsActive, id)

	if err != nil {
		log.Printf("Error updating SLA policy: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update SLA policy"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "SLA policy updated successfully"})
}

func (r *Router) deleteSLAPolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid SLA policy ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM sla_policies WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting SLA policy: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete SLA policy"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "SLA policy not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "SLA policy deleted successfully"})
}

// Handler wrappers for SLA policies
func listSLAPoliciesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listSLAPolicies
}

func getSLAPolicyHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getSLAPolicy
}

func createSLAPolicyHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createSLAPolicy
}

func updateSLAPolicyHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateSLAPolicy
}

func deleteSLAPolicyHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteSLAPolicy
}
