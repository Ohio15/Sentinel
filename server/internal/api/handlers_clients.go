package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Client handlers

func (r *Router) listClients(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, description, color, logo_url, logo_width, logo_height, created_at, updated_at
		FROM clients ORDER BY name
	`)
	if err != nil {
		log.Printf("Error listing clients: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch clients"})
		return
	}
	defer rows.Close()

	clients := make([]map[string]interface{}, 0)
	for rows.Next() {
		var client struct {
			ID          uuid.UUID
			Name        string
			Description *string
			Color       *string
			LogoURL     *string
			LogoWidth   *int
			LogoHeight  *int
			CreatedAt   time.Time
			UpdatedAt   time.Time
		}
		if err := rows.Scan(&client.ID, &client.Name, &client.Description, &client.Color,
			&client.LogoURL, &client.LogoWidth, &client.LogoHeight, &client.CreatedAt, &client.UpdatedAt); err != nil {
			log.Printf("Error scanning client row: %v", err)
			continue
		}
		clients = append(clients, map[string]interface{}{
			"id":          client.ID,
			"name":        client.Name,
			"description": client.Description,
			"color":       client.Color,
			"logoUrl":     client.LogoURL,
			"logoWidth":   client.LogoWidth,
			"logoHeight":  client.LogoHeight,
			"createdAt":   client.CreatedAt,
			"updatedAt":   client.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, clients)
}

func (r *Router) getClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
		return
	}

	ctx := context.Background()
	var client struct {
		ID          uuid.UUID
		Name        string
		Description *string
		Color       *string
		LogoURL     *string
		LogoWidth   *int
		LogoHeight  *int
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, color, logo_url, logo_width, logo_height, created_at, updated_at
		FROM clients WHERE id = $1
	`, id).Scan(&client.ID, &client.Name, &client.Description, &client.Color,
		&client.LogoURL, &client.LogoWidth, &client.LogoHeight, &client.CreatedAt, &client.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          client.ID,
		"name":        client.Name,
		"description": client.Description,
		"color":       client.Color,
		"logoUrl":     client.LogoURL,
		"logoWidth":   client.LogoWidth,
		"logoHeight":  client.LogoHeight,
		"createdAt":   client.CreatedAt,
		"updatedAt":   client.UpdatedAt,
	})
}

func (r *Router) createClient(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Color       string `json:"color"`
		LogoURL     string `json:"logoUrl"`
		LogoWidth   *int   `json:"logoWidth"`
		LogoHeight  *int   `json:"logoHeight"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO clients (name, description, color, logo_url, logo_width, logo_height)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, req.Name, req.Description, req.Color, req.LogoURL, req.LogoWidth, req.LogoHeight).Scan(&id)

	if err != nil {
		log.Printf("Error creating client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create client"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":   id,
		"name": req.Name,
	})
}

func (r *Router) updateClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
		LogoURL     string `json:"logoUrl"`
		LogoWidth   *int   `json:"logoWidth"`
		LogoHeight  *int   `json:"logoHeight"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE clients SET
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			color = COALESCE(NULLIF($3, ''), color),
			logo_url = COALESCE(NULLIF($4, ''), logo_url),
			logo_width = COALESCE($5, logo_width),
			logo_height = COALESCE($6, logo_height),
			updated_at = NOW()
		WHERE id = $7
	`, req.Name, req.Description, req.Color, req.LogoURL, req.LogoWidth, req.LogoHeight, id)

	if err != nil {
		log.Printf("Error updating client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update client"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Client updated successfully"})
}

func (r *Router) deleteClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM clients WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete client"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Client deleted successfully"})
}

func (r *Router) assignDeviceToClient(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req struct {
		ClientID *string `json:"clientId"`
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

	_, err = r.db.Pool().Exec(ctx, "UPDATE devices SET client_id = $1, updated_at = NOW() WHERE id = $2", clientID, deviceID)
	if err != nil {
		log.Printf("Error assigning device to client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign device to client"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device assigned to client successfully"})
}

func (r *Router) bulkAssignDevicesToClient(c *gin.Context) {
	var req struct {
		DeviceIDs []string `json:"deviceIds" binding:"required"`
		ClientID  *string  `json:"clientId"`
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

	deviceIDs := make([]uuid.UUID, 0, len(req.DeviceIDs))
	for _, idStr := range req.DeviceIDs {
		parsed, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		deviceIDs = append(deviceIDs, parsed)
	}

	if len(deviceIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid device IDs provided"})
		return
	}

	_, err := r.db.Pool().Exec(ctx, "UPDATE devices SET client_id = $1, updated_at = NOW() WHERE id = ANY($2)", clientID, deviceIDs)
	if err != nil {
		log.Printf("Error bulk assigning devices to client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign devices to client"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Devices assigned to client successfully", "count": len(deviceIDs)})
}

// Handler wrappers for clients
func listClientsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listClients
}

func getClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getClient
}

func createClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createClient
}

func updateClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateClient
}

func deleteClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteClient
}

func assignDeviceToClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.assignDeviceToClient
}

func bulkAssignDevicesToClientHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.bulkAssignDevicesToClient
}
