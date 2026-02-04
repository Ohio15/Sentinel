package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/middleware"
)

// ClientTenant represents an Azure AD tenant mapped to a Sentinel client
type ClientTenant struct {
	ID         string     `json:"id"`
	ClientID   *string    `json:"clientId"`
	ClientName *string    `json:"clientName"`
	TenantID   string     `json:"tenantId"`
	TenantName *string    `json:"tenantName"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// CreateClientTenantRequest is the request body for creating a client tenant mapping
type CreateClientTenantRequest struct {
	ClientID   string `json:"clientId"`
	TenantID   string `json:"tenantId" binding:"required"`
	TenantName string `json:"tenantName"`
}

// RegisterPortalRoutes registers the portal-related API routes
func RegisterPortalRoutes(api *gin.RouterGroup, protected *gin.RouterGroup, services *Services) {
	// Portal routes (admin only)
	portal := protected.Group("/portal")
	portal.Use(middleware.RequireRole("admin"))
	{
		portal.GET("/client-tenants", listClientTenantsHandler(services))
		portal.POST("/client-tenants", createClientTenantHandler(services))
		portal.DELETE("/client-tenants/:id", deleteClientTenantHandler(services))
	}

	log.Printf("[PORTAL] Registered portal routes")
}

// listClientTenantsHandler returns all client tenant mappings
func listClientTenantsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		query := `
			SELECT
				ct.id,
				ct.client_id,
				cl.name as client_name,
				ct.tenant_id,
				ct.tenant_name,
				ct.enabled,
				ct.created_at,
				ct.updated_at
			FROM client_tenants ct
			LEFT JOIN clients cl ON ct.client_id = cl.id
			ORDER BY ct.created_at DESC
		`

		rows, err := services.DB.Pool().Query(ctx, query)
		if err != nil {
			log.Printf("[PORTAL] Error listing client tenants: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list client tenants"})
			return
		}
		defer rows.Close()

		tenants := []ClientTenant{}
		for rows.Next() {
			var t ClientTenant
			err := rows.Scan(
				&t.ID,
				&t.ClientID,
				&t.ClientName,
				&t.TenantID,
				&t.TenantName,
				&t.Enabled,
				&t.CreatedAt,
				&t.UpdatedAt,
			)
			if err != nil {
				log.Printf("[PORTAL] Error scanning client tenant: %v", err)
				continue
			}
			tenants = append(tenants, t)
		}

		c.JSON(http.StatusOK, tenants)
	}
}

// createClientTenantHandler creates a new client tenant mapping
func createClientTenantHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateClientTenantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// Validate tenant ID format (should be a GUID)
		if _, err := uuid.Parse(req.TenantID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tenant ID format. Must be a valid GUID."})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		// Check if tenant already exists
		var exists bool
		err := services.DB.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM client_tenants WHERE tenant_id = $1)",
			req.TenantID,
		).Scan(&exists)
		if err != nil {
			log.Printf("[PORTAL] Error checking existing tenant: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if exists {
			c.JSON(http.StatusConflict, gin.H{"error": "Tenant mapping already exists"})
			return
		}

		var clientID *uuid.UUID
		if req.ClientID != "" {
			parsedID, err := uuid.Parse(req.ClientID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID format"})
				return
			}
			clientID = &parsedID
		} else if req.TenantName != "" {
			// Auto-create a client if no client ID provided but tenant name given
			newClientID := uuid.New()
			_, err := services.DB.Pool().Exec(ctx,
				"INSERT INTO clients (id, name, organization_id) VALUES ($1, $2, 1)",
				newClientID, req.TenantName,
			)
			if err != nil {
				log.Printf("[PORTAL] Error creating client: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create client"})
				return
			}
			clientID = &newClientID
		}

		// Insert the tenant mapping
		var id uuid.UUID
		err = services.DB.Pool().QueryRow(ctx,
			`INSERT INTO client_tenants (client_id, tenant_id, tenant_name, enabled)
			 VALUES ($1, $2, $3, true)
			 RETURNING id`,
			clientID, req.TenantID, req.TenantName,
		).Scan(&id)

		if err != nil {
			log.Printf("[PORTAL] Error creating client tenant: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tenant mapping"})
			return
		}

		// Fetch the created record
		var tenant ClientTenant
		err = services.DB.Pool().QueryRow(ctx,
			`SELECT
				ct.id,
				ct.client_id,
				cl.name as client_name,
				ct.tenant_id,
				ct.tenant_name,
				ct.enabled,
				ct.created_at,
				ct.updated_at
			FROM client_tenants ct
			LEFT JOIN clients cl ON ct.client_id = cl.id
			WHERE ct.id = $1`,
			id,
		).Scan(
			&tenant.ID,
			&tenant.ClientID,
			&tenant.ClientName,
			&tenant.TenantID,
			&tenant.TenantName,
			&tenant.Enabled,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		)
		if err != nil {
			log.Printf("[PORTAL] Error fetching created tenant: %v", err)
			c.JSON(http.StatusCreated, gin.H{"id": id.String()})
			return
		}

		c.JSON(http.StatusCreated, tenant)
	}
}

// deleteClientTenantHandler deletes a client tenant mapping
func deleteClientTenantHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		result, err := services.DB.Pool().Exec(ctx,
			"DELETE FROM client_tenants WHERE id = $1",
			id,
		)
		if err != nil {
			log.Printf("[PORTAL] Error deleting client tenant: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tenant mapping"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tenant mapping not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Tenant mapping deleted"})
	}
}
