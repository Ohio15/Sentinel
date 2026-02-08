package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/constants"
	"github.com/xuri/excelize/v2"
)

// ExportFormat represents the export file format
type ExportFormat string

const (
	FormatCSV   ExportFormat = "csv"
	FormatExcel ExportFormat = "xlsx"
)

// exportDevicesHandler exports device data
func exportDevicesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT d.id, d.hostname, COALESCE(d.os_type, ''),
			       COALESCE(d.os_version, ''), d.status, d.last_seen,
			       COALESCE(d.ip_address, ''), COALESCE(d.mac_address, ''),
			       d.is_disabled, d.created_at
			FROM devices d
			WHERE d.organization_id = $1
			ORDER BY d.hostname
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Export] Error querying devices: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export devices"})
			return
		}
		defer rows.Close()

		headers := []string{"ID", "Hostname", "OS Type", "OS Version", "Status", "Last Seen", "IP Address", "MAC Address", "Disabled", "Created At"}
		var data [][]string

		for rows.Next() {
			var id, hostname, osType, osVersion, status, ipAddr, macAddr string
			var lastSeen, createdAt time.Time
			var isDisabled bool

			if err := rows.Scan(&id, &hostname, &osType, &osVersion, &status, &lastSeen, &ipAddr, &macAddr, &isDisabled, &createdAt); err != nil {
				continue
			}

			data = append(data, []string{
				id, hostname, osType, osVersion, status,
				lastSeen.Format(time.RFC3339),
				ipAddr, macAddr,
				strconv.FormatBool(isDisabled),
				createdAt.Format(time.RFC3339),
			})
		}

		exportData(c, "devices", format, headers, data)
	}
}

