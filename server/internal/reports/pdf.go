package reports

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jung-kurt/gofpdf"
	"github.com/sentinel/server/internal/constants"
)

// PDFGenerator generates PDF reports
type PDFGenerator struct {
	db *pgxpool.Pool
}

// NewPDFGenerator creates a new PDF generator
func NewPDFGenerator(db *pgxpool.Pool) *PDFGenerator {
	return &PDFGenerator{db: db}
}

// ReportType represents different types of reports
type ReportType string

const (
	ReportTypeSecurityPosture ReportType = "security_posture"
	ReportTypeDeviceSummary   ReportType = "device_summary"
	ReportTypeAlertHistory    ReportType = "alert_history"
	ReportTypeUpdateStatus    ReportType = "update_status"
	ReportTypeExecutive       ReportType = "executive"
)

// ReportOptions configures report generation
type ReportOptions struct {
	Type       ReportType
	DeviceID   *uuid.UUID    // Optional: for device-specific reports
	StartDate  *time.Time    // Optional: for date-ranged reports
	EndDate    *time.Time    // Optional: for date-ranged reports
	Title      string
	Subtitle   string
}

// DeviceData holds device information for reports
type DeviceData struct {
	ID              uuid.UUID
	Hostname        string
	OSType          string
	OSVersion       string
	Status          string
	LastSeen        time.Time
	CPUPercent      float64
	MemoryPercent   float64
	DiskPercent     float64
	PendingUpdates  int
	SecurityScore   int
}

// AlertData holds alert information for reports
type AlertData struct {
	ID        uuid.UUID
	Hostname  string
	Severity  string
	Title     string
	Message   string
	Status    string
	CreatedAt time.Time
}

// colors for the PDF
const (
	colorPrimary   = 0x1a73e8 // Blue
	colorDanger    = 0xdc3545 // Red
	colorWarning   = 0xffc107 // Yellow
	colorSuccess   = 0x28a745 // Green
	colorSecondary = 0x6c757d // Gray
)

