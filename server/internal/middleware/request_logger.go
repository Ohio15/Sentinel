package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// StructuredLogger returns a Gin middleware that logs each request
// using the slog structured logger. It categorises by HTTP status:
//
//	5xx → Error, 4xx → Warn, everything else → Info.
func StructuredLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		attrs := []any{
			"status", status,
			"method", c.Request.Method,
			"path", path,
			"latency_ms", latency.Milliseconds(),
			"ip", c.ClientIP(),
		}

		if status >= 500 {
			slog.Error("request failed", attrs...)
		} else if status >= 400 {
			slog.Warn("request error", attrs...)
		} else {
			slog.Info("request", attrs...)
		}
	}
}
