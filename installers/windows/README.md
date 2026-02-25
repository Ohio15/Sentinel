# Sentinel Agent Windows Installer

Professional Windows installer for the Sentinel RMM Agent using Inno Setup 6.

## Prerequisites

1. **Inno Setup 6** - Download from [jrsoftware.org](https://jrsoftware.org/isdl.php)
2. **PowerShell 5.1+** (included with Windows 10+)
3. **Agent binaries** in `release/agent/`:
   - `sentinel-agent.exe`
   - `sentinel-watchdog.exe`

## Quick Start

### Build Template Installer

```powershell
cd D:\Projects\Sentinel\installers\windows
.\build.ps1
```

This creates `release/agent/sentinel-installer-template.exe` - a base installer without embedded config.

### Create Organization-Specific Installer

```powershell
.\embed-config.ps1 `
    -ServerUrl "https://sentinelrmm.us" `
    -GrpcEndpoint "sentinelrmm.us:4444" `
    -EnrollmentToken "your-token-here" `
    -OrganizationId "org-uuid-here" `
    -OutputInstaller "sentinel-installer-acme.exe"
```

Or use a config file:

```powershell
.\embed-config.ps1 -ConfigFile ".\org-config.json" -OutputInstaller "sentinel-installer-acme.exe"
```

### Build with Embedded Config (One Step)

```powershell
.\build.ps1 `
    -ServerUrl "https://sentinelrmm.us" `
    -GrpcEndpoint "sentinelrmm.us:4444" `
    -EnrollmentToken "your-token" `
    -OrganizationId "org-uuid" `
    -OutputName "sentinel-installer-acme.exe"
```

## Installation Modes

### Interactive Installation

Double-click the installer or run:

```cmd
sentinel-installer.exe
```

Shows a wizard: Welcome -> License -> Progress -> Complete

### Silent Installation

For automated deployment:

```cmd
sentinel-installer.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

Additional silent options:
- `/LOG="C:\path\to\install.log"` - Write install log
- `/DIR="C:\CustomPath"` - Custom install directory
- `/NOICONS` - Don't create Start Menu shortcuts

### Upgrade

Running the installer on a system with Sentinel already installed will:
1. Stop existing services
2. Backup existing config (if no new config embedded)
3. Replace binaries
4. Recreate services
5. Start services

Existing config is preserved during upgrades unless new config is embedded.

## Uninstallation

### Via Control Panel

Settings -> Apps -> Sentinel Agent -> Uninstall

### Silent Uninstall

```cmd
"C:\Program Files\Sentinel\unins000.exe" /VERYSILENT /SUPPRESSMSGBOXES
```

## Config Format

The embedded configuration JSON format:

```json
{
  "server_url": "https://sentinelrmm.us",
  "grpc_endpoint": "sentinelrmm.us:4444",
  "enrollment_token": "enrollment-token-here",
  "organization_id": "organization-uuid-here"
}
```

This is written to `C:\Program Files\Sentinel\config.json` during installation.

## Logging

Installation logs are written to:
```
%TEMP%\Sentinel\install-{reference-id}.log
```

The reference ID format is: `INS-HHMMSS-YYYYMMDD`

Example: `INS-143052-20260210`

Use this reference ID when contacting support.

## Error Handling

If installation fails, a popup shows:
- Clear error message
- Reference ID
- Log file path

The reference ID helps support trace the exact issue in logs.

## Service Configuration

Two Windows services are created:

| Service | Name | Description |
|---------|------|-------------|
| SentinelAgent | Sentinel Agent | Main monitoring agent |
| SentinelWatchdog | Sentinel Watchdog | Keeps agent running |

Both services:
- Start automatically on boot
- Restart on failure (after 5s, 10s, then 30s delays)
- Run as Local System

## File Structure

```
C:\Program Files\Sentinel\
├── sentinel-agent.exe     # Main agent
├── sentinel-watchdog.exe  # Watchdog
├── config.json            # Configuration
├── logs\                  # Log directory
└── unins000.exe           # Uninstaller
```

## Registry Entries

```
HKLM\SOFTWARE\Sentinel
├── InstallPath = C:\Program Files\Sentinel
└── Version = 1.72.0
```

## Customization

### Custom Icon

Replace `resources/sentinel.ico` with your icon (256x256 recommended).

### Custom Wizard Images

Create and reference in `sentinel-setup.iss`:
- `resources/wizard-large.bmp` (164x314 pixels)
- `resources/wizard-small.bmp` (55x58 pixels)

### License Text

Edit `resources/license.rtf` with your license agreement.

## Troubleshooting

### "Inno Setup not found"

Install Inno Setup 6 from https://jrsoftware.org/isdl.php

### "Agent binary not found"

Build the agent first:
```bash
cd agent
GOOS=windows GOARCH=amd64 go build -o ../release/agent/sentinel-agent.exe ./cmd/sentinel-agent
GOOS=windows GOARCH=amd64 go build -o ../release/agent/sentinel-watchdog.exe ./cmd/sentinel-watchdog
```

### "Service failed to start"

Check:
1. Config file exists and is valid JSON
2. Server is reachable
3. Windows Event Viewer for service errors

### "Template already has embedded config"

Use the clean template file (`sentinel-installer-template.exe`), not a previously configured installer.
