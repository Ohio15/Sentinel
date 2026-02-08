package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/reports"
)

// reportsGenerator is the PDF generator instance
var reportsGenerator *reports.PDFGenerator

// initReportsGenerator initializes the reports generator (called from router setup)
func initReportsGenerator(services *Services) {
	reportsGenerator = reports.NewPDFGenerator(services.DB.Pool())
}

func generateSecurityReportHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reportsGenerator == nil {
			initReportsGenerator(services)
		}

		ctx := c.Request.Context()
		pdf, err := reportsGenerator.GenerateSecurityPostureReport(ctx, reports.ReportOptions{
			Type: reports.ReportTypeSecurityPosture,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate report: " + err.Error()})
			return
		}

		filename := fmt.Sprintf("security-posture-report-%s.pdf", time.Now().Format("2006-01-02"))
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Length", fmt.Sprintf("%d", len(pdf)))
		c.Data(http.StatusOK, "application/pdf", pdf)
	}
}

func generateDeviceReportHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reportsGenerator == nil {
			initReportsGenerator(services)
		}

		deviceID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		ctx := c.Request.Context()
		pdf, err := reportsGenerator.GenerateDeviceSummaryReport(ctx, deviceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate report: " + err.Error()})
			return
		}

		filename := fmt.Sprintf("device-report-%s-%s.pdf", deviceID.String()[:8], time.Now().Format("2006-01-02"))
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Length", fmt.Sprintf("%d", len(pdf)))
		c.Data(http.StatusOK, "application/pdf", pdf)
	}
}

func generateAlertHistoryReportHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reportsGenerator == nil {
			initReportsGenerator(services)
		}

		// Parse date range from query params
		startStr := c.Query("start")
		endStr := c.Query("end")

		var startDate, endDate time.Time
		var err error

		if startStr != "" {
			startDate, err = time.Parse("2006-01-02", startStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start date format. Use YYYY-MM-DD"})
				return
			}
		} else {
			// Default to last 30 days
			startDate = time.Now().AddDate(0, 0, -30)
		}

		if endStr != "" {
			endDate, err = time.Parse("2006-01-02", endStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end date format. Use YYYY-MM-DD"})
				return
			}
			// Set to end of day
			endDate = endDate.Add(24*time.Hour - time.Second)
		} else {
			endDate = time.Now()
		}

		ctx := c.Request.Context()
		pdf, err := reportsGenerator.GenerateAlertHistoryReport(ctx, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate report: " + err.Error()})
			return
		}

		filename := fmt.Sprintf("alert-history-%s-to-%s.pdf",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"))
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Length", fmt.Sprintf("%d", len(pdf)))
		c.Data(http.StatusOK, "application/pdf", pdf)
	}
}

func generateExecutiveReportHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reportsGenerator == nil {
			initReportsGenerator(services)
		}

		ctx := c.Request.Context()
		pdf, err := reportsGenerator.GenerateExecutiveReport(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate report: " + err.Error()})
			return
		}

		filename := fmt.Sprintf("executive-summary-%s.pdf", time.Now().Format("2006-01-02"))
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Length", fmt.Sprintf("%d", len(pdf)))
		c.Data(http.StatusOK, "application/pdf", pdf)
	}
}

// listReportTypesHandler returns available report types
func listReportTypesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		reportTypes := []map[string]interface{}{
			{
				"id":          "security_posture",
				"name":        "Security Posture Report",
				"description": "Overview of fleet security status, alerts, and recommendations",
				"endpoint":    "/api/reports/security-posture",
			},
			{
				"id":          "device_summary",
				"name":        "Device Summary Report",
				"description": "Detailed report for a specific device including metrics and alerts",
				"endpoint":    "/api/devices/:id/report",
				"parameters":  []string{"deviceId"},
			},
			{
				"id":          "alert_history",
				"name":        "Alert History Report",
				"description": "Historical view of all alerts within a date range",
				"endpoint":    "/api/reports/alert-history",
				"parameters":  []string{"start", "end"},
			},
			{
				"id":          "executive",
				"name":        "Executive Summary",
				"description": "High-level KPIs and recommendations for leadership",
				"endpoint":    "/api/reports/executive",
			},
		}

		c.JSON(http.StatusOK, reportTypes)
	}
}