// GenerateSecurityPostureReport generates a security posture PDF report
func (g *PDFGenerator) GenerateSecurityPostureReport(ctx context.Context, opts ReportOptions) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)

	// Add first page with header
	pdf.AddPage()
	g.addHeader(pdf, "Security Posture Report", time.Now())

	// Get summary data
	summary, err := g.getSecuritySummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("get security summary: %w", err)
	}

	// Add summary section
	pdf.SetY(45)
	g.addSummarySection(pdf, summary)

	// Add device status breakdown
	pdf.Ln(10)
	g.addDeviceStatusSection(pdf, summary)

	// Add critical alerts section
	alerts, err := g.getRecentAlerts(ctx, 10)
	if err == nil && len(alerts) > 0 {
		pdf.Ln(10)
		g.addAlertsSection(pdf, alerts)
	}

	// Add update status section
	updateSummary, err := g.getUpdateSummary(ctx)
	if err == nil {
		pdf.Ln(10)
		g.addUpdateSection(pdf, updateSummary)
	}

	// Add footer
	g.addFooter(pdf)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("output PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// GenerateDeviceSummaryReport generates a single device summary report
func (g *PDFGenerator) GenerateDeviceSummaryReport(ctx context.Context, deviceID uuid.UUID) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)

	// Get device data
	device, err := g.getDeviceData(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get device data: %w", err)
	}

	pdf.AddPage()
	g.addHeader(pdf, fmt.Sprintf("Device Report: %s", device.Hostname), time.Now())

	// Device info section
	pdf.SetY(45)
	g.addDeviceInfoSection(pdf, device)

	// Resource usage section
	pdf.Ln(10)
	g.addResourceSection(pdf, device)

	// Device alerts
	deviceAlerts, err := g.getDeviceAlerts(ctx, deviceID, 10)
	if err == nil && len(deviceAlerts) > 0 {
		pdf.Ln(10)
		g.addAlertsSection(pdf, deviceAlerts)
	}

	g.addFooter(pdf)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("output PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// GenerateAlertHistoryReport generates an alert history report
func (g *PDFGenerator) GenerateAlertHistoryReport(ctx context.Context, startDate, endDate time.Time) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)

	pdf.AddPage()
	g.addHeader(pdf, "Alert History Report", time.Now())

	// Date range subtitle
	pdf.SetY(38)
	pdf.SetFont("Arial", "I", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(190, 5, fmt.Sprintf("Period: %s to %s",
		startDate.Format("Jan 2, 2006"),
		endDate.Format("Jan 2, 2006")), "", 0, "C", false, 0, "")

	// Get alerts for the period
	alerts, err := g.getAlertsByDateRange(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("get alerts: %w", err)
	}

	// Summary stats
	pdf.SetY(50)
	g.addAlertSummaryStats(pdf, alerts)

	// Alert details table
	pdf.Ln(10)
	g.addAlertDetailsTable(pdf, alerts)

	g.addFooter(pdf)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("output PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// GenerateExecutiveReport generates a high-level executive summary
func (g *PDFGenerator) GenerateExecutiveReport(ctx context.Context) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)

	pdf.AddPage()
	g.addHeader(pdf, "Executive Summary Report", time.Now())

	// Get all summary data
	summary, _ := g.getSecuritySummary(ctx)
	updateSummary, _ := g.getUpdateSummary(ctx)

	// Key metrics section
	pdf.SetY(45)
	g.addKeyMetricsSection(pdf, summary, updateSummary)

	// Trends section (placeholder for future implementation)
	pdf.Ln(15)
	g.addTrendsSection(pdf)

	// Recommendations
	pdf.Ln(10)
	g.addRecommendationsSection(pdf, summary)

	g.addFooter(pdf)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("output PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// Helper functions

func (g *PDFGenerator) addHeader(pdf *gofpdf.Fpdf, title string, date time.Time) {
	// Logo placeholder / company name
	pdf.SetFont("Arial", "B", 20)
	pdf.SetTextColor(26, 115, 232) // Primary blue
	pdf.CellFormat(190, 10, "Sentinel RMM", "", 0, "L", false, 0, "")

	// Title
	pdf.Ln(12)
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, title, "", 0, "L", false, 0, "")

	// Date
	pdf.Ln(8)
	pdf.SetFont("Arial", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(190, 5, fmt.Sprintf("Generated: %s", date.Format("January 2, 2006 3:04 PM")), "", 0, "L", false, 0, "")

	// Separator line
	pdf.Ln(8)
	pdf.SetDrawColor(200, 200, 200)
	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())
}

func (g *PDFGenerator) addFooter(pdf *gofpdf.Fpdf) {
	pdf.SetY(-15)
	pdf.SetFont("Arial", "I", 8)
	pdf.SetTextColor(150, 150, 150)
	pdf.CellFormat(0, 10, fmt.Sprintf("Page %d - Generated by Sentinel RMM", pdf.PageNo()), "", 0, "C", false, 0, "")
}

type securitySummary struct {
	TotalDevices   int
	OnlineDevices  int
	OfflineDevices int
	CriticalAlerts int
	WarningAlerts  int
	SecurityScore  int
}

func (g *PDFGenerator) getSecuritySummary(ctx context.Context) (*securitySummary, error) {
	summary := &securitySummary{}

	// Get device counts
	err := g.db.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'online'),
			COUNT(*) FILTER (WHERE status = 'offline')
		FROM devices
		WHERE organization_id = $1 AND is_disabled = false
	`, constants.CurrentOrganizationID).Scan(
		&summary.TotalDevices,
		&summary.OnlineDevices,
		&summary.OfflineDevices,
	)
	if err != nil {
		return nil, err
	}

	// Get alert counts
	err = g.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE severity = 'critical' AND status = 'open'),
			COUNT(*) FILTER (WHERE severity = 'warning' AND status = 'open')
		FROM alerts a
		JOIN devices d ON a.device_id = d.id
		WHERE d.organization_id = $1
	`, constants.CurrentOrganizationID).Scan(
		&summary.CriticalAlerts,
		&summary.WarningAlerts,
	)
	if err != nil {
		// Non-fatal, continue with zeros
		summary.CriticalAlerts = 0
		summary.WarningAlerts = 0
	}

	// Calculate security score (simple algorithm)
	if summary.TotalDevices > 0 {
		onlineRatio := float64(summary.OnlineDevices) / float64(summary.TotalDevices)
		alertPenalty := float64(summary.CriticalAlerts*10 + summary.WarningAlerts*3)
		summary.SecurityScore = int(onlineRatio*100 - alertPenalty)
		if summary.SecurityScore < 0 {
			summary.SecurityScore = 0
		}
		if summary.SecurityScore > 100 {
			summary.SecurityScore = 100
		}
	}

	return summary, nil
}