// exportAlertsHandler exports alert data
func exportAlertsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		ctx := context.Background()

		// Optional date filters
		startDate := c.Query("start")
		endDate := c.Query("end")

		query := `
			SELECT a.id, COALESCE(d.hostname, 'Unknown'), a.severity, a.title,
			       COALESCE(a.message, ''), a.status, a.created_at,
			       a.acknowledged_at, a.resolved_at
			FROM alerts a
			JOIN devices d ON a.device_id = d.id
			WHERE d.organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}

		if startDate != "" {
			if t, err := time.Parse("2006-01-02", startDate); err == nil {
				query += " AND a.created_at >= $2"
				args = append(args, t)
			}
		}
		if endDate != "" {
			if t, err := time.Parse("2006-01-02", endDate); err == nil {
				t = t.Add(24*time.Hour - time.Second)
				query += fmt.Sprintf(" AND a.created_at <= $%d", len(args)+1)
				args = append(args, t)
			}
		}

		query += " ORDER BY a.created_at DESC"

		rows, err := services.DB.Pool().Query(ctx, query, args...)
		if err != nil {
			log.Printf("[Export] Error querying alerts: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export alerts"})
			return
		}
		defer rows.Close()

		headers := []string{"ID", "Hostname", "Severity", "Title", "Message", "Status", "Created At", "Acknowledged At", "Resolved At"}
		var data [][]string

		for rows.Next() {
			var id, hostname, severity, title, message, status string
			var createdAt time.Time
			var acknowledgedAt, resolvedAt *time.Time

			if err := rows.Scan(&id, &hostname, &severity, &title, &message, &status, &createdAt, &acknowledgedAt, &resolvedAt); err != nil {
				continue
			}

			ackStr, resolveStr := "", ""
			if acknowledgedAt != nil {
				ackStr = acknowledgedAt.Format(time.RFC3339)
			}
			if resolvedAt != nil {
				resolveStr = resolvedAt.Format(time.RFC3339)
			}

			data = append(data, []string{
				id, hostname, severity, title, message, status,
				createdAt.Format(time.RFC3339), ackStr, resolveStr,
			})
		}

		exportData(c, "alerts", format, headers, data)
	}
}

// exportUpdatesHandler exports Windows update data
func exportUpdatesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT d.hostname, du.pending_updates, du.security_updates,
			       du.last_check, du.last_install
			FROM device_updates du
			JOIN devices d ON du.device_id = d.id
			WHERE d.organization_id = $1
			ORDER BY d.hostname
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Export] Error querying updates: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export updates"})
			return
		}
		defer rows.Close()

		headers := []string{"Hostname", "Pending Updates", "Security Updates", "Last Check", "Last Install"}
		var data [][]string

		for rows.Next() {
			var hostname string
			var pending, security int
			var lastCheck, lastInstall *time.Time

			if err := rows.Scan(&hostname, &pending, &security, &lastCheck, &lastInstall); err != nil {
				continue
			}

			checkStr, installStr := "", ""
			if lastCheck != nil {
				checkStr = lastCheck.Format(time.RFC3339)
			}
			if lastInstall != nil {
				installStr = lastInstall.Format(time.RFC3339)
			}

			data = append(data, []string{
				hostname,
				strconv.Itoa(pending),
				strconv.Itoa(security),
				checkStr, installStr,
			})
		}

		exportData(c, "updates", format, headers, data)
	}
}

// exportSoftwareHandler exports installed software data
func exportSoftwareHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT d.hostname, s->>'name', s->>'version', s->>'vendor',
			       COALESCE(s->>'installed_date', '')
			FROM devices d
			CROSS JOIN LATERAL jsonb_array_elements(
				COALESCE(d.installed_software, '[]'::jsonb)
			) AS s(data)
			WHERE d.organization_id = $1
			ORDER BY d.hostname, s->>'name'
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Export] Error querying software: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export software"})
			return
		}
		defer rows.Close()

		headers := []string{"Hostname", "Software Name", "Version", "Vendor", "Installed Date"}
		var data [][]string

		for rows.Next() {
			var hostname, name, version, vendor, installedDate string
			if err := rows.Scan(&hostname, &name, &version, &vendor, &installedDate); err != nil {
				continue
			}
			data = append(data, []string{hostname, name, version, vendor, installedDate})
		}

		exportData(c, "software", format, headers, data)
	}
}

// exportUsersHandler exports user data (admin only)
func exportUsersHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, email, COALESCE(username, ''), COALESCE(name, ''),
			       role, created_at, COALESCE(last_login, created_at),
			       totp_enabled
			FROM users
			WHERE organization_id = $1
			ORDER BY email
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Export] Error querying users: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export users"})
			return
		}
		defer rows.Close()

		headers := []string{"ID", "Email", "Username", "Name", "Role", "Created At", "Last Login", "MFA Enabled"}
		var data [][]string

		for rows.Next() {
			var id, email, username, name, role string
			var createdAt, lastLogin time.Time
			var mfaEnabled bool

			if err := rows.Scan(&id, &email, &username, &name, &role, &createdAt, &lastLogin, &mfaEnabled); err != nil {
				continue
			}

			data = append(data, []string{
				id, email, username, name, role,
				createdAt.Format(time.RFC3339),
				lastLogin.Format(time.RFC3339),
				strconv.FormatBool(mfaEnabled),
			})
		}

		exportData(c, "users", format, headers, data)
	}
}

// exportMetricsHandler exports device metrics data
func exportMetricsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		format := ExportFormat(c.DefaultQuery("format", "csv"))
		deviceIDStr := c.Query("deviceId")
		hours := c.DefaultQuery("hours", "24")
		ctx := context.Background()

		hoursInt, _ := strconv.Atoi(hours)
		if hoursInt <= 0 || hoursInt > 168 {
			hoursInt = 24
		}

		query := `
			SELECT d.hostname, m.cpu_percent, m.memory_percent, m.disk_percent,
			       m.network_rx_bytes, m.network_tx_bytes, m.timestamp
			FROM device_metrics m
			JOIN devices d ON m.device_id = d.id
			WHERE d.organization_id = $1
			  AND m.timestamp > NOW() - $2 * INTERVAL '1 hour'
		`
		args := []interface{}{constants.CurrentOrganizationID, hoursInt}

		if deviceIDStr != "" {
			query += " AND d.id = $3"
			args = append(args, deviceIDStr)
		}

		query += " ORDER BY d.hostname, m.timestamp DESC LIMIT 10000"

		rows, err := services.DB.Pool().Query(ctx, query, args...)
		if err != nil {
			log.Printf("[Export] Error querying metrics: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export metrics"})
			return
		}
		defer rows.Close()

		headers := []string{"Hostname", "CPU %", "Memory %", "Disk %", "Network RX Bytes", "Network TX Bytes", "Timestamp"}
		var data [][]string

		for rows.Next() {
			var hostname string
			var cpu, mem, disk float64
			var rxBytes, txBytes int64
			var timestamp time.Time

			if err := rows.Scan(&hostname, &cpu, &mem, &disk, &rxBytes, &txBytes, &timestamp); err != nil {
				continue
			}

			data = append(data, []string{
				hostname,
				fmt.Sprintf("%.2f", cpu),
				fmt.Sprintf("%.2f", mem),
				fmt.Sprintf("%.2f", disk),
				strconv.FormatInt(rxBytes, 10),
				strconv.FormatInt(txBytes, 10),
				timestamp.Format(time.RFC3339),
			})
		}

		exportData(c, "metrics", format, headers, data)
	}
}

// Helper function to export data in the requested format
func exportData(c *gin.Context, name string, format ExportFormat, headers []string, data [][]string) {
	filename := fmt.Sprintf("%s-%s", name, time.Now().Format("2006-01-02"))

	switch format {
	case FormatExcel:
		exportExcel(c, filename, headers, data)
	default:
		exportCSV(c, filename, headers, data)
	}
}

func exportCSV(c *gin.Context, filename string, headers []string, data [][]string) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header
	writer.Write(headers)

	// Write data rows
	for _, row := range data {
		writer.Write(row)
	}
	writer.Flush()

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	c.Data(http.StatusOK, "text/csv", buf.Bytes())
}

func exportExcel(c *gin.Context, filename string, headers []string, data [][]string) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "Sheet1"

	// Write headers with styling
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#4472C4"}},
	})

	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, header)
		f.SetCellStyle(sheetName, cell, cell, style)
	}

	// Write data rows
	for rowIdx, row := range data {
		for colIdx, value := range row {
			cell := fmt.Sprintf("%c%d", 'A'+colIdx, rowIdx+2)
			f.SetCellValue(sheetName, cell, value)
		}
	}

	// Auto-fit columns (approximate)
	for i := range headers {
		col := string(rune('A' + i))
		f.SetColWidth(sheetName, col, col, 15)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		log.Printf("[Export] Error writing Excel: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate Excel file"})
		return
	}

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.xlsx", filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// listExportTypesHandler returns available export types
func listExportTypesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		exports := []map[string]interface{}{
			{
				"id":          "devices",
				"name":        "Devices",
				"description": "Export all device information",
				"endpoint":    "/api/export/devices",
				"formats":     []string{"csv", "xlsx"},
			},
			{
				"id":          "alerts",
				"name":        "Alerts",
				"description": "Export alert history",
				"endpoint":    "/api/export/alerts",
				"formats":     []string{"csv", "xlsx"},
				"parameters":  []string{"start", "end"},
			},
			{
				"id":          "updates",
				"name":        "Windows Updates",
				"description": "Export Windows update status",
				"endpoint":    "/api/export/updates",
				"formats":     []string{"csv", "xlsx"},
			},
			{
				"id":          "software",
				"name":        "Installed Software",
				"description": "Export installed software inventory",
				"endpoint":    "/api/export/software",
				"formats":     []string{"csv", "xlsx"},
			},
			{
				"id":          "users",
				"name":        "Users",
				"description": "Export user list (admin only)",
				"endpoint":    "/api/export/users",
				"formats":     []string{"csv", "xlsx"},
			},
			{
				"id":          "metrics",
				"name":        "Device Metrics",
				"description": "Export device performance metrics",
				"endpoint":    "/api/export/metrics",
				"formats":     []string{"csv", "xlsx"},
				"parameters":  []string{"deviceId", "hours"},
			},
		}

		c.JSON(http.StatusOK, exports)
	}
}
