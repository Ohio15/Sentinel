package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Ticket handlers

func (r *Router) listTickets(c *gin.Context) {
	ctx := context.Background()
	status := c.Query("status")
	priority := c.Query("priority")
	assignedTo := c.Query("assignedTo")
	deviceID := c.Query("deviceId")
	clientID := c.Query("clientId")

	query := `
		SELECT t.id, t.ticket_number, t.subject, t.description, t.status, t.priority, t.type,
			   t.device_id, d.hostname as device_name, t.client_id, cl.name as client_name,
			   t.requester_name, t.requester_email, t.assigned_to, t.tags, t.due_date,
			   t.category_id, tc.name as category_name,
			   t.sla_policy_id, t.first_response_at, t.first_response_due_at, t.resolution_due_at,
			   t.sla_response_breached, t.sla_resolution_breached,
			   t.resolved_at, t.closed_at, t.created_at, t.updated_at
		FROM tickets t
		LEFT JOIN devices d ON t.device_id = d.id
		LEFT JOIN clients cl ON t.client_id = cl.id
		LEFT JOIN ticket_categories tc ON t.category_id = tc.id
		WHERE 1=1
	`
	args := make([]interface{}, 0)
	argNum := 1

	if status != "" {
		query += " AND t.status = $" + string(rune('0'+argNum))
		args = append(args, status)
		argNum++
	}
	if priority != "" {
		query += " AND t.priority = $" + string(rune('0'+argNum))
		args = append(args, priority)
		argNum++
	}
	if assignedTo != "" {
		query += " AND t.assigned_to = $" + string(rune('0'+argNum))
		args = append(args, assignedTo)
		argNum++
	}
	if deviceID != "" {
		query += " AND t.device_id = $" + string(rune('0'+argNum))
		args = append(args, deviceID)
		argNum++
	}
	if clientID != "" {
		query += " AND t.client_id = $" + string(rune('0'+argNum))
		args = append(args, clientID)
		argNum++
	}

	query += " ORDER BY t.created_at DESC LIMIT 100"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		log.Printf("Error listing tickets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tickets"})
		return
	}
	defer rows.Close()

	tickets := make([]map[string]interface{}, 0)
	for rows.Next() {
		var t struct {
			ID                    uuid.UUID
			TicketNumber          int
			Subject               string
			Description           *string
			Status                string
			Priority              string
			Type                  string
			DeviceID              *uuid.UUID
			DeviceName            *string
			ClientID              *uuid.UUID
			ClientName            *string
			RequesterName         *string
			RequesterEmail        *string
			AssignedTo            *string
			Tags                  json.RawMessage
			DueDate               *time.Time
			CategoryID            *uuid.UUID
			CategoryName          *string
			SLAPolicyID           *uuid.UUID
			FirstResponseAt       *time.Time
			FirstResponseDueAt    *time.Time
			ResolutionDueAt       *time.Time
			SLAResponseBreached   bool
			SLAResolutionBreached bool
			ResolvedAt            *time.Time
			ClosedAt              *time.Time
			CreatedAt             time.Time
			UpdatedAt             time.Time
		}
		if err := rows.Scan(&t.ID, &t.TicketNumber, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.Type,
			&t.DeviceID, &t.DeviceName, &t.ClientID, &t.ClientName,
			&t.RequesterName, &t.RequesterEmail, &t.AssignedTo, &t.Tags, &t.DueDate,
			&t.CategoryID, &t.CategoryName,
			&t.SLAPolicyID, &t.FirstResponseAt, &t.FirstResponseDueAt, &t.ResolutionDueAt,
			&t.SLAResponseBreached, &t.SLAResolutionBreached,
			&t.ResolvedAt, &t.ClosedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			log.Printf("Error scanning ticket row: %v", err)
			continue
		}

		var tags []string
		json.Unmarshal(t.Tags, &tags)

		tickets = append(tickets, map[string]interface{}{
			"id":                    t.ID,
			"ticketNumber":          t.TicketNumber,
			"subject":               t.Subject,
			"description":           t.Description,
			"status":                t.Status,
			"priority":              t.Priority,
			"type":                  t.Type,
			"deviceId":              t.DeviceID,
			"deviceName":            t.DeviceName,
			"clientId":              t.ClientID,
			"clientName":            t.ClientName,
			"requesterName":         t.RequesterName,
			"requesterEmail":        t.RequesterEmail,
			"assignedTo":            t.AssignedTo,
			"tags":                  tags,
			"dueDate":               t.DueDate,
			"categoryId":            t.CategoryID,
			"categoryName":          t.CategoryName,
			"slaPolicyId":           t.SLAPolicyID,
			"firstResponseAt":       t.FirstResponseAt,
			"firstResponseDueAt":    t.FirstResponseDueAt,
			"resolutionDueAt":       t.ResolutionDueAt,
			"slaResponseBreached":   t.SLAResponseBreached,
			"slaResolutionBreached": t.SLAResolutionBreached,
			"resolvedAt":            t.ResolvedAt,
			"closedAt":              t.ClosedAt,
			"createdAt":             t.CreatedAt,
			"updatedAt":             t.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, tickets)
}

func (r *Router) getTicket(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	var t struct {
		ID                    uuid.UUID
		TicketNumber          int
		Subject               string
		Description           *string
		Status                string
		Priority              string
		Type                  string
		DeviceID              *uuid.UUID
		DeviceName            *string
		ClientID              *uuid.UUID
		ClientName            *string
		RequesterName         *string
		RequesterEmail        *string
		AssignedTo            *string
		Tags                  json.RawMessage
		DueDate               *time.Time
		CategoryID            *uuid.UUID
		CategoryName          *string
		SLAPolicyID           *uuid.UUID
		FirstResponseAt       *time.Time
		FirstResponseDueAt    *time.Time
		ResolutionDueAt       *time.Time
		SLAResponseBreached   bool
		SLAResolutionBreached bool
		ResolvedAt            *time.Time
		ClosedAt              *time.Time
		CreatedAt             time.Time
		UpdatedAt             time.Time
		CustomFields          json.RawMessage
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT t.id, t.ticket_number, t.subject, t.description, t.status, t.priority, t.type,
			   t.device_id, d.hostname, t.client_id, cl.name,
			   t.requester_name, t.requester_email, t.assigned_to, t.tags, t.due_date,
			   t.category_id, tc.name,
			   t.sla_policy_id, t.first_response_at, t.first_response_due_at, t.resolution_due_at,
			   t.sla_response_breached, t.sla_resolution_breached,
			   t.resolved_at, t.closed_at, t.created_at, t.updated_at, COALESCE(t.custom_fields, '{}'::jsonb)
		FROM tickets t
		LEFT JOIN devices d ON t.device_id = d.id
		LEFT JOIN clients cl ON t.client_id = cl.id
		LEFT JOIN ticket_categories tc ON t.category_id = tc.id
		WHERE t.id = $1
	`, id).Scan(&t.ID, &t.TicketNumber, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.Type,
		&t.DeviceID, &t.DeviceName, &t.ClientID, &t.ClientName,
		&t.RequesterName, &t.RequesterEmail, &t.AssignedTo, &t.Tags, &t.DueDate,
		&t.CategoryID, &t.CategoryName,
		&t.SLAPolicyID, &t.FirstResponseAt, &t.FirstResponseDueAt, &t.ResolutionDueAt,
		&t.SLAResponseBreached, &t.SLAResolutionBreached,
		&t.ResolvedAt, &t.ClosedAt, &t.CreatedAt, &t.UpdatedAt, &t.CustomFields)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ticket not found"})
		return
	}

	var tags []string
	json.Unmarshal(t.Tags, &tags)

	var customFields map[string]interface{}
	json.Unmarshal(t.CustomFields, &customFields)

	c.JSON(http.StatusOK, gin.H{
		"id":                    t.ID,
		"ticketNumber":          t.TicketNumber,
		"subject":               t.Subject,
		"description":           t.Description,
		"status":                t.Status,
		"priority":              t.Priority,
		"type":                  t.Type,
		"deviceId":              t.DeviceID,
		"deviceName":            t.DeviceName,
		"clientId":              t.ClientID,
		"clientName":            t.ClientName,
		"requesterName":         t.RequesterName,
		"requesterEmail":        t.RequesterEmail,
		"assignedTo":            t.AssignedTo,
		"tags":                  tags,
		"dueDate":               t.DueDate,
		"categoryId":            t.CategoryID,
		"categoryName":          t.CategoryName,
		"slaPolicyId":           t.SLAPolicyID,
		"firstResponseAt":       t.FirstResponseAt,
		"firstResponseDueAt":    t.FirstResponseDueAt,
		"resolutionDueAt":       t.ResolutionDueAt,
		"slaResponseBreached":   t.SLAResponseBreached,
		"slaResolutionBreached": t.SLAResolutionBreached,
		"resolvedAt":            t.ResolvedAt,
		"closedAt":              t.ClosedAt,
		"createdAt":             t.CreatedAt,
		"updatedAt":             t.UpdatedAt,
		"customFields":          customFields,
	})
}

func (r *Router) createTicket(c *gin.Context) {
	var req struct {
		Subject        string                 `json:"subject" binding:"required"`
		Description    string                 `json:"description"`
		Status         string                 `json:"status"`
		Priority       string                 `json:"priority"`
		Type           string                 `json:"type"`
		DeviceID       *string                `json:"deviceId"`
		ClientID       *string                `json:"clientId"`
		RequesterName  string                 `json:"requesterName"`
		RequesterEmail string                 `json:"requesterEmail"`
		AssignedTo     string                 `json:"assignedTo"`
		Tags           []string               `json:"tags"`
		DueDate        *time.Time             `json:"dueDate"`
		CategoryID     *string                `json:"categoryId"`
		CustomFields   map[string]interface{} `json:"customFields"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Status == "" {
		req.Status = "open"
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	if req.Type == "" {
		req.Type = "incident"
	}

	ctx := context.Background()

	var deviceID, clientID, categoryID *uuid.UUID
	if req.DeviceID != nil && *req.DeviceID != "" {
		parsed, _ := uuid.Parse(*req.DeviceID)
		deviceID = &parsed
	}
	if req.ClientID != nil && *req.ClientID != "" {
		parsed, _ := uuid.Parse(*req.ClientID)
		clientID = &parsed
	}
	if req.CategoryID != nil && *req.CategoryID != "" {
		parsed, _ := uuid.Parse(*req.CategoryID)
		categoryID = &parsed
	}

	tagsJSON, _ := json.Marshal(req.Tags)
	customFieldsJSON, _ := json.Marshal(req.CustomFields)

	var id uuid.UUID
	var ticketNumber int
	var createdAt, updatedAt time.Time
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO tickets (subject, description, status, priority, type, device_id, client_id,
			requester_name, requester_email, assigned_to, tags, due_date, category_id, custom_fields)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, ticket_number, created_at, updated_at
	`, req.Subject, req.Description, req.Status, req.Priority, req.Type, deviceID, clientID,
		req.RequesterName, req.RequesterEmail, req.AssignedTo, tagsJSON, req.DueDate, categoryID, customFieldsJSON).Scan(&id, &ticketNumber, &createdAt, &updatedAt)

	if err != nil {
		log.Printf("Error creating ticket: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create ticket"})
		return
	}

	// Log activity
	r.logTicketActivity(ctx, id, "created", "", "", "", "System")

	// Return full ticket data for frontend state management
	c.JSON(http.StatusCreated, gin.H{
		"id":             id,
		"ticketNumber":   ticketNumber,
		"subject":        req.Subject,
		"description":    req.Description,
		"status":         req.Status,
		"priority":       req.Priority,
		"type":           req.Type,
		"deviceId":       deviceID,
		"clientId":       clientID,
		"requesterName":  req.RequesterName,
		"requesterEmail": req.RequesterEmail,
		"assignedTo":     req.AssignedTo,
		"tags":           req.Tags,
		"dueDate":        req.DueDate,
		"categoryId":     categoryID,
		"customFields":   req.CustomFields,
		"createdAt":      createdAt,
		"updatedAt":      updatedAt,
	})
}

func (r *Router) updateTicket(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	var req struct {
		Subject        *string                `json:"subject"`
		Description    *string                `json:"description"`
		Status         *string                `json:"status"`
		Priority       *string                `json:"priority"`
		Type           *string                `json:"type"`
		DeviceID       *string                `json:"deviceId"`
		ClientID       *string                `json:"clientId"`
		RequesterName  *string                `json:"requesterName"`
		RequesterEmail *string                `json:"requesterEmail"`
		AssignedTo     *string                `json:"assignedTo"`
		Tags           []string               `json:"tags"`
		DueDate        *time.Time             `json:"dueDate"`
		CategoryID     *string                `json:"categoryId"`
		CustomFields   map[string]interface{} `json:"customFields"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Get current ticket for activity logging
	var current struct {
		Status   string
		Priority string
	}
	r.db.Pool().QueryRow(ctx, "SELECT status, priority FROM tickets WHERE id = $1", id).Scan(&current.Status, &current.Priority)

	// Build update query dynamically
	updates := "updated_at = NOW()"
	args := make([]interface{}, 0)
	argNum := 1

	if req.Subject != nil {
		updates += ", subject = $" + string(rune('0'+argNum))
		args = append(args, *req.Subject)
		argNum++
	}
	if req.Description != nil {
		updates += ", description = $" + string(rune('0'+argNum))
		args = append(args, *req.Description)
		argNum++
	}
	if req.Status != nil {
		updates += ", status = $" + string(rune('0'+argNum))
		args = append(args, *req.Status)
		argNum++

		if *req.Status == "resolved" {
			updates += ", resolved_at = NOW()"
		} else if *req.Status == "closed" {
			updates += ", closed_at = NOW()"
		}

		if current.Status != *req.Status {
			r.logTicketActivity(ctx, id, "status_changed", "status", current.Status, *req.Status, "System")
		}
	}
	if req.Priority != nil {
		updates += ", priority = $" + string(rune('0'+argNum))
		args = append(args, *req.Priority)
		argNum++

		if current.Priority != *req.Priority {
			r.logTicketActivity(ctx, id, "priority_changed", "priority", current.Priority, *req.Priority, "System")
		}
	}
	if req.Type != nil {
		updates += ", type = $" + string(rune('0'+argNum))
		args = append(args, *req.Type)
		argNum++
	}
	if req.AssignedTo != nil {
		updates += ", assigned_to = $" + string(rune('0'+argNum))
		args = append(args, *req.AssignedTo)
		argNum++
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		updates += ", tags = $" + string(rune('0'+argNum))
		args = append(args, tagsJSON)
		argNum++
	}
	if req.CustomFields != nil {
		customFieldsJSON, _ := json.Marshal(req.CustomFields)
		updates += ", custom_fields = $" + string(rune('0'+argNum))
		args = append(args, customFieldsJSON)
		argNum++
	}

	args = append(args, id)
	query := "UPDATE tickets SET " + updates + " WHERE id = $" + string(rune('0'+argNum))

	_, err = r.db.Pool().Exec(ctx, query, args...)
	if err != nil {
		log.Printf("Error updating ticket: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ticket"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Ticket updated successfully"})
}

func (r *Router) deleteTicket(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM tickets WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting ticket: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete ticket"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ticket not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Ticket deleted successfully"})
}

// Ticket comments
func (r *Router) getTicketComments(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, ticket_id, content, is_internal, author_name, author_email, attachments, created_at
		FROM ticket_comments WHERE ticket_id = $1 ORDER BY created_at ASC
	`, ticketID)
	if err != nil {
		log.Printf("Error listing ticket comments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch comments"})
		return
	}
	defer rows.Close()

	comments := make([]map[string]interface{}, 0)
	for rows.Next() {
		var comment struct {
			ID          uuid.UUID
			TicketID    uuid.UUID
			Content     string
			IsInternal  bool
			AuthorName  string
			AuthorEmail *string
			Attachments json.RawMessage
			CreatedAt   time.Time
		}
		if err := rows.Scan(&comment.ID, &comment.TicketID, &comment.Content, &comment.IsInternal,
			&comment.AuthorName, &comment.AuthorEmail, &comment.Attachments, &comment.CreatedAt); err != nil {
			log.Printf("Error scanning comment row: %v", err)
			continue
		}
		comments = append(comments, map[string]interface{}{
			"id":          comment.ID,
			"ticketId":    comment.TicketID,
			"content":     comment.Content,
			"isInternal":  comment.IsInternal,
			"authorName":  comment.AuthorName,
			"authorEmail": comment.AuthorEmail,
			"attachments": comment.Attachments,
			"createdAt":   comment.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, comments)
}

func (r *Router) addTicketComment(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	var req struct {
		Content     string          `json:"content" binding:"required"`
		IsInternal  bool            `json:"isInternal"`
		AuthorName  string          `json:"authorName" binding:"required"`
		AuthorEmail string          `json:"authorEmail"`
		Attachments json.RawMessage `json:"attachments"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var id uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO ticket_comments (ticket_id, content, is_internal, author_name, author_email, attachments)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, ticketID, req.Content, req.IsInternal, req.AuthorName, req.AuthorEmail, req.Attachments).Scan(&id)

	if err != nil {
		log.Printf("Error adding ticket comment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add comment"})
		return
	}

	// Log activity
	r.logTicketActivity(ctx, ticketID, "comment_added", "", "", "", req.AuthorName)

	c.JSON(http.StatusCreated, gin.H{
		"id":        id,
		"ticketId":  ticketID,
		"content":   req.Content,
		"createdAt": time.Now(),
	})
}

// Ticket activity
func (r *Router) getTicketActivity(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, ticket_id, action, field_name, old_value, new_value, actor_name, created_at
		FROM ticket_activity WHERE ticket_id = $1 ORDER BY created_at DESC
	`, ticketID)
	if err != nil {
		log.Printf("Error listing ticket activity: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activity"})
		return
	}
	defer rows.Close()

	activities := make([]map[string]interface{}, 0)
	for rows.Next() {
		var activity struct {
			ID        uuid.UUID
			TicketID  uuid.UUID
			Action    string
			FieldName *string
			OldValue  *string
			NewValue  *string
			ActorName *string
			CreatedAt time.Time
		}
		if err := rows.Scan(&activity.ID, &activity.TicketID, &activity.Action, &activity.FieldName,
			&activity.OldValue, &activity.NewValue, &activity.ActorName, &activity.CreatedAt); err != nil {
			log.Printf("Error scanning activity row: %v", err)
			continue
		}
		activities = append(activities, map[string]interface{}{
			"id":        activity.ID,
			"ticketId":  activity.TicketID,
			"action":    activity.Action,
			"fieldName": activity.FieldName,
			"oldValue":  activity.OldValue,
			"newValue":  activity.NewValue,
			"actorName": activity.ActorName,
			"createdAt": activity.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, activities)
}

func (r *Router) logTicketActivity(ctx context.Context, ticketID uuid.UUID, action, fieldName, oldValue, newValue, actorName string) {
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO ticket_activity (ticket_id, action, field_name, old_value, new_value, actor_name)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, ticketID, action, fieldName, oldValue, newValue, actorName)
	if err != nil {
		log.Printf("Error logging ticket activity: %v", err)
	}
}

// Ticket stats
func (r *Router) getTicketStats(c *gin.Context) {
	ctx := context.Background()

	// Count by status
	var open, inProgress, waiting, resolved, closed int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE status = 'open'").Scan(&open)
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE status = 'in_progress'").Scan(&inProgress)
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE status = 'waiting'").Scan(&waiting)
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE status = 'resolved'").Scan(&resolved)
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE status = 'closed'").Scan(&closed)

	total := open + inProgress + waiting + resolved + closed

	// Return stats in format expected by frontend
	c.JSON(http.StatusOK, gin.H{
		"openCount":       open,
		"inProgressCount": inProgress,
		"waitingCount":    waiting,
		"resolvedCount":   resolved,
		"closedCount":     closed,
		"totalCount":      total,
	})
}

// Ticket templates
func (r *Router) listTicketTemplates(c *gin.Context) {
	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, subject, content, is_active, created_at, updated_at
		FROM ticket_templates WHERE is_active = true ORDER BY name
	`)
	if err != nil {
		log.Printf("Error listing ticket templates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch templates"})
		return
	}
	defer rows.Close()

	templates := make([]map[string]interface{}, 0)
	for rows.Next() {
		var t struct {
			ID        uuid.UUID
			Name      string
			Subject   *string
			Content   string
			IsActive  bool
			CreatedAt time.Time
			UpdatedAt time.Time
		}
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.Content, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			log.Printf("Error scanning template row: %v", err)
			continue
		}
		templates = append(templates, map[string]interface{}{
			"id":        t.ID,
			"name":      t.Name,
			"subject":   t.Subject,
			"content":   t.Content,
			"isActive":  t.IsActive,
			"createdAt": t.CreatedAt,
			"updatedAt": t.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, templates)
}

