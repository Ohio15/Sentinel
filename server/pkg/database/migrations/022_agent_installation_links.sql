-- Migration: Agent Installation Links for Self-Service Portal
-- Creates tables for self-service agent download portal with email notifications

-- Main table for installation links
CREATE TABLE IF NOT EXISTS agent_installation_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Link identification
    download_token VARCHAR(64) UNIQUE NOT NULL,

    -- Device pre-registration
    device_name VARCHAR(255) NOT NULL,
    user_email VARCHAR(255) NOT NULL,
    user_name VARCHAR(255),

    -- Associated enrollment token
    enrollment_token_id UUID REFERENCES enrollment_tokens(id) ON DELETE SET NULL,

    -- Link lifecycle
    created_at TIMESTAMPTZ DEFAULT NOW(),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    -- Download tracking
    downloaded_at TIMESTAMPTZ,
    download_ip VARCHAR(45),
    download_user_agent TEXT,
    download_count INTEGER DEFAULT 0,

    -- Installation tracking
    agent_connected_at TIMESTAMPTZ,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,

    -- Status: pending, downloaded, installing, installed, expired, revoked
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'downloaded', 'installing', 'installed', 'expired', 'revoked')),
    revoked_at TIMESTAMPTZ,
    revoked_by UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Email notifications
    email_sent_at TIMESTAMPTZ,
    email_delivery_status VARCHAR(50),
    email_opened_at TIMESTAMPTZ,
    reminder_sent_at TIMESTAMPTZ,

    -- Metadata
    notes TEXT,
    metadata JSONB DEFAULT '{}'::jsonb,

    -- Soft delete support
    deleted_at TIMESTAMPTZ
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_agent_links_token ON agent_installation_links(download_token);
CREATE INDEX IF NOT EXISTS idx_agent_links_status ON agent_installation_links(status);
CREATE INDEX IF NOT EXISTS idx_agent_links_created ON agent_installation_links(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_links_device ON agent_installation_links(device_id);
CREATE INDEX IF NOT EXISTS idx_agent_links_enrollment ON agent_installation_links(enrollment_token_id);
CREATE INDEX IF NOT EXISTS idx_agent_links_expires ON agent_installation_links(expires_at);
CREATE INDEX IF NOT EXISTS idx_agent_links_email ON agent_installation_links(user_email);
CREATE INDEX IF NOT EXISTS idx_agent_links_not_deleted ON agent_installation_links(deleted_at) WHERE deleted_at IS NULL;

-- Audit table for link access attempts
CREATE TABLE IF NOT EXISTS agent_link_access_log (
    id SERIAL PRIMARY KEY,
    link_id UUID REFERENCES agent_installation_links(id) ON DELETE CASCADE,
    accessed_at TIMESTAMPTZ DEFAULT NOW(),
    ip_address VARCHAR(45),
    user_agent TEXT,
    action VARCHAR(50) NOT NULL,  -- view, download, validate, status_check
    success BOOLEAN DEFAULT TRUE,
    error_message TEXT,
    metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_link_access_link_id ON agent_link_access_log(link_id);
CREATE INDEX IF NOT EXISTS idx_link_access_time ON agent_link_access_log(accessed_at DESC);

-- Email configuration table (for multi-tenant support or overrides)
CREATE TABLE IF NOT EXISTS email_config (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(50) NOT NULL DEFAULT 'sendgrid',  -- sendgrid, ses, smtp
    api_key_encrypted TEXT,  -- Encrypted API key
    from_address VARCHAR(255) NOT NULL DEFAULT 'noreply@sentinel.local',
    from_name VARCHAR(255) DEFAULT 'Sentinel RMM',
    reply_to VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Email templates table
CREATE TABLE IF NOT EXISTS email_templates (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    subject_template TEXT NOT NULL,
    html_template TEXT NOT NULL,
    text_template TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default email templates
INSERT INTO email_templates (name, subject_template, html_template, text_template)
VALUES
    ('installation_link',
     'Install Sentinel Agent on {{.DeviceName}}',
     '<!DOCTYPE html>
<html>
<head>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, ''Segoe UI'', sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 30px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 30px; border-radius: 0 0 8px 8px; }
    .button { display: inline-block; background: #667eea; color: white; padding: 14px 28px; text-decoration: none; border-radius: 6px; font-weight: 600; margin: 20px 0; }
    .info-box { background: white; padding: 20px; border-radius: 6px; margin: 20px 0; border-left: 4px solid #667eea; }
    .steps { background: white; padding: 20px; border-radius: 6px; margin: 20px 0; }
    .step { margin: 15px 0; padding-left: 30px; position: relative; }
    .step::before { content: "✓"; position: absolute; left: 0; color: #667eea; font-weight: bold; }
    .warning { background: #fef3c7; padding: 15px; border-radius: 6px; border-left: 4px solid #f59e0b; margin: 20px 0; }
    .footer { text-align: center; color: #6b7280; font-size: 14px; margin-top: 30px; padding-top: 20px; border-top: 1px solid #e5e7eb; }
  </style>
</head>
<body>
  <div class="header">
    <h1 style="margin: 0;">Sentinel RMM</h1>
    <p style="margin: 10px 0 0;">Agent Installation Required</p>
  </div>

  <div class="content">
    <p>Hi {{.UserName}},</p>

    <p>You need to install the Sentinel Agent on your device (<strong>{{.DeviceName}}</strong>) to enable remote support and monitoring.</p>

    <div class="info-box">
      <strong>Device Name:</strong> {{.DeviceName}}<br>
      <strong>Link Expires:</strong> {{.ExpirationDate}}<br>
      <strong>Installation Time:</strong> ~2 minutes
    </div>

    <center>
      <a href="{{.DownloadURL}}" class="button">Install Sentinel Agent</a>
    </center>

    <div class="steps">
      <h3 style="margin-top: 0;">Installation Steps:</h3>
      <div class="step">Click the button above to open the installation portal</div>
      <div class="step">Download the installer package</div>
      <div class="step">Run the installer (you may see a security warning - this is normal)</div>
      <div class="step">Click "Yes" when prompted for administrator permission</div>
      <div class="step">The agent will connect automatically after installation</div>
    </div>

    <div class="warning">
      <strong>Important:</strong> This link expires in {{.HoursRemaining}} hours. If you miss this window, please contact your IT administrator for a new link.
    </div>

    <p><strong>Having trouble?</strong></p>
    <ul>
      <li>Make sure you are logged in with an administrator account</li>
      <li>Temporarily disable antivirus if download is blocked</li>
      <li>Contact support if you see any error messages</li>
    </ul>
  </div>

  <div class="footer">
    <p>Need help? Contact {{.SupportEmail}}</p>
    <p style="font-size: 12px; color: #9ca3af;">
      This email was sent by {{.CompanyName}}<br>
      You received this because your IT administrator is deploying remote management software
    </p>
  </div>
</body>
</html>',
     'Hi {{.UserName}},

You need to install the Sentinel Agent on {{.DeviceName}}.

Installation Link: {{.DownloadURL}}

This link expires: {{.ExpirationDate}}

INSTALLATION STEPS:
1. Click the link above to open the installation portal
2. Download the installer package
3. Run the installer (click Yes for admin permission)
4. The agent connects automatically

Need help? Contact {{.SupportEmail}}'),

    ('installation_reminder',
     'Reminder: Install Sentinel Agent on {{.DeviceName}}',
     '<!DOCTYPE html>
<html>
<head>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, ''Segoe UI'', sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: #f59e0b; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 30px; border-radius: 0 0 8px 8px; }
    .button { display: inline-block; background: #667eea; color: white; padding: 14px 28px; text-decoration: none; border-radius: 6px; font-weight: 600; margin: 20px 0; }
    .footer { text-align: center; color: #6b7280; font-size: 14px; margin-top: 20px; }
  </style>
</head>
<body>
  <div class="header">
    <h2 style="margin: 0;">Reminder: Complete Your Installation</h2>
  </div>
  <div class="content">
    <p>Hi {{.UserName}},</p>
    <p>You have not yet installed the Sentinel Agent on <strong>{{.DeviceName}}</strong>.</p>
    <p>This link will expire on <strong>{{.ExpirationDate}}</strong>.</p>
    <center>
      <a href="{{.DownloadURL}}" class="button">Install Now</a>
    </center>
  </div>
  <div class="footer">
    <p>Need help? Contact {{.SupportEmail}}</p>
  </div>
</body>
</html>',
     'Hi {{.UserName}},

Reminder: You have not yet installed the Sentinel Agent on {{.DeviceName}}.

Installation Link: {{.DownloadURL}}

This link expires: {{.ExpirationDate}}

Need help? Contact {{.SupportEmail}}'),

    ('installation_confirmed',
     'Sentinel Agent Successfully Installed on {{.DeviceName}}',
     '<!DOCTYPE html>
<html>
<head>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, ''Segoe UI'', sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: #10b981; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 30px; border-radius: 0 0 8px 8px; }
    .success-icon { font-size: 48px; }
    .footer { text-align: center; color: #6b7280; font-size: 14px; margin-top: 20px; }
  </style>
</head>
<body>
  <div class="header">
    <div class="success-icon">&#10004;</div>
    <h2 style="margin: 10px 0 0;">Installation Complete!</h2>
  </div>
  <div class="content">
    <p>Hi {{.UserName}},</p>
    <p>The Sentinel Agent has been successfully installed on <strong>{{.DeviceName}}</strong>.</p>
    <p>Your device is now being monitored and can receive remote support.</p>
    <p style="color: #6b7280;">Installed: {{.InstalledAt}}<br>Agent Version: {{.AgentVersion}}</p>
  </div>
  <div class="footer">
    <p>If you have any questions, contact {{.SupportEmail}}</p>
  </div>
</body>
</html>',
     'Hi {{.UserName}},

The Sentinel Agent has been successfully installed on {{.DeviceName}}.

Your device is now being monitored and can receive remote support.

Installed: {{.InstalledAt}}
Agent Version: {{.AgentVersion}}

If you have any questions, contact {{.SupportEmail}}')
ON CONFLICT (name) DO NOTHING;

-- Add email settings to system settings if not exists
-- This will be handled by the application layer

-- Add comments for documentation
COMMENT ON TABLE agent_installation_links IS 'Self-service agent installation links sent to end users via email';
COMMENT ON TABLE agent_link_access_log IS 'Audit log for all access attempts to installation links';
COMMENT ON TABLE email_config IS 'Email service configuration for notifications';
COMMENT ON TABLE email_templates IS 'Email templates for installation notifications';

COMMENT ON COLUMN agent_installation_links.download_token IS 'Public token used in download URL (64-char hex)';
COMMENT ON COLUMN agent_installation_links.status IS 'Link lifecycle status: pending, downloaded, installing, installed, expired, revoked';
COMMENT ON COLUMN agent_installation_links.email_delivery_status IS 'Email delivery status: pending, sent, delivered, failed, bounced';
