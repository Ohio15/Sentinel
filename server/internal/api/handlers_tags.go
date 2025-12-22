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

// Ticket Category handlers

func (r *Router) listTicketCategories(c *gin.Context) {
	ctx := context.Background()
	clientID := c.Query("clientId")
	activeOnly := c.Query("active") == "true"

	query := `
		SELECT id, name, description, parent_id, color, icon, sort_order, is_active, client_id, created_at, updated_at
		FROM ticket_categories WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if clientID != "" {
		parsed, err := uuid.Parse(clientID)
		if err == nil {
			query += " AND (client_id = $" + string(rune('0'+argNum)) + " OR client_id IS NULL)"
			args = append(args, parsed)
			argNum++
		}
	}

	if activeOnly {
		query += " AND is_active = TRUE"
	}

	query += " ORDER BY sort_order, name"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		log.Printf("Error listing ticket categories: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch ticket categories"})
		return
	}
	defer rows.Close()

	categories := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var name string
		var description, color, icon *string
		var parentID, clientID *uuid.UUID
		var sortOrder int
		var isActive bool
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&id, &name, &description, &parentID, &color, &icon,
			&sortOrder, &isActive, &clientID, &createdAt, &updatedAt); err != nil {
			log.Printf("Error scanning ticket category row: %v", err)
			continue
		}
		categories = append(categories, map[string]interface{}{
			"id":          id,
			"name":        name,
			"description": description,
			"parentId":    parentID,
			"color":       color,
			"icon":        icon,
			"sortOrder":   sortOrder,
			"isActive":    isActive,
			"clientId":    clientID,
			"createdAt":   createdAt,
			"updatedAt":   updatedAt,
		})
	}

	c.JSON(http.StatusOK, categories)
}

func (r *Router) getTicketCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	ctx := context.Background()
	var category struct {
		ID          uuid.UUID
		Name        string
		Description *string
		ParentID    *uuid.UUID
		Color       *string
		Icon        *string
		SortOrder   int
		IsActive    bool
		ClientID    *uuid.UUID
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, parent_id, color, icon, sort_order, is_active, client_id, created_at, updated_at
		FROM ticket_categories WHERE id = $1
	`, id).Scan(&category.ID, &category.Name, &category.Description, &category.ParentID, &category.Color,
		&category.Icon, &category.SortOrder, &category.IsActive, &category.ClientID, &category.CreatedAt, &category.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          category.ID,
		"name":        category.Name,
		"description": category.Description,
		"parentId":    category.ParentID,
		"color":       category.Color,
		"icon":        category.Icon,
		"sortOrder":   category.SortOrder,
		"isActive":    category.IsActive,
		"clientId":    category.ClientID,
		"createdAt":   category.CreatedAt,
		"updatedAt":   category.UpdatedAt,
	})
}

func (r *Router) createTicketCategory(c *gin.Context) {
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Description string  `json:"description"`
		ParentID    *string `json:"parentId"`
		Color       string  `json:"color"`
		Icon        string  `json:"icon"`
		SortOrder   *int    `json:"sortOrder"`
		ClientID    *string `json:"clientId"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var parentID, clientID *uuid.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		parsed, err := uuid.Parse(*req.ParentID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
			return
		}
		parentID = &parsed
	}

	if req.ClientID != nil && *req.ClientID != "" {
		parsed, err := uuid.Parse(*req.ClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}
		clientID = &parsed
	}

	color := "#6B7280"
	if req.Color != "" {
		color = req.Color
	}

	icon := "folder"
	if req.Icon != "" {
		icon = req.Icon
	}

	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO ticket_categories (name, description, parent_id, color, icon, sort_order, client_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, req.Name, req.Description, parentID, color, icon, sortOrder, clientID).Scan(&id)

	if err != nil {
		log.Printf("Error creating ticket category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) updateTicketCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ParentID    *string `json:"parentId"`
		Color       string  `json:"color"`
		Icon        string  `json:"icon"`
		SortOrder   *int    `json:"sortOrder"`
		IsActive    *bool   `json:"isActive"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var parentID *uuid.UUID
	if req.ParentID != nil {
		if *req.ParentID != "" {
			parsed, err := uuid.Parse(*req.ParentID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
				return
			}
			parentID = &parsed
		}
	}

	_, err = r.db.Pool().Exec(ctx, `
		UPDATE ticket_categories SET
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			parent_id = $3,
			color = COALESCE(NULLIF($4, ''), color),
			icon = COALESCE(NULLIF($5, ''), icon),
			sort_order = COALESCE($6, sort_order),
			is_active = COALESCE($7, is_active),
			updated_at = NOW()
		WHERE id = $8
	`, req.Name, req.Description, parentID, req.Color, req.Icon, req.SortOrder, req.IsActive, id)

	if err != nil {
		log.Printf("Error updating ticket category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated successfully"})
}

