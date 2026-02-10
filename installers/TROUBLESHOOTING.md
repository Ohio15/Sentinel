# Sentinel Agent Troubleshooting Guide

This guide covers common installation, configuration, and runtime issues across all platforms.

---

## Table of Contents

1. [Quick Diagnostics](#quick-diagnostics)
2. [Windows Issues](#windows-issues)
3. [Linux Issues](#linux-issues)
4. [macOS Issues](#macos-issues)
5. [Network Issues](#network-issues)
6. [Service Issues](#service-issues)
7. [Log Analysis](#log-analysis)
8. [Recovery Procedures](#recovery-procedures)

---

## Quick Diagnostics

### Check Installation Status

**Windows (PowerShell as Administrator):**
```powershell
# Check services
Get-Service SentinelAgent, SentinelWatchdog | Format-Table Name, Status, StartType

# Check installation path
Test-Path "C:\Program Files\Sentinel\sentinel-agent.exe"

# Check version
& "C:\Program Files\Sentinel\sentinel-agent.exe" --version
```

**Linux:**
```bash
# Check systemd services
systemctl status sentinel-agent sentinel-watchdog

# Check installation
ls -la /opt/sentinel/

# Check version
/opt/sentinel/sentinel-agent --version
```

**macOS:**
```bash
# Check launchd daemons
sudo launchctl list | grep sentinel

# Check installation
ls -la /usr/local/bin/sentinel-*

# Check version
/usr/local/bin/sentinel-agent --version
```

### Quick Health Check

```bash
# Test connectivity to server
curl -I https://your-server.com:4443/health

# Check agent process
# Windows: tasklist /FI "IMAGENAME eq sentinel-agent.exe"
# Linux/macOS: pgrep -a sentinel
```

---

## Windows Issues

### W1: Installation Requires Administrator

**Symptom:** Installer fails with "Administrator privileges required"

**Solution:**
1. Right-click the installer
2. Select "Run as administrator"
3. Or from elevated PowerShell: `Start-Process .\installer.exe -Verb RunAs`

### W2: Service Creation Fails

**Symptom:** Error E201 - Failed to create service

**Diagnosis:**
```powershell
# Check for existing service
sc.exe query SentinelAgent

# Check Event Viewer
Get-EventLog -LogName System -Source "Service Control Manager" -Newest 10
```

**Solutions:**
1. Remove existing service:
   ```powershell
   sc.exe delete SentinelAgent
   sc.exe delete SentinelWatchdog
   ```
2. Reboot and reinstall

### W3: Service Won't Start

**Symptom:** Service created but fails to start

**Diagnosis:**
```powershell
# Check service status
sc.exe query SentinelAgent
sc.exe qc SentinelAgent

# Check Windows Event Log
Get-EventLog -LogName Application -Source "SentinelAgent" -Newest 20
```

**Common Causes:**
- Missing Visual C++ Redistributable
- Config file missing or invalid
- Binary path incorrect

**Solutions:**
1. Install VC++ Redistributable 2019+
2. Verify config.json exists in installation folder
3. Check binary path in service configuration

### W4: Antivirus Blocking

**Symptom:** Installation or agent blocked by antivirus

**Solutions:**
1. Add exclusions for:
   - `C:\Program Files\Sentinel\`
   - `sentinel-agent.exe`
   - `sentinel-watchdog.exe`
2. Temporarily disable real-time protection during install
3. Submit binary for whitelisting with AV vendor

### W5: Windows Firewall Blocking

**Symptom:** Agent cannot connect to server

**Solution - Add firewall rules:**
```powershell
# Outbound rule for agent
New-NetFirewallRule -DisplayName "Sentinel Agent" -Direction Outbound `
    -Program "C:\Program Files\Sentinel\sentinel-agent.exe" -Action Allow

# Or allow the port
New-NetFirewallRule -DisplayName "Sentinel HTTPS" -Direction Outbound `
    -Protocol TCP -RemotePort 443,4443 -Action Allow
```

### W6: Previous Installation Remnants

**Symptom:** Installation fails due to leftover files

**Full Cleanup Procedure:**
```powershell
# Stop and remove services
sc.exe stop SentinelAgent
sc.exe stop SentinelWatchdog
sc.exe delete SentinelAgent
sc.exe delete SentinelWatchdog

# Kill processes
taskkill /F /IM sentinel-agent.exe 2>$null
taskkill /F /IM sentinel-watchdog.exe 2>$null

# Remove files
Remove-Item -Recurse -Force "C:\Program Files\Sentinel" -ErrorAction SilentlyContinue

# Remove registry entries
Remove-Item -Path "HKLM:\SOFTWARE\Sentinel" -Recurse -ErrorAction SilentlyContinue

# Reboot recommended
```

---

## Linux Issues

### L1: Permission Denied

**Symptom:** Installation fails with permission errors

**Solution:**
```bash
# Run with sudo
sudo ./install.sh --server=https://... --token=...

# Or fix ownership after extraction
sudo chown -R root:root /opt/sentinel
sudo chmod 755 /opt/sentinel/sentinel-agent
```

### L2: systemd Service Fails

**Symptom:** Service fails to start

**Diagnosis:**
```bash
# Check service status
sudo systemctl status sentinel-agent -l

# View full logs
sudo journalctl -u sentinel-agent -n 100 --no-pager

# Check binary
file /opt/sentinel/sentinel-agent
ldd /opt/sentinel/sentinel-agent
```

**Common Fixes:**
```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable and start
sudo systemctl enable sentinel-agent
sudo systemctl start sentinel-agent

# Check for missing libraries
ldd /opt/sentinel/sentinel-agent | grep "not found"
```

### L3: SELinux Blocking

**Symptom:** Service blocked by SELinux

**Diagnosis:**
```bash
# Check SELinux status
getenforce

# Check audit log
sudo ausearch -m avc -ts recent
```

**Solutions:**
```bash
# Option 1: Create custom policy
sudo audit2allow -a -M sentinel-agent
sudo semodule -i sentinel-agent.pp

# Option 2: Set permissive for testing (not recommended for production)
sudo setenforce 0

# Option 3: Set correct context
sudo restorecon -Rv /opt/sentinel/
```

### L4: Binary Architecture Mismatch

**Symptom:** "Exec format error" or "cannot execute binary file"

**Diagnosis:**
```bash
# Check binary architecture
file /opt/sentinel/sentinel-agent

# Check system architecture
uname -m
```

**Solution:**
Download the correct binary for your architecture:
- `amd64` for x86_64
- `arm64` for aarch64
- `arm` for armv7l

### L5: AppArmor Blocking (Ubuntu)

**Symptom:** Service blocked by AppArmor

**Diagnosis:**
```bash
# Check AppArmor status
sudo aa-status

# Check logs
sudo dmesg | grep sentinel
```

**Solutions:**
```bash
# Add AppArmor profile or set to complain mode
sudo aa-complain /opt/sentinel/sentinel-agent
```

---

## macOS Issues

### M1: SIP Blocking Installation

**Symptom:** Installation blocked by System Integrity Protection

**Note:** Standard installations to `/usr/local/` should not be affected.

**If installing to protected location:**
1. Boot to Recovery Mode (Cmd+R at startup)
2. Disable SIP: `csrutil disable`
3. Install, then re-enable SIP: `csrutil enable`

### M2: launchd Service Issues

**Symptom:** Service fails to load

**Diagnosis:**
```bash
# Check if loaded
sudo launchctl list | grep sentinel

# Check plist
sudo launchctl load -w /Library/LaunchDaemons/com.sentinel.agent.plist 2>&1

# Check logs
sudo log show --predicate 'processImagePath CONTAINS "sentinel"' --last 1h
```

**Common Fixes:**
```bash
# Fix plist permissions
sudo chmod 644 /Library/LaunchDaemons/com.sentinel.*.plist
sudo chown root:wheel /Library/LaunchDaemons/com.sentinel.*.plist

# Reload daemon
sudo launchctl unload /Library/LaunchDaemons/com.sentinel.agent.plist
sudo launchctl load -w /Library/LaunchDaemons/com.sentinel.agent.plist
```

### M3: Gatekeeper Blocking

**Symptom:** "App cannot be opened because it is from an unidentified developer"

**Solutions:**
```bash
# Remove quarantine attribute
sudo xattr -rd com.apple.quarantine /usr/local/bin/sentinel-*

# Or allow in Security & Privacy preferences
```

### M4: Code Signing Issues

**Symptom:** Binary signature validation fails

**Diagnosis:**
```bash
# Check signature
codesign -dv --verbose=4 /usr/local/bin/sentinel-agent
```

**Solution:**
For unsigned binaries, authorize in System Preferences > Security & Privacy

### M5: Keychain Access Issues

**Symptom:** Agent cannot access stored credentials

**Solution:**
Ensure the agent has proper Keychain Access permissions in Security preferences.

---

## Network Issues

### N1: Cannot Reach Server

**Symptom:** Connection refused or timeout

**Diagnosis:**
```bash
# Test connectivity
curl -v https://server.example.com:4443/health

# Check DNS
nslookup server.example.com

# Check route
traceroute server.example.com

# Check open ports
nc -zv server.example.com 4443
```

### N2: SSL/TLS Certificate Errors

**Symptom:** Certificate validation failure

**Diagnosis:**
```bash
# Check certificate
openssl s_client -connect server.example.com:4443 -servername server.example.com

# Check certificate chain
openssl s_client -connect server.example.com:4443 -showcerts
```

**Common Fixes:**
1. Ensure system time is correct
2. Update CA certificates:
   - Ubuntu: `sudo update-ca-certificates`
   - CentOS: `sudo update-ca-trust`
   - macOS: Update via Keychain Access
3. For testing only: Use `--insecure` flag

### N3: Proxy Configuration

**Symptom:** Connection fails when behind proxy

**Solutions:**
```bash
# Set environment variables
export HTTP_PROXY=http://proxy.example.com:8080
export HTTPS_PROXY=http://proxy.example.com:8080
export NO_PROXY=localhost,127.0.0.1

# Or configure in agent config
{
  "proxy_url": "http://proxy.example.com:8080",
  "proxy_auth": "username:password"
}
```

### N4: Token Issues

**Symptom:** Enrollment fails with 401/403 error

**Causes:**
- Token expired
- Token disabled
- Token max uses exceeded
- Token for different organization

**Solution:**
Generate a new enrollment token from the Sentinel dashboard.

---

## Service Issues

### S1: Service Crashes Immediately

**Symptom:** Service starts then stops within seconds

**Diagnosis:**
1. Check exit code in logs
2. Try running binary directly:
   ```bash
   /opt/sentinel/sentinel-agent --server=https://... 2>&1
   ```
3. Check for core dumps

### S2: High CPU Usage

**Symptom:** Agent consuming excessive CPU

**Diagnosis:**
```bash
# Check what's happening
strace -p $(pgrep sentinel-agent) -c

# Profile
top -p $(pgrep sentinel-agent)
```

**Common Causes:**
- Polling interval too frequent
- Large inventory collection
- Network retry loops

### S3: High Memory Usage

**Symptom:** Agent using too much memory

**Diagnosis:**
```bash
# Check memory
ps aux | grep sentinel
pmap $(pgrep sentinel-agent)
```

**Solutions:**
- Check for memory leaks (upgrade to latest version)
- Adjust collection intervals
- Limit concurrent operations

### S4: Watchdog Keeps Restarting Agent

**Symptom:** Agent restarts frequently

**Diagnosis:**
Check watchdog logs for restart reasons:
```bash
# Linux
journalctl -u sentinel-watchdog

# Windows
Get-EventLog -LogName Application -Source "SentinelWatchdog" -Newest 50
```

---

## Log Analysis

### Finding Logs

**Windows:**
```
C:\Program Files\Sentinel\logs\agent.log
C:\Program Files\Sentinel\logs\watchdog.log
%TEMP%\Sentinel\install-*.log
```

**Linux:**
```
/var/log/sentinel/agent.log
/var/log/sentinel/watchdog.log
journalctl -u sentinel-agent
```

**macOS:**
```
/var/log/sentinel/agent.log
/var/log/sentinel-install.log
sudo log show --predicate 'processImagePath CONTAINS "sentinel"'
```

### Common Log Patterns

```
# Successful connection
INFO Connected to server successfully

# Authentication failure
ERROR Authentication failed: invalid token

# Network issue
ERROR Failed to connect: connection refused
WARN Retrying connection in 30s

# Configuration error
ERROR Failed to parse config: unexpected end of JSON input
```

### Enabling Debug Logging

Add to config.json:
```json
{
  "log_level": "debug"
}
```

Or use environment variable:
```bash
SENTINEL_LOG_LEVEL=debug /opt/sentinel/sentinel-agent
```

---

## Recovery Procedures

### Complete Reinstall

**Windows:**
```powershell
# 1. Full uninstall
& "C:\Program Files\Sentinel\unins000.exe" /SILENT

# 2. Cleanup
sc.exe delete SentinelAgent
sc.exe delete SentinelWatchdog
Remove-Item -Recurse -Force "C:\Program Files\Sentinel" -ErrorAction SilentlyContinue

# 3. Reboot
Restart-Computer

# 4. Fresh install
.\SentinelAgent-Setup.exe
```

**Linux:**
```bash
# 1. Stop services
sudo systemctl stop sentinel-agent sentinel-watchdog

# 2. Remove services
sudo systemctl disable sentinel-agent sentinel-watchdog
sudo rm /etc/systemd/system/sentinel-*.service

# 3. Remove files
sudo rm -rf /opt/sentinel
sudo rm -rf /etc/sentinel
sudo rm -f /var/log/sentinel/*.log

# 4. Reload systemd
sudo systemctl daemon-reload

# 5. Fresh install
sudo ./install.sh --server=https://... --token=...
```

**macOS:**
```bash
# 1. Unload services
sudo launchctl unload /Library/LaunchDaemons/com.sentinel.*.plist

# 2. Remove files
sudo rm -f /Library/LaunchDaemons/com.sentinel.*.plist
sudo rm -f /usr/local/bin/sentinel-*
sudo rm -rf /etc/sentinel
sudo rm -f /var/log/sentinel/*.log

# 3. Fresh install
sudo installer -pkg SentinelAgent.pkg -target /
```

### Config Restore from Backup

**Windows:**
```powershell
Copy-Item "C:\Program Files\Sentinel\config.json.backup" "C:\Program Files\Sentinel\config.json"
Restart-Service SentinelAgent
```

**Linux/macOS:**
```bash
sudo cp /etc/sentinel/config.json.backup /etc/sentinel/config.json
sudo systemctl restart sentinel-agent
# or
sudo launchctl stop com.sentinel.agent && sudo launchctl start com.sentinel.agent
```

---

## Support Contact

If issues persist after following this guide:

1. Collect the **Reference ID** from the error message
2. Gather relevant **log files**
3. Note your **OS version** and **agent version**
4. Contact support with this information

**Log Collection Script:**
```bash
# Create support bundle
mkdir /tmp/sentinel-support
cp /var/log/sentinel/*.log /tmp/sentinel-support/
journalctl -u sentinel-agent > /tmp/sentinel-support/journal.log 2>/dev/null
uname -a > /tmp/sentinel-support/system-info.txt
tar czf sentinel-support-bundle.tar.gz /tmp/sentinel-support/
```
