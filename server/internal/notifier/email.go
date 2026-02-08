package notifier

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"time"

	"github.com/sentinel/server/internal/alerting"
)

var (
	ErrEmailNotConfigured = errors.New("email not configured")
	ErrNoRecipients       = errors.New("no recipients specified")
)

// EmailConfig holds SMTP configuration
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	UseTLS   bool
}

// EmailClient handles sending emails
type EmailClient struct {
	config EmailConfig
}

// NewEmailClient creates a new email client
func NewEmailClient(config EmailConfig) *EmailClient {
	return &EmailClient{config: config}
}

// SendAlert sends an alert notification email
func (c *EmailClient) SendAlert(recipients []string, alert *alerting.Alert, device *alerting.DeviceMetrics, rule *alerting.AlertRule) error {
	if len(recipients) == 0 {
		return ErrNoRecipients
	}

	subject := fmt.Sprintf("[%s] %s - %s", strings.ToUpper(alert.Severity), rule.Name, device.Hostname)

	data := struct {
		Alert    *alerting.Alert
		Device   *alerting.DeviceMetrics
		Rule     *alerting.AlertRule
		Severity string
		Time     string
	}{
		Alert:    alert,
		Device:   device,
		Rule:     rule,
		Severity: strings.ToUpper(alert.Severity),
		Time:     time.Now().Format("2006-01-02 15:04:05 MST"),
	}

	htmlBody, err := c.renderTemplate(alertEmailTemplate, data)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	textBody, err := c.renderTemplate(alertEmailTextTemplate, data)
	if err != nil {
		return fmt.Errorf("render text template: %w", err)
	}

	return c.send(recipients, subject, htmlBody, textBody)
}

// SendTest sends a test email
func (c *EmailClient) SendTest(to string) error {
	subject := "Sentinel RMM - Test Email"
	htmlBody := `
		<html>
		<body style="font-family: Arial, sans-serif;">
			<h2>Sentinel RMM Email Test</h2>
			<p>This is a test email to verify your SMTP configuration.</p>
			<p>If you received this email, your email notifications are working correctly.</p>
			<p style="color: #666; font-size: 12px;">Sent at: ` + time.Now().Format("2006-01-02 15:04:05 MST") + `</p>
		</body>
		</html>
	`
	textBody := "Sentinel RMM Email Test\n\nThis is a test email to verify your SMTP configuration.\nIf you received this email, your email notifications are working correctly."

	return c.send([]string{to}, subject, htmlBody, textBody)
}

func (c *EmailClient) send(to []string, subject, htmlBody, textBody string) error {
	// Build MIME message
	boundary := "sentinel-email-boundary"

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", c.config.FromName, c.config.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Plain text part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n")

	// HTML part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	var auth smtp.Auth
	if c.config.Username != "" {
		auth = smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.Host)
	}

	if c.config.UseTLS {
		return c.sendTLS(addr, auth, to, msg.Bytes())
	}

	return smtp.SendMail(addr, auth, c.config.From, to, msg.Bytes())
}

func (c *EmailClient) sendTLS(addr string, auth smtp.Auth, to []string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName: c.config.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.config.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(c.config.From); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp rcpt: %w", err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	return client.Quit()
}

func (c *EmailClient) renderTemplate(tmpl string, data interface{}) (string, error) {
	t, err := template.New("email").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Alert email HTML template
const alertEmailTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 600px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header { padding: 20px; color: white; }
        .header.critical { background: #dc2626; }
        .header.warning { background: #f59e0b; }
        .header.info { background: #3b82f6; }
        .header h1 { margin: 0; font-size: 24px; }
        .content { padding: 20px; }
        .detail { margin-bottom: 15px; }
        .detail-label { color: #6b7280; font-size: 12px; text-transform: uppercase; margin-bottom: 4px; }
        .detail-value { color: #111827; font-size: 16px; }
        .footer { padding: 20px; background: #f9fafb; border-top: 1px solid #e5e7eb; font-size: 12px; color: #6b7280; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header {{.Alert.Severity}}">
            <h1>🚨 {{.Severity}} Alert</h1>
        </div>
        <div class="content">
            <div class="detail">
                <div class="detail-label">Alert</div>
                <div class="detail-value">{{.Rule.Name}}</div>
            </div>
            <div class="detail">
                <div class="detail-label">Device</div>
                <div class="detail-value">{{.Device.Hostname}}</div>
            </div>
            <div class="detail">
                <div class="detail-label">Message</div>
                <div class="detail-value">{{.Alert.Message}}</div>
            </div>
            <div class="detail">
                <div class="detail-label">Time</div>
                <div class="detail-value">{{.Time}}</div>
            </div>
        </div>
        <div class="footer">
            This alert was generated by Sentinel RMM. Log in to the dashboard to view more details and manage this alert.
        </div>
    </div>
</body>
</html>
`

// Alert email plain text template
const alertEmailTextTemplate = `
{{.Severity}} ALERT

Alert: {{.Rule.Name}}
Device: {{.Device.Hostname}}
Message: {{.Alert.Message}}
Time: {{.Time}}

Log in to Sentinel RMM to view more details and manage this alert.
`