func (r *Router) deleteTicketCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM ticket_categories WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting ticket category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}

// Ticket Tag handlers

func (r *Router) listTicketTags(c *gin.Context) {
	ctx := context.Background()
	clientID := c.Query("clientId")

	query := `
		SELECT id, name, color, description, usage_count, client_id, created_at
		FROM ticket_tags WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if clientID != "" {
		parsed, err := uuid.Parse(clientID)
		if err == nil {
			query += " AND (client_id = $" + string(rune('0'+argNum)) + " OR client_id IS NULL)"
			args = append(args, parsed)
			argNum++
		}
	}

	query += " ORDER BY usage_count DESC, name"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		log.Printf("Error listing ticket tags: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch ticket tags"})
		return
	}
	defer rows.Close()

	tags := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var name string
		var color, description *string
		var usageCount int
		var clientID *uuid.UUID
		var createdAt time.Time

		if err := rows.Scan(&id, &name, &color, &description, &usageCount, &clientID, &createdAt); err != nil {
			log.Printf("Error scanning ticket tag row: %v", err)
			continue
		}
		tags = append(tags, map[string]interface{}{
			"id":          id,
			"name":        name,
			"color":       color,
			"description": description,
			"usageCount":  usageCount,
			"clientId":    clientID,
			"createdAt":   createdAt,
		})
	}

	c.JSON(http.StatusOK, tags)
}

func (r *Router) getTicketTag(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	ctx := context.Background()
	var tag struct {
		ID          uuid.UUID
		Name        string
		Color       *string
		Description *string
		UsageCount  int
		ClientID    *uuid.UUID
		CreatedAt   time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, color, description, usage_count, client_id, created_at
		FROM ticket_tags WHERE id = $1
	`, id).Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Description, &tag.UsageCount, &tag.ClientID, &tag.CreatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          tag.ID,
		"name":        tag.Name,
		"color":       tag.Color,
		"description": tag.Description,
		"usageCount":  tag.UsageCount,
		"clientId":    tag.ClientID,
		"createdAt":   tag.CreatedAt,
	})
}

func (r *Router) createTicketTag(c *gin.Context) {
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Color       string  `json:"color"`
		Description string  `json:"description"`
		ClientID    *string `json:"clientId"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var clientID *uuid.UUID
	if req.ClientID != nil && *req.ClientID != "" {
		parsed, err := uuid.Parse(*req.ClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}
		clientID = &parsed
	}

	color := "#6B7280"
	if req.Color != "" {
		color = req.Color
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO ticket_tags (name, color, description, client_id)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, req.Name, color, req.Description, clientID).Scan(&id)

	if err != nil {
		log.Printf("Error creating ticket tag: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) updateTicketTag(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE ticket_tags SET
			name = COALESCE(NULLIF($1, ''), name),
			color = COALESCE(NULLIF($2, ''), color),
			description = COALESCE(NULLIF($3, ''), description)
		WHERE id = $4
	`, req.Name, req.Color, req.Description, id)

	if err != nil {
		log.Printf("Error updating ticket tag: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tag updated successfully"})
}

func (r *Router) deleteTicketTag(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM ticket_tags WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting ticket tag: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tag deleted successfully"})
}

// Ticket Tag Assignment handlers

