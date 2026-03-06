package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// sanitizedError logs the real error server-side and returns a generic message to the client.
// This prevents internal error details (DB queries, file paths, stack traces) from leaking
// to clients, which would be an information disclosure vulnerability.
func sanitizedError(c *gin.Context, statusCode int, publicMsg string, err error) {
	if err != nil {
		log.Printf("[ERROR] %s: %v (ip=%s path=%s)", publicMsg, err, c.ClientIP(), c.Request.URL.Path)
	}
	c.JSON(statusCode, gin.H{"error": publicMsg})
}

// internalError is a shorthand for 500 errors
func internalError(c *gin.Context, publicMsg string, err error) {
	sanitizedError(c, http.StatusInternalServerError, publicMsg, err)
}