func (r *Router) createTicketTemplate(c *gin.Context) {
	var req struct {
		Name    string `json:"name" binding:"required"`
		Subject string `json:"subject"`
		Content string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO ticket_templates (name, subject, content) VALUES ($1, $2, $3) RETURNING id
	`, req.Name, req.Subject, req.Content).Scan(&id)

	if err != nil {
		log.Printf("Error creating ticket template: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) updateTicketTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}

	var req struct {
		Name     string `json:"name"`
		Subject  string `json:"subject"`
		Content  string `json:"content"`
		IsActive *bool  `json:"isActive"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE ticket_templates SET
			name = COALESCE(NULLIF($1, ''), name),
			subject = COALESCE(NULLIF($2, ''), subject),
			content = COALESCE(NULLIF($3, ''), content),
			updated_at = NOW()
		WHERE id = $4
	`, req.Name, req.Subject, req.Content, id)

	if err != nil {
		log.Printf("Error updating ticket template: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update template"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Template updated successfully"})
}

func (r *Router) deleteTicketTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, "DELETE FROM ticket_templates WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting ticket template: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete template"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Template deleted successfully"})
}

// Handler wrappers for tickets
func listTicketsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listTickets
}

func getTicketHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicket
}

func createTicketHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createTicket
}

func updateTicketHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateTicket
}

func deleteTicketHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicket
}

func getTicketCommentsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketComments
}

func addTicketCommentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.addTicketComment
}

func getTicketActivityHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketActivity
}

func getTicketStatsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketStats
}

func listTicketTemplatesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listTicketTemplates
}

func createTicketTemplateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createTicketTemplate
}

func updateTicketTemplateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateTicketTemplate
}

func deleteTicketTemplateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicketTemplate
}

// Additional handler wrappers

func createTicketCommentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.addTicketComment
}

func updateTicketCommentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateTicketComment
}

func deleteTicketCommentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicketComment
}

func getTicketTemplateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketTemplate
}

// Update a ticket comment
func (r *Router) updateTicketComment(c *gin.Context) {
	commentID, err := uuid.Parse(c.Param("commentId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid comment ID"})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, `
		UPDATE ticket_comments SET content = $1, updated_at = NOW()
		WHERE id = $2
	`, req.Content, commentID)

	if err != nil {
		log.Printf("Error updating ticket comment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update comment"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Comment updated successfully"})
}

// Delete a ticket comment
func (r *Router) deleteTicketComment(c *gin.Context) {
	commentID, err := uuid.Parse(c.Param("commentId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid comment ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM ticket_comments WHERE id = $1", commentID)
	if err != nil {
		log.Printf("Error deleting ticket comment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete comment"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Comment deleted successfully"})
}

// Get a single ticket template
func (r *Router) getTicketTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}

	ctx := context.Background()
	var template struct {
		ID             uuid.UUID
		Name           string
		Description    *string
		DefaultSubject string
		DefaultContent string
		Priority       string
		CategoryID     *uuid.UUID
		IsActive       bool
		CreatedAt      time.Time
		UpdatedAt      time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, default_subject, default_content, priority,
			category_id, is_active, created_at, updated_at
		FROM ticket_templates WHERE id = $1
	`, id).Scan(&template.ID, &template.Name, &template.Description, &template.DefaultSubject,
		&template.DefaultContent, &template.Priority, &template.CategoryID, &template.IsActive,
		&template.CreatedAt, &template.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":             template.ID,
		"name":           template.Name,
		"description":    template.Description,
		"defaultSubject": template.DefaultSubject,
		"defaultContent": template.DefaultContent,
		"priority":       template.Priority,
		"categoryId":     template.CategoryID,
		"isActive":       template.IsActive,
		"createdAt":      template.CreatedAt,
		"updatedAt":      template.UpdatedAt,
	})
}