func (g *PDFGenerator) addSummarySection(pdf *gofpdf.Fpdf, summary *securitySummary) {
	pdf.SetFont("Arial", "B", 14)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Fleet Overview", "", 1, "L", false, 0, "")
	pdf.Ln(3)

	// Metrics boxes
	boxWidth := 45.0
	boxHeight := 25.0
	startX := 10.0

	// Total Devices
	g.addMetricBox(pdf, startX, pdf.GetY(), boxWidth, boxHeight,
		"Total Devices", fmt.Sprintf("%d", summary.TotalDevices), 26, 115, 232)

	// Online Devices
	g.addMetricBox(pdf, startX+boxWidth+5, pdf.GetY(), boxWidth, boxHeight,
		"Online", fmt.Sprintf("%d", summary.OnlineDevices), 40, 167, 69)

	// Offline Devices
	g.addMetricBox(pdf, startX+(boxWidth+5)*2, pdf.GetY(), boxWidth, boxHeight,
		"Offline", fmt.Sprintf("%d", summary.OfflineDevices), 108, 117, 125)

	// Security Score
	scoreColor := [3]int{40, 167, 69} // Green
	if summary.SecurityScore < 70 {
		scoreColor = [3]int{255, 193, 7} // Yellow
	}
	if summary.SecurityScore < 50 {
		scoreColor = [3]int{220, 53, 69} // Red
	}
	g.addMetricBox(pdf, startX+(boxWidth+5)*3, pdf.GetY(), boxWidth, boxHeight,
		"Security Score", fmt.Sprintf("%d%%", summary.SecurityScore), scoreColor[0], scoreColor[1], scoreColor[2])

	pdf.SetY(pdf.GetY() + boxHeight + 5)
}

func (g *PDFGenerator) addMetricBox(pdf *gofpdf.Fpdf, x, y, w, h float64, label, value string, r, gr, b int) {
	pdf.SetFillColor(r, gr, b)
	pdf.RoundedRect(x, y, w, h, 3, "1234", "F")

	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(x, y+5)
	pdf.SetFont("Arial", "", 9)
	pdf.CellFormat(w, 5, label, "", 0, "C", false, 0, "")

	pdf.SetXY(x, y+12)
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(w, 8, value, "", 0, "C", false, 0, "")
}

func (g *PDFGenerator) addDeviceStatusSection(pdf *gofpdf.Fpdf, summary *securitySummary) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Alert Summary", "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	if summary.CriticalAlerts > 0 {
		pdf.SetTextColor(220, 53, 69)
		pdf.CellFormat(190, 6, fmt.Sprintf("Critical Alerts: %d", summary.CriticalAlerts), "", 1, "L", false, 0, "")
	}
	if summary.WarningAlerts > 0 {
		pdf.SetTextColor(255, 193, 7)
		pdf.CellFormat(190, 6, fmt.Sprintf("Warning Alerts: %d", summary.WarningAlerts), "", 1, "L", false, 0, "")
	}
	if summary.CriticalAlerts == 0 && summary.WarningAlerts == 0 {
		pdf.SetTextColor(40, 167, 69)
		pdf.CellFormat(190, 6, "No open alerts - All systems operational", "", 1, "L", false, 0, "")
	}
}

