package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

const (
	// uninstallTokenTTL is how long an uninstall token remains valid (5 minutes)
	uninstallTokenTTL = 5 * time.Minute

	// uninstallTokenPrefix is the Redis key prefix for uninstall tokens
	uninstallTokenPrefix = "uninstall_token:"
)

// uninstallTokenData is stored in Redis alongside the token
type uninstallTokenData struct {
	DeviceID  string    `json:"deviceId"`
	AgentID   string    `json:"agentId"`
	CreatedAt time.Time `json:"createdAt"`
}

// requestUninstallToken generates a short-lived, single-use uninstall token.
// Called by the agent (authenticated via X-Enrollment-Token) when it needs
// authorization to perform a self-uninstall.
func (r *Router) requestUninstallToken(c *gin.Context) {
	var req struct {
		DeviceID string `json:"deviceId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
		return
	}

	ctx := context.Background()

	// Verify the device exists in the database
	var agentID string
	err := r.db.Pool().QueryRow(ctx,
		"SELECT agent_id FROM devices WHERE (id::text = $1 OR agent_id = $1) AND organization_id = $2",
		req.DeviceID, constants.CurrentOrganizationID,
	).Scan(&agentID)
	if err != nil {
		log.Printf("[UninstallToken] Device not found for ID %s: %v", req.DeviceID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Generate a single-use token
	token := uuid.New().String()

	// Store token in Redis with TTL
	tokenData := uninstallTokenData{
		DeviceID:  req.DeviceID,
		AgentID:   agentID,
		CreatedAt: time.Now().UTC(),
	}
	dataBytes, err := json.Marshal(tokenData)
	if err != nil {
		log.Printf("[UninstallToken] Failed to marshal token data: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	redisKey := uninstallTokenPrefix + token
	if err := r.cache.Set(ctx, redisKey, string(dataBytes), uninstallTokenTTL); err != nil {
		log.Printf("[UninstallToken] Failed to store token in Redis: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate uninstall token"})
		return
	}

	log.Printf("[UninstallToken] Generated token %s... for device %s (agent %s), expires in %v",
		token[:8], req.DeviceID, agentID, uninstallTokenTTL)

	c.JSON(http.StatusOK, gin.H{
		"token":     token,
		"expiresIn": int(uninstallTokenTTL.Seconds()),
	})
}

// validateUninstallToken validates and consumes an uninstall token.
// Called by the agent before performing the actual uninstall to confirm
// the token is still valid. The token is consumed (deleted) on validation
// to enforce single-use semantics.
func (r *Router) validateUninstallToken(c *gin.Context) {
	var req struct {
		DeviceID string `json:"deviceId" binding:"required"`
		Token    string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and token are required"})
		return
	}

	ctx := context.Background()

	// Look up the token in Redis
	redisKey := uninstallTokenPrefix + req.Token
	dataStr, err := r.cache.Get(ctx, redisKey)
	if err != nil {
		log.Printf("[UninstallToken] Token not found or expired: %s...", req.Token[:min(8, len(req.Token))])
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired uninstall token"})
		return
	}

	// Parse the token data
	var tokenData uninstallTokenData
	if err := json.Unmarshal([]byte(dataStr), &tokenData); err != nil {
		log.Printf("[UninstallToken] Failed to parse token data: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Verify the token belongs to the requesting device
	if tokenData.DeviceID != req.DeviceID && tokenData.AgentID != req.DeviceID {
		log.Printf("[UninstallToken] Token device mismatch: token for %s/%s, request from %s",
			tokenData.DeviceID, tokenData.AgentID, req.DeviceID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Token does not match device"})
		return
	}

	// Consume the token (delete from Redis) — single-use enforcement
	if err := r.cache.Delete(ctx, redisKey); err != nil {
		log.Printf("[UninstallToken] Warning: failed to delete consumed token: %v", err)
		// Don't fail the validation — token was valid, deletion is best-effort
	}

	// Mark device as authorized for uninstall in the database
	if _, err := r.db.Pool().Exec(ctx,
		"UPDATE devices SET status = 'uninstalling', updated_at = NOW() WHERE (id::text = $1 OR agent_id = $1) AND organization_id = $2",
		req.DeviceID, constants.CurrentOrganizationID,
	); err != nil {
		log.Printf("[UninstallToken] Warning: failed to update device status: %v", err)
	}

	log.Printf("[UninstallToken] Token validated and consumed for device %s (agent %s)",
		tokenData.DeviceID, tokenData.AgentID)

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"message": fmt.Sprintf("Uninstall authorized for device %s", tokenData.DeviceID),
	})
}

// requestUninstallTokenHandler wraps requestUninstallToken for the services-based router
func requestUninstallTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.requestUninstallToken
}

// validateUninstallTokenHandler wraps validateUninstallToken for the services-based router
func validateUninstallTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.validateUninstallToken
}
