package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Sentinel RMM API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.10.3/swagger-ui.css" />
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.10.3/swagger-ui-bundle.js"></script>
    <script>
        window.onload = () => {
            window.ui = SwaggerUIBundle({
                url: "/api/openapi.yaml",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIBundle.SwaggerUIStandalonePreset
                ],
                layout: "StandaloneLayout"
            });
        };
    </script>
</body>
</html>`

// serveOpenAPISpec serves the OpenAPI specification file
func serveOpenAPISpec() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try to find the spec file in common locations
		paths := []string{
			"api/openapi.yaml",
			"../api/openapi.yaml",
			filepath.Join(os.Getenv("HOME"), "Sentinel/server/api/openapi.yaml"),
		}

		var spec []byte
		var err error
		for _, path := range paths {
			spec, err = os.ReadFile(path)
			if err == nil {
				break
			}
		}

		if err != nil {
			// Return a dynamically generated minimal spec
			c.Header("Content-Type", "application/yaml")
			c.String(http.StatusOK, generateMinimalSpec())
			return
		}

		c.Header("Content-Type", "application/yaml")
		c.Data(http.StatusOK, "application/yaml", spec)
	}
}

// serveSwaggerUI serves the Swagger UI interface
func serveSwaggerUI() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, swaggerUIHTML)
	}
}

// apiInfoHandler returns basic API info
func apiInfoHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":        "Sentinel RMM API",
			"version":     "1.0.0",
			"description": "Remote Monitoring and Management API",
			"docs":        "/api/docs",
			"spec":        "/api/openapi.yaml",
			"endpoints": map[string]string{
				"auth":     "/api/auth",
				"devices":  "/api/devices",
				"alerts":   "/api/alerts",
				"scripts":  "/api/scripts",
				"webhooks": "/api/webhooks",
				"reports":  "/api/reports",
				"export":   "/api/export",
				"search":   "/api/search",
			},
		})
	}
}

func generateMinimalSpec() string {
	return `openapi: 3.0.3
info:
  title: Sentinel RMM API
  version: 1.0.0
  description: Remote Monitoring and Management API
servers:
  - url: /api
tags:
  - name: Authentication
  - name: Devices
  - name: Alerts
  - name: Scripts
  - name: Reports
paths:
  /auth/login:
    post:
      tags: [Authentication]
      summary: User login
      responses:
        '200':
          description: Login successful
  /devices:
    get:
      tags: [Devices]
      summary: List devices
      security:
        - bearerAuth: []
      responses:
        '200':
          description: Device list
  /alerts:
    get:
      tags: [Alerts]
      summary: List alerts
      security:
        - bearerAuth: []
      responses:
        '200':
          description: Alert list
  /scripts:
    get:
      tags: [Scripts]
      summary: List scripts
      security:
        - bearerAuth: []
      responses:
        '200':
          description: Script list
  /reports/security-posture:
    get:
      tags: [Reports]
      summary: Generate security posture report
      security:
        - bearerAuth: []
      responses:
        '200':
          description: PDF report
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT`
}