func (g *PDFGenerator) getRecentAlerts(ctx context.Context, limit int) ([]AlertData, error) {
	rows, err := g.db.Query(ctx, `
		SELECT a.id, COALESCE(d.hostname, 'Unknown'), a.severity, a.title,
		       COALESCE(a.message, ''), a.status, a.created_at
		FROM alerts a
		JOIN devices d ON a.device_id = d.id
		WHERE d.organization_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2
	`, constants.CurrentOrganizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AlertData
	for rows.Next() {
		var a AlertData
		if err := rows.Scan(&a.ID, &a.Hostname, &a.Severity, &a.Title, &a.Message, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

func (g *PDFGenerator) addAlertsSection(pdf *gofpdf.Fpdf, alerts []AlertData) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Recent Alerts", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Table header
	pdf.SetFillColor(248, 249, 250)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(40, 7, "Device", "1", 0, "L", true, 0, "")
	pdf.CellFormat(25, 7, "Severity", "1", 0, "C", true, 0, "")
	pdf.CellFormat(80, 7, "Title", "1", 0, "L", true, 0, "")
	pdf.CellFormat(45, 7, "Time", "1", 1, "C", true, 0, "")

	// Table rows
	pdf.SetFont("Arial", "", 9)
	for _, alert := range alerts {
		// Set severity color
		switch alert.Severity {
		case "critical":
			pdf.SetTextColor(220, 53, 69)
		case "warning":
			pdf.SetTextColor(255, 140, 0)
		default:
			pdf.SetTextColor(33, 37, 41)
		}

		pdf.CellFormat(40, 6, truncate(alert.Hostname, 20), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 6, alert.Severity, "1", 0, "C", false, 0, "")
		pdf.SetTextColor(33, 37, 41)
		pdf.CellFormat(80, 6, truncate(alert.Title, 40), "1", 0, "L", false, 0, "")
		pdf.CellFormat(45, 6, alert.CreatedAt.Format("Jan 2, 15:04"), "1", 1, "C", false, 0, "")
	}
}

type updateSummary struct {
	DevicesWithUpdates int
	TotalPending       int
	CriticalPending    int
}

func (g *PDFGenerator) getUpdateSummary(ctx context.Context) (*updateSummary, error) {
	summary := &updateSummary{}

	err := g.db.QueryRow(ctx, `
		SELECT
			COUNT(DISTINCT device_id),
			COALESCE(SUM(pending_updates), 0),
			COALESCE(SUM(security_updates), 0)
		FROM device_updates du
		JOIN devices d ON du.device_id = d.id
		WHERE d.organization_id = $1
		  AND pending_updates > 0
	`, constants.CurrentOrganizationID).Scan(
		&summary.DevicesWithUpdates,
		&summary.TotalPending,
		&summary.CriticalPending,
	)
	if err != nil {
		return summary, err
	}

	return summary, nil
}

func (g *PDFGenerator) addUpdateSection(pdf *gofpdf.Fpdf, summary *updateSummary) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Windows Update Status", "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	pdf.SetTextColor(33, 37, 41)

	if summary.TotalPending == 0 {
		pdf.SetTextColor(40, 167, 69)
		pdf.CellFormat(190, 6, "All devices are up to date", "", 1, "L", false, 0, "")
	} else {
		pdf.CellFormat(190, 6, fmt.Sprintf("Devices with pending updates: %d", summary.DevicesWithUpdates), "", 1, "L", false, 0, "")
		pdf.CellFormat(190, 6, fmt.Sprintf("Total pending updates: %d", summary.TotalPending), "", 1, "L", false, 0, "")
		if summary.CriticalPending > 0 {
			pdf.SetTextColor(220, 53, 69)
			pdf.CellFormat(190, 6, fmt.Sprintf("Security updates pending: %d", summary.CriticalPending), "", 1, "L", false, 0, "")
		}
	}
}

func (g *PDFGenerator) getDeviceData(ctx context.Context, deviceID uuid.UUID) (*DeviceData, error) {
	device := &DeviceData{ID: deviceID}

	err := g.db.QueryRow(ctx, `
		SELECT d.hostname, COALESCE(d.os_type, 'Unknown'), COALESCE(d.os_version, ''),
		       d.status, d.last_seen,
		       COALESCE(m.cpu_percent, 0), COALESCE(m.memory_percent, 0), COALESCE(m.disk_percent, 0)
		FROM devices d
		LEFT JOIN LATERAL (
			SELECT cpu_percent, memory_percent, disk_percent
			FROM device_metrics
			WHERE device_id = d.id
			ORDER BY timestamp DESC
			LIMIT 1
		) m ON true
		WHERE d.id = $1 AND d.organization_id = $2
	`, deviceID, constants.CurrentOrganizationID).Scan(
		&device.Hostname, &device.OSType, &device.OSVersion,
		&device.Status, &device.LastSeen,
		&device.CPUPercent, &device.MemoryPercent, &device.DiskPercent,
	)
	if err != nil {
		return nil, err
	}

	// Get pending updates count
	g.db.QueryRow(ctx, `
		SELECT COALESCE(pending_updates, 0) FROM device_updates WHERE device_id = $1
	`, deviceID).Scan(&device.PendingUpdates)

	return device, nil
}

func (g *PDFGenerator) addDeviceInfoSection(pdf *gofpdf.Fpdf, device *DeviceData) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Device Information", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "", 10)
	infoItems := [][2]string{
		{"Hostname", device.Hostname},
		{"Operating System", fmt.Sprintf("%s %s", device.OSType, device.OSVersion)},
		{"Status", device.Status},
		{"Last Seen", device.LastSeen.Format("Jan 2, 2006 3:04 PM")},
		{"Pending Updates", fmt.Sprintf("%d", device.PendingUpdates)},
	}

	for _, item := range infoItems {
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(50, 6, item[0]+":", "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.CellFormat(140, 6, item[1], "", 1, "L", false, 0, "")
	}
}

func (g *PDFGenerator) addResourceSection(pdf *gofpdf.Fpdf, device *DeviceData) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Resource Usage", "", 1, "L", false, 0, "")
	pdf.Ln(3)

	// CPU bar
	g.addProgressBar(pdf, "CPU", device.CPUPercent)
	// Memory bar
	g.addProgressBar(pdf, "Memory", device.MemoryPercent)
	// Disk bar
	g.addProgressBar(pdf, "Disk", device.DiskPercent)
}

func (g *PDFGenerator) addProgressBar(pdf *gofpdf.Fpdf, label string, value float64) {
	pdf.SetFont("Arial", "", 10)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(30, 6, label+":", "", 0, "L", false, 0, "")

	// Bar background
	barX := pdf.GetX()
	barY := pdf.GetY()
	barWidth := 120.0
	barHeight := 5.0

	pdf.SetFillColor(230, 230, 230)
	pdf.Rect(barX, barY, barWidth, barHeight, "F")

	// Bar fill
	if value < 70 {
		pdf.SetFillColor(40, 167, 69) // Green
	} else if value < 90 {
		pdf.SetFillColor(255, 193, 7) // Yellow
	} else {
		pdf.SetFillColor(220, 53, 69) // Red
	}
	pdf.Rect(barX, barY, barWidth*value/100, barHeight, "F")

	// Value text
	pdf.SetX(barX + barWidth + 5)
	pdf.CellFormat(20, 6, fmt.Sprintf("%.1f%%", value), "", 1, "L", false, 0, "")
}

func (g *PDFGenerator) getDeviceAlerts(ctx context.Context, deviceID uuid.UUID, limit int) ([]AlertData, error) {
	rows, err := g.db.Query(ctx, `
		SELECT a.id, COALESCE(d.hostname, 'Unknown'), a.severity, a.title,
		       COALESCE(a.message, ''), a.status, a.created_at
		FROM alerts a
		JOIN devices d ON a.device_id = d.id
		WHERE a.device_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AlertData
	for rows.Next() {
		var a AlertData
		if err := rows.Scan(&a.ID, &a.Hostname, &a.Severity, &a.Title, &a.Message, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

func (g *PDFGenerator) getAlertsByDateRange(ctx context.Context, start, end time.Time) ([]AlertData, error) {
	rows, err := g.db.Query(ctx, `
		SELECT a.id, COALESCE(d.hostname, 'Unknown'), a.severity, a.title,
		       COALESCE(a.message, ''), a.status, a.created_at
		FROM alerts a
		JOIN devices d ON a.device_id = d.id
		WHERE d.organization_id = $1
		  AND a.created_at >= $2
		  AND a.created_at <= $3
		ORDER BY a.created_at DESC
	`, constants.CurrentOrganizationID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AlertData
	for rows.Next() {
		var a AlertData
		if err := rows.Scan(&a.ID, &a.Hostname, &a.Severity, &a.Title, &a.Message, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

func (g *PDFGenerator) addAlertSummaryStats(pdf *gofpdf.Fpdf, alerts []AlertData) {
	critical := 0
	warning := 0
	info := 0
	resolved := 0

	for _, a := range alerts {
		switch a.Severity {
		case "critical":
			critical++
		case "warning":
			warning++
		default:
			info++
		}
		if a.Status == "resolved" {
			resolved++
		}
	}

	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Summary Statistics", "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(95, 6, fmt.Sprintf("Total Alerts: %d", len(alerts)), "", 0, "L", false, 0, "")
	pdf.CellFormat(95, 6, fmt.Sprintf("Resolved: %d", resolved), "", 1, "L", false, 0, "")
	pdf.CellFormat(95, 6, fmt.Sprintf("Critical: %d", critical), "", 0, "L", false, 0, "")
	pdf.CellFormat(95, 6, fmt.Sprintf("Warning: %d", warning), "", 1, "L", false, 0, "")
}

func (g *PDFGenerator) addAlertDetailsTable(pdf *gofpdf.Fpdf, alerts []AlertData) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Alert Details", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Table header
	pdf.SetFillColor(248, 249, 250)
	pdf.SetFont("Arial", "B", 8)
	pdf.CellFormat(35, 6, "Device", "1", 0, "L", true, 0, "")
	pdf.CellFormat(20, 6, "Severity", "1", 0, "C", true, 0, "")
	pdf.CellFormat(65, 6, "Title", "1", 0, "L", true, 0, "")
	pdf.CellFormat(20, 6, "Status", "1", 0, "C", true, 0, "")
	pdf.CellFormat(50, 6, "Time", "1", 1, "C", true, 0, "")

	pdf.SetFont("Arial", "", 8)
	for _, alert := range alerts {
		pdf.CellFormat(35, 5, truncate(alert.Hostname, 18), "1", 0, "L", false, 0, "")
		pdf.CellFormat(20, 5, alert.Severity, "1", 0, "C", false, 0, "")
		pdf.CellFormat(65, 5, truncate(alert.Title, 35), "1", 0, "L", false, 0, "")
		pdf.CellFormat(20, 5, alert.Status, "1", 0, "C", false, 0, "")
		pdf.CellFormat(50, 5, alert.CreatedAt.Format("Jan 2, 2006 15:04"), "1", 1, "C", false, 0, "")
	}
}

func (g *PDFGenerator) addKeyMetricsSection(pdf *gofpdf.Fpdf, summary *securitySummary, updates *updateSummary) {
	pdf.SetFont("Arial", "B", 14)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 10, "Key Performance Indicators", "", 1, "L", false, 0, "")
	pdf.Ln(5)

	// KPI boxes
	boxWidth := 60.0
	boxHeight := 30.0
	startX := 10.0
	y := pdf.GetY()

	// Availability
	availability := 0
	if summary.TotalDevices > 0 {
		availability = summary.OnlineDevices * 100 / summary.TotalDevices
	}
	g.addKPIBox(pdf, startX, y, boxWidth, boxHeight, "Fleet Availability",
		fmt.Sprintf("%d%%", availability), getScoreColor(availability))

	// Security Score
	g.addKPIBox(pdf, startX+boxWidth+5, y, boxWidth, boxHeight, "Security Score",
		fmt.Sprintf("%d%%", summary.SecurityScore), getScoreColor(summary.SecurityScore))

	// Patch Compliance
	patchCompliance := 100
	if summary.TotalDevices > 0 && updates != nil && updates.DevicesWithUpdates > 0 {
		patchCompliance = (summary.TotalDevices - updates.DevicesWithUpdates) * 100 / summary.TotalDevices
	}
	g.addKPIBox(pdf, startX+(boxWidth+5)*2, y, boxWidth, boxHeight, "Patch Compliance",
		fmt.Sprintf("%d%%", patchCompliance), getScoreColor(patchCompliance))

	pdf.SetY(y + boxHeight + 10)
}

func (g *PDFGenerator) addKPIBox(pdf *gofpdf.Fpdf, x, y, w, h float64, label, value string, color [3]int) {
	// Border
	pdf.SetDrawColor(200, 200, 200)
	pdf.RoundedRect(x, y, w, h, 3, "1234", "D")

	// Label
	pdf.SetTextColor(100, 100, 100)
	pdf.SetFont("Arial", "", 9)
	pdf.SetXY(x, y+5)
	pdf.CellFormat(w, 5, label, "", 0, "C", false, 0, "")

	// Value
	pdf.SetTextColor(color[0], color[1], color[2])
	pdf.SetFont("Arial", "B", 20)
	pdf.SetXY(x, y+13)
	pdf.CellFormat(w, 10, value, "", 0, "C", false, 0, "")
}

func (g *PDFGenerator) addTrendsSection(pdf *gofpdf.Fpdf) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Trends & Analysis", "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "I", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(190, 6, "Historical trend analysis is available in the web dashboard.", "", 1, "L", false, 0, "")
}

func (g *PDFGenerator) addRecommendationsSection(pdf *gofpdf.Fpdf, summary *securitySummary) {
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(33, 37, 41)
	pdf.CellFormat(190, 8, "Recommendations", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "", 10)
	recommendations := []string{}

	if summary.OfflineDevices > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Investigate %d offline device(s) and restore connectivity", summary.OfflineDevices))
	}
	if summary.CriticalAlerts > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Address %d critical alert(s) immediately", summary.CriticalAlerts))
	}
	if summary.SecurityScore < 80 {
		recommendations = append(recommendations,
			"Review security policies and update vulnerable systems")
	}

	if len(recommendations) == 0 {
		pdf.SetTextColor(40, 167, 69)
		pdf.CellFormat(190, 6, "No immediate actions required. System health is optimal.", "", 1, "L", false, 0, "")
	} else {
		for i, rec := range recommendations {
			pdf.SetTextColor(33, 37, 41)
			pdf.CellFormat(190, 6, fmt.Sprintf("%d. %s", i+1, rec), "", 1, "L", false, 0, "")
		}
	}
}

func getScoreColor(score int) [3]int {
	if score >= 80 {
		return [3]int{40, 167, 69} // Green
	} else if score >= 60 {
		return [3]int{255, 193, 7} // Yellow
	}
	return [3]int{220, 53, 69} // Red
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