func (r *Router) getTicketTags(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT t.id, t.name, t.color, t.description, ta.assigned_at, ta.assigned_by
		FROM ticket_tags t
		JOIN ticket_tag_assignments ta ON t.id = ta.tag_id
		WHERE ta.ticket_id = $1
		ORDER BY ta.assigned_at DESC
	`, ticketID)
	if err != nil {
		log.Printf("Error getting ticket tags: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch ticket tags"})
		return
	}
	defer rows.Close()

	tags := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var name string
		var color, description, assignedBy *string
		var assignedAt time.Time

		if err := rows.Scan(&id, &name, &color, &description, &assignedAt, &assignedBy); err != nil {
			continue
		}
		tags = append(tags, map[string]interface{}{
			"id":          id,
			"name":        name,
			"color":       color,
			"description": description,
			"assignedAt":  assignedAt,
			"assignedBy":  assignedBy,
		})
	}

	c.JSON(http.StatusOK, tags)
}

func (r *Router) assignTicketTag(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	var req struct {
		TagID      string `json:"tagId" binding:"required"`
		AssignedBy string `json:"assignedBy"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tagID, err := uuid.Parse(req.TagID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO ticket_tag_assignments (ticket_id, tag_id, assigned_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (ticket_id, tag_id) DO NOTHING
	`, ticketID, tagID, req.AssignedBy)

	if err != nil {
		log.Printf("Error assigning ticket tag: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign tag"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tag assigned successfully"})
}

func (r *Router) removeTicketTag(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	tagID, err := uuid.Parse(c.Param("tagId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		DELETE FROM ticket_tag_assignments WHERE ticket_id = $1 AND tag_id = $2
	`, ticketID, tagID)

	if err != nil {
		log.Printf("Error removing ticket tag: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove tag"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tag removed successfully"})
}

// Ticket Link handlers

func (r *Router) getTicketLinks(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
		return
	}

	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT l.id, l.source_ticket_id, l.target_ticket_id, l.link_type, l.created_by, l.created_at,
			t.subject as target_subject, t.status as target_status
		FROM ticket_links l
		JOIN tickets t ON (l.target_ticket_id = t.id OR l.source_ticket_id = t.id) AND t.id != $1
		WHERE l.source_ticket_id = $1 OR l.target_ticket_id = $1
		ORDER BY l.created_at DESC
	`, ticketID)
	if err != nil {
		log.Printf("Error getting ticket links: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch ticket links"})
		return
	}
	defer rows.Close()

	links := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, sourceID, targetID uuid.UUID
		var linkType string
		var createdBy, targetSubject, targetStatus *string
		var createdAt time.Time

		if err := rows.Scan(&id, &sourceID, &targetID, &linkType, &createdBy, &createdAt, &targetSubject, &targetStatus); err != nil {
			continue
		}
		links = append(links, map[string]interface{}{
			"id":             id,
			"sourceTicketId": sourceID,
			"targetTicketId": targetID,
			"linkType":       linkType,
			"createdBy":      createdBy,
			"createdAt":      createdAt,
			"targetSubject":  targetSubject,
			"targetStatus":   targetStatus,
		})
	}

	c.JSON(http.StatusOK, links)
}

func (r *Router) createTicketLink(c *gin.Context) {
	sourceTicketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source ticket ID"})
		return
	}

	var req struct {
		TargetTicketID string `json:"targetTicketId" binding:"required"`
		LinkType       string `json:"linkType" binding:"required"`
		CreatedBy      string `json:"createdBy"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	targetTicketID, err := uuid.Parse(req.TargetTicketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target ticket ID"})
		return
	}

	if sourceTicketID == targetTicketID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot link ticket to itself"})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO ticket_links (source_ticket_id, target_ticket_id, link_type, created_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, sourceTicketID, targetTicketID, req.LinkType, req.CreatedBy).Scan(&id)

	if err != nil {
		log.Printf("Error creating ticket link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create link"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Link created successfully"})
}

func (r *Router) deleteTicketLink(c *gin.Context) {
	linkID, err := uuid.Parse(c.Param("linkId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM ticket_links WHERE id = $1", linkID)
	if err != nil {
		log.Printf("Error deleting ticket link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete link"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Link deleted successfully"})
}

// Custom Field Definition handlers

func (r *Router) listCustomFieldDefinitions(c *gin.Context) {
	ctx := context.Background()
	clientID := c.Query("clientId")
	ticketType := c.Query("type")

	query := `
		SELECT id, name, field_key, field_type, description, placeholder, options, default_value,
			is_required, applies_to_type, sort_order, is_active, client_id, created_at, updated_at
		FROM custom_field_definitions WHERE is_active = TRUE
	`
	args := []interface{}{}
	argNum := 1

	if clientID != "" {
		parsed, err := uuid.Parse(clientID)
		if err == nil {
			query += " AND (client_id = $" + string(rune('0'+argNum)) + " OR client_id IS NULL)"
			args = append(args, parsed)
			argNum++
		}
	}

	if ticketType != "" {
		query += " AND (applies_to_type = $" + string(rune('0'+argNum)) + " OR applies_to_type IS NULL)"
		args = append(args, ticketType)
		argNum++
	}

	query += " ORDER BY sort_order, name"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		log.Printf("Error listing custom field definitions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch custom fields"})
		return
	}
	defer rows.Close()

	fields := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var name, fieldKey, fieldType string
		var description, placeholder, defaultValue, appliesToType *string
		var optionsJSON []byte
		var isRequired, isActive bool
		var sortOrder int
		var clientID *uuid.UUID
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&id, &name, &fieldKey, &fieldType, &description, &placeholder, &optionsJSON,
			&defaultValue, &isRequired, &appliesToType, &sortOrder, &isActive, &clientID, &createdAt, &updatedAt); err != nil {
			log.Printf("Error scanning custom field row: %v", err)
			continue
		}

		var options interface{}
		if len(optionsJSON) > 0 {
			if err := json.Unmarshal(optionsJSON, &options); err != nil {
				options = []interface{}{}
			}
		}

		fields = append(fields, map[string]interface{}{
			"id":            id,
			"name":          name,
			"fieldKey":      fieldKey,
			"fieldType":     fieldType,
			"description":   description,
			"placeholder":   placeholder,
			"options":       options,
			"defaultValue":  defaultValue,
			"isRequired":    isRequired,
			"appliesToType": appliesToType,
			"sortOrder":     sortOrder,
			"isActive":      isActive,
			"clientId":      clientID,
			"createdAt":     createdAt,
			"updatedAt":     updatedAt,
		})
	}

	c.JSON(http.StatusOK, fields)
}

func (r *Router) createCustomFieldDefinition(c *gin.Context) {
	var req struct {
		Name          string      `json:"name" binding:"required"`
		FieldKey      string      `json:"fieldKey" binding:"required"`
		FieldType     string      `json:"fieldType" binding:"required"`
		Description   string      `json:"description"`
		Placeholder   string      `json:"placeholder"`
		Options       interface{} `json:"options"`
		DefaultValue  string      `json:"defaultValue"`
		IsRequired    *bool       `json:"isRequired"`
		AppliesToType string      `json:"appliesToType"`
		SortOrder     *int        `json:"sortOrder"`
		ClientID      *string     `json:"clientId"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var clientID *uuid.UUID
	if req.ClientID != nil && *req.ClientID != "" {
		parsed, err := uuid.Parse(*req.ClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}
		clientID = &parsed
	}

	isRequired := false
	if req.IsRequired != nil {
		isRequired = *req.IsRequired
	}

	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	optionsJSON, _ := json.Marshal(req.Options)
	if req.Options == nil {
		optionsJSON = []byte("[]")
	}

	var appliesToType *string
	if req.AppliesToType != "" {
		appliesToType = &req.AppliesToType
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO custom_field_definitions (name, field_key, field_type, description, placeholder, options,
			default_value, is_required, applies_to_type, sort_order, client_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id
	`, req.Name, req.FieldKey, req.FieldType, req.Description, req.Placeholder, optionsJSON,
		req.DefaultValue, isRequired, appliesToType, sortOrder, clientID).Scan(&id)

	if err != nil {
		log.Printf("Error creating custom field definition: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create custom field"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "fieldKey": req.FieldKey})
}

func (r *Router) updateCustomFieldDefinition(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field ID"})
		return
	}

	var req struct {
		Name          string      `json:"name"`
		Description   string      `json:"description"`
		Placeholder   string      `json:"placeholder"`
		Options       interface{} `json:"options"`
		DefaultValue  string      `json:"defaultValue"`
		IsRequired    *bool       `json:"isRequired"`
		AppliesToType *string     `json:"appliesToType"`
		SortOrder     *int        `json:"sortOrder"`
		IsActive      *bool       `json:"isActive"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var optionsJSON []byte
	if req.Options != nil {
		optionsJSON, _ = json.Marshal(req.Options)
	}

	_, err = r.db.Pool().Exec(ctx, `
		UPDATE custom_field_definitions SET
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			placeholder = COALESCE(NULLIF($3, ''), placeholder),
			options = COALESCE($4::jsonb, options),
			default_value = COALESCE(NULLIF($5, ''), default_value),
			is_required = COALESCE($6, is_required),
			applies_to_type = $7,
			sort_order = COALESCE($8, sort_order),
			is_active = COALESCE($9, is_active),
			updated_at = NOW()
		WHERE id = $10
	`, req.Name, req.Description, req.Placeholder, optionsJSON, req.DefaultValue,
		req.IsRequired, req.AppliesToType, req.SortOrder, req.IsActive, id)

	if err != nil {
		log.Printf("Error updating custom field definition: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update custom field"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Custom field updated successfully"})
}

func (r *Router) deleteCustomFieldDefinition(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM custom_field_definitions WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting custom field definition: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete custom field"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Custom field not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Custom field deleted successfully"})
}

// Handler wrappers for ticket categories
func listTicketCategoriesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listTicketCategories
}

func getTicketCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketCategory
}

func createTicketCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createTicketCategory
}

func updateTicketCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateTicketCategory
}

func deleteTicketCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicketCategory
}

// Handler wrappers for ticket tags
func listTicketTagsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listTicketTags
}

func getTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketTag
}

func createTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createTicketTag
}

func updateTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateTicketTag
}

func deleteTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicketTag
}

func getTicketTagsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketTags
}

func assignTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.assignTicketTag
}

func removeTicketTagHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.removeTicketTag
}

// Handler wrappers for ticket links
func getTicketLinksHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getTicketLinks
}

func createTicketLinkHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createTicketLink
}

func deleteTicketLinkHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteTicketLink
}

// Handler wrappers for custom fields
func listCustomFieldDefinitionsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listCustomFieldDefinitions
}

func createCustomFieldDefinitionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createCustomFieldDefinition
}

func updateCustomFieldDefinitionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateCustomFieldDefinition
}

func deleteCustomFieldDefinitionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteCustomFieldDefinition
}

func getCustomFieldDefinitionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getCustomFieldDefinition
}

// Get a single custom field definition
func (r *Router) getCustomFieldDefinition(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid custom field ID"})
		return
	}

	ctx := context.Background()
	var field struct {
		ID           uuid.UUID
		Name         string
		FieldType    string
		Description  *string
		Options      json.RawMessage
		Required     bool
		DefaultValue *string
		IsActive     bool
		SortOrder    int
		CreatedAt    time.Time
		UpdatedAt    time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, field_type, description, options, required,
			default_value, is_active, sort_order, created_at, updated_at
		FROM custom_field_definitions WHERE id = $1
	`, id).Scan(&field.ID, &field.Name, &field.FieldType, &field.Description,
		&field.Options, &field.Required, &field.DefaultValue, &field.IsActive,
		&field.SortOrder, &field.CreatedAt, &field.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Custom field definition not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           field.ID,
		"name":         field.Name,
		"fieldType":    field.FieldType,
		"description":  field.Description,
		"options":      field.Options,
		"required":     field.Required,
		"defaultValue": field.DefaultValue,
		"isActive":     field.IsActive,
		"sortOrder":    field.SortOrder,
		"createdAt":    field.CreatedAt,
		"updatedAt":    field.UpdatedAt,
	})
}
