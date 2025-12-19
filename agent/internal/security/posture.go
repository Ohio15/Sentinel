package security

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SecurityPosture represents the overall security state of a device
type SecurityPosture struct {
	CollectedAt time.Time `json:"collectedAt"`

	// Antivirus/EDR
	AntivirusProduct        string    `json:"antivirusProduct,omitempty"`
	AntivirusVersion        string    `json:"antivirusVersion,omitempty"`
	AntivirusEnabled        bool      `json:"antivirusEnabled"`
	AntivirusUpToDate       bool      `json:"antivirusUpToDate"`
	AntivirusLastScan       time.Time `json:"antivirusLastScan,omitempty"`
	AntivirusRealtimeEnabled bool     `json:"antivirusRealtimeEnabled"`

	// Firewall
	FirewallEnabled  bool              `json:"firewallEnabled"`
	FirewallProfiles map[string]bool   `json:"firewallProfiles,omitempty"` // domain, private, public

	// Encryption
	DiskEncryptionEnabled bool   `json:"diskEncryptionEnabled"`
	DiskEncryptionType    string `json:"diskEncryptionType,omitempty"` // bitlocker, filevault, luks
	DiskEncryptionPercent int    `json:"diskEncryptionPercent,omitempty"`

	// TPM
	TPMPresent bool   `json:"tpmPresent"`
	TPMVersion string `json:"tpmVersion,omitempty"`
	TPMEnabled bool   `json:"tpmEnabled"`

	// Boot Security
	SecureBootEnabled bool `json:"secureBootEnabled"`

	// OS Security
	UACEnabled              bool `json:"uacEnabled"`
	UACLevel                int  `json:"uacLevel,omitempty"`
	ScreenLockEnabled       bool `json:"screenLockEnabled"`
	ScreenLockTimeout       int  `json:"screenLockTimeout,omitempty"` // seconds

	// Risk Factors
	RemoteDesktopEnabled  bool `json:"remoteDesktopEnabled"`
	GuestAccountEnabled   bool `json:"guestAccountEnabled"`
	AutoLoginEnabled      bool `json:"autoLoginEnabled"`
	DeveloperModeEnabled  bool `json:"developerModeEnabled"`

	// Calculated score
	SecurityScore int      `json:"securityScore"` // 0-100
	RiskFactors   []string `json:"riskFactors,omitempty"`
}

// PostureCollector collects security posture information
type PostureCollector struct {
	timeout time.Duration
}

// NewPostureCollector creates a new security posture collector
func NewPostureCollector() *PostureCollector {
	return &PostureCollector{
		timeout: 60 * time.Second,
	}
}

// Collect gathers security posture information
func (c *PostureCollector) Collect(ctx context.Context) (*SecurityPosture, error) {
	posture := &SecurityPosture{
		CollectedAt:      time.Now(),
		FirewallProfiles: make(map[string]bool),
	}

	switch runtime.GOOS {
	case "windows":
		c.collectWindows(ctx, posture)
	case "darwin":
		c.collectMacOS(ctx, posture)
	case "linux":
		c.collectLinux(ctx, posture)
	}

	// Calculate security score
	posture.SecurityScore, posture.RiskFactors = c.calculateScore(posture)

	return posture, nil
}

// collectWindows collects Windows security posture
func (c *PostureCollector) collectWindows(ctx context.Context, posture *SecurityPosture) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Collect antivirus status via Windows Security Center
	c.collectWindowsAntivirus(ctx, posture)

	// Collect firewall status
	c.collectWindowsFirewall(ctx, posture)

	// Collect BitLocker status
	c.collectWindowsBitLocker(ctx, posture)

	// Collect TPM status
	c.collectWindowsTPM(ctx, posture)

	// Collect UAC status
	c.collectWindowsUAC(ctx, posture)

	// Collect Secure Boot status
	c.collectWindowsSecureBoot(ctx, posture)

	// Collect other security settings
	c.collectWindowsSecuritySettings(ctx, posture)
}

// collectWindowsAntivirus gets antivirus status from Windows Security Center
func (c *PostureCollector) collectWindowsAntivirus(ctx context.Context, posture *SecurityPosture) {
	psScript := `
$av = Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntivirusProduct -ErrorAction SilentlyContinue | Select-Object -First 1
if ($av) {
    $state = $av.productState
    $enabled = (($state -band 0x1000) -ne 0)
    $upToDate = (($state -band 0x10) -eq 0)
    [PSCustomObject]@{
        Name = $av.displayName
        Enabled = $enabled
        UpToDate = $upToDate
    } | ConvertTo-Json
}
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "Name") {
				posture.AntivirusProduct = extractValue(line, "Name")
			}
			if strings.Contains(line, `"Enabled": true`) || strings.Contains(line, `"Enabled":true`) {
				posture.AntivirusEnabled = true
			}
			if strings.Contains(line, `"UpToDate": true`) || strings.Contains(line, `"UpToDate":true`) {
				posture.AntivirusUpToDate = true
			}
		}
	}

	// Check Windows Defender specifically
	defenderScript := `
$mpStatus = Get-MpComputerStatus -ErrorAction SilentlyContinue
if ($mpStatus) {
    [PSCustomObject]@{
        AntivirusEnabled = $mpStatus.AntivirusEnabled
        RealTimeProtectionEnabled = $mpStatus.RealTimeProtectionEnabled
        AntivirusSignatureAge = $mpStatus.AntivirusSignatureAge
        FullScanAge = $mpStatus.FullScanAge
    } | ConvertTo-Json
}
`

	cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", defenderScript)
	output, err = cmd.Output()
	if err == nil {
		outputStr := string(output)
		if strings.Contains(outputStr, `"AntivirusEnabled": true`) || strings.Contains(outputStr, `"AntivirusEnabled":true`) {
			posture.AntivirusEnabled = true
			if posture.AntivirusProduct == "" {
				posture.AntivirusProduct = "Windows Defender"
			}
		}
		if strings.Contains(outputStr, `"RealTimeProtectionEnabled": true`) || strings.Contains(outputStr, `"RealTimeProtectionEnabled":true`) {
			posture.AntivirusRealtimeEnabled = true
		}
	}
}

// collectWindowsFirewall gets Windows firewall status
func (c *PostureCollector) collectWindowsFirewall(ctx context.Context, posture *SecurityPosture) {
	psScript := `
Get-NetFirewallProfile | ForEach-Object {
    [PSCustomObject]@{
        Name = $_.Name
        Enabled = $_.Enabled
    }
} | ConvertTo-Json
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)

		// Parse profiles
		if strings.Contains(outputStr, `"Domain"`) || strings.Contains(outputStr, `"Name": "Domain"`) {
			posture.FirewallProfiles["domain"] = strings.Contains(outputStr, `"Enabled": true`) ||
				strings.Contains(outputStr, `"Enabled":true`)
		}

		// Simple check - if any profile mentions Enabled: true
		if strings.Contains(outputStr, `"Enabled": true`) || strings.Contains(outputStr, `"Enabled":true`) {
			posture.FirewallEnabled = true
		}

		// Check specific profiles
		lines := strings.Split(outputStr, "}")
		for _, block := range lines {
			if strings.Contains(block, "Domain") {
				posture.FirewallProfiles["domain"] = strings.Contains(block, `"Enabled": true`) ||
					strings.Contains(block, `"Enabled":true`) ||
					strings.Contains(block, "Enabled\": true") ||
					strings.Contains(block, "Enabled\":true")
			}
			if strings.Contains(block, "Private") {
				posture.FirewallProfiles["private"] = strings.Contains(block, `"Enabled": true`) ||
					strings.Contains(block, `"Enabled":true`) ||
					strings.Contains(block, "Enabled\": true") ||
					strings.Contains(block, "Enabled\":true")
			}
			if strings.Contains(block, "Public") {
				posture.FirewallProfiles["public"] = strings.Contains(block, `"Enabled": true`) ||
					strings.Contains(block, `"Enabled":true`) ||
					strings.Contains(block, "Enabled\": true") ||
					strings.Contains(block, "Enabled\":true")
			}
		}
	}
}

// collectWindowsBitLocker gets BitLocker encryption status
func (c *PostureCollector) collectWindowsBitLocker(ctx context.Context, posture *SecurityPosture) {
	psScript := `
$bl = Get-BitLockerVolume -MountPoint C: -ErrorAction SilentlyContinue
if ($bl) {
    [PSCustomObject]@{
        ProtectionStatus = $bl.ProtectionStatus.ToString()
        EncryptionPercentage = $bl.EncryptionPercentage
        VolumeStatus = $bl.VolumeStatus.ToString()
    } | ConvertTo-Json
}
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "FullyEncrypted") || strings.Contains(outputStr, `"ProtectionStatus": "On"`) {
			posture.DiskEncryptionEnabled = true
			posture.DiskEncryptionType = "bitlocker"
		}

		// Extract percentage
		if idx := strings.Index(outputStr, "EncryptionPercentage"); idx != -1 {
			rest := outputStr[idx:]
			if colonIdx := strings.Index(rest, ":"); colonIdx != -1 {
				valueStr := strings.TrimSpace(rest[colonIdx+1:])
				if commaIdx := strings.Index(valueStr, ","); commaIdx != -1 {
					valueStr = valueStr[:commaIdx]
				}
				if pct, err := strconv.Atoi(strings.TrimSpace(valueStr)); err == nil {
					posture.DiskEncryptionPercent = pct
				}
			}
		}
	}
}

// collectWindowsTPM gets TPM status
func (c *PostureCollector) collectWindowsTPM(ctx context.Context, posture *SecurityPosture) {
	psScript := `
$tpm = Get-Tpm -ErrorAction SilentlyContinue
if ($tpm) {
    [PSCustomObject]@{
        TpmPresent = $tpm.TpmPresent
        TpmEnabled = $tpm.TpmEnabled
        TpmActivated = $tpm.TpmActivated
    } | ConvertTo-Json
}
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		posture.TPMPresent = strings.Contains(outputStr, `"TpmPresent": true`) || strings.Contains(outputStr, `"TpmPresent":true`)
		posture.TPMEnabled = strings.Contains(outputStr, `"TpmEnabled": true`) || strings.Contains(outputStr, `"TpmEnabled":true`)
	}

	// Get TPM version
	versionScript := `(Get-WmiObject -Namespace root/cimv2/security/microsofttpm -Class Win32_Tpm -ErrorAction SilentlyContinue).SpecVersion`
	cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", versionScript)
	output, err = cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
		if version != "" {
			if strings.HasPrefix(version, "2.") {
				posture.TPMVersion = "2.0"
			} else if strings.HasPrefix(version, "1.") {
				posture.TPMVersion = "1.2"
			} else {
				posture.TPMVersion = version
			}
		}
	}
}

// collectWindowsUAC gets UAC status
func (c *PostureCollector) collectWindowsUAC(ctx context.Context, posture *SecurityPosture) {
	cmd := exec.CommandContext(ctx, "reg", "query",
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`,
		"/v", "EnableLUA")
	output, err := cmd.Output()
	if err == nil {
		posture.UACEnabled = strings.Contains(string(output), "0x1")
	}

	// Get UAC level
	cmd = exec.CommandContext(ctx, "reg", "query",
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`,
		"/v", "ConsentPromptBehaviorAdmin")
	output, err = cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "0x5") {
			posture.UACLevel = 3 // Max
		} else if strings.Contains(string(output), "0x2") {
			posture.UACLevel = 2 // Default
		} else {
			posture.UACLevel = 1 // Min
		}
	}
}

// collectWindowsSecureBoot gets Secure Boot status
func (c *PostureCollector) collectWindowsSecureBoot(ctx context.Context, posture *SecurityPosture) {
	psScript := `Confirm-SecureBootUEFI -ErrorAction SilentlyContinue`
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err == nil {
		posture.SecureBootEnabled = strings.TrimSpace(string(output)) == "True"
	}
}

// collectWindowsSecuritySettings gets additional security settings
func (c *PostureCollector) collectWindowsSecuritySettings(ctx context.Context, posture *SecurityPosture) {
	// Check RDP
	cmd := exec.CommandContext(ctx, "reg", "query",
		`HKLM\SYSTEM\CurrentControlSet\Control\Terminal Server`,
		"/v", "fDenyTSConnections")
	output, err := cmd.Output()
	if err == nil {
		posture.RemoteDesktopEnabled = strings.Contains(string(output), "0x0")
	}

	// Check Guest account
	psScript := `(Get-LocalUser -Name Guest -ErrorAction SilentlyContinue).Enabled`
	cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err = cmd.Output()
	if err == nil {
		posture.GuestAccountEnabled = strings.TrimSpace(string(output)) == "True"
	}

	// Check auto-login
	cmd = exec.CommandContext(ctx, "reg", "query",
		`HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon`,
		"/v", "AutoAdminLogon")
	output, err = cmd.Output()
	if err == nil {
		posture.AutoLoginEnabled = strings.Contains(string(output), "1")
	}

	// Check screen lock
	psScript = `
$power = powercfg /query SCHEME_CURRENT SUB_VIDEO VIDEOIDLE 2>$null
if ($power -match 'Current AC Power Setting Index:\s*0x([0-9a-fA-F]+)') {
    [int]"0x$($Matches[1])"
}
`
	cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err = cmd.Output()
	if err == nil {
		if timeout, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			posture.ScreenLockEnabled = timeout > 0
			posture.ScreenLockTimeout = timeout
		}
	}
}

// collectMacOS collects macOS security posture
func (c *PostureCollector) collectMacOS(ctx context.Context, posture *SecurityPosture) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Check FileVault
	cmd := exec.CommandContext(ctx, "fdesetup", "status")
	output, err := cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "FileVault is On") {
			posture.DiskEncryptionEnabled = true
			posture.DiskEncryptionType = "filevault"
			posture.DiskEncryptionPercent = 100
		}
	}

	// Check firewall
	cmd = exec.CommandContext(ctx, "defaults", "read", "/Library/Preferences/com.apple.alf", "globalstate")
	output, err = cmd.Output()
	if err == nil {
		state := strings.TrimSpace(string(output))
		posture.FirewallEnabled = state == "1" || state == "2"
	}

	// Check Gatekeeper
	cmd = exec.CommandContext(ctx, "spctl", "--status")
	output, err = cmd.Output()
	if err == nil {
		posture.UACEnabled = strings.Contains(string(output), "enabled")
	}

	// Check SIP (System Integrity Protection)
	cmd = exec.CommandContext(ctx, "csrutil", "status")
	output, err = cmd.Output()
	if err == nil {
		posture.SecureBootEnabled = strings.Contains(string(output), "enabled")
	}

	// Check screen saver lock
	cmd = exec.CommandContext(ctx, "defaults", "read", "com.apple.screensaver", "askForPassword")
	output, err = cmd.Output()
	if err == nil {
		posture.ScreenLockEnabled = strings.TrimSpace(string(output)) == "1"
	}

	// Check for XProtect (Apple's built-in antimalware)
	cmd = exec.CommandContext(ctx, "system_profiler", "SPInstallHistoryDataType")
	output, err = cmd.Output()
	if err == nil && strings.Contains(string(output), "XProtect") {
		posture.AntivirusProduct = "XProtect"
		posture.AntivirusEnabled = true
	}

	// Check Remote Login (SSH)
	cmd = exec.CommandContext(ctx, "systemsetup", "-getremotelogin")
	output, err = cmd.Output()
	if err == nil {
		posture.RemoteDesktopEnabled = strings.Contains(strings.ToLower(string(output)), "on")
	}

	// Check Guest account
	cmd = exec.CommandContext(ctx, "dscl", ".", "-read", "/Users/Guest")
	if err := cmd.Run(); err == nil {
		posture.GuestAccountEnabled = true
	}
}

// collectLinux collects Linux security posture
func (c *PostureCollector) collectLinux(ctx context.Context, posture *SecurityPosture) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Check LUKS encryption
	cmd := exec.CommandContext(ctx, "lsblk", "-o", "NAME,TYPE,FSTYPE")
	output, err := cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "crypto_LUKS") || strings.Contains(string(output), "luks") {
			posture.DiskEncryptionEnabled = true
			posture.DiskEncryptionType = "luks"
		}
	}

	// Check firewall (ufw or firewalld)
	if _, err := exec.LookPath("ufw"); err == nil {
		cmd = exec.CommandContext(ctx, "ufw", "status")
		output, err = cmd.Output()
		if err == nil {
			posture.FirewallEnabled = strings.Contains(string(output), "active")
		}
	} else if _, err := exec.LookPath("firewall-cmd"); err == nil {
		cmd = exec.CommandContext(ctx, "firewall-cmd", "--state")
		output, err = cmd.Output()
		if err == nil {
			posture.FirewallEnabled = strings.Contains(string(output), "running")
		}
	} else {
		// Check iptables
		cmd = exec.CommandContext(ctx, "iptables", "-L", "-n")
		output, err = cmd.Output()
		if err == nil {
			// If there are rules beyond default
			lines := strings.Split(string(output), "\n")
			posture.FirewallEnabled = len(lines) > 8
		}
	}

	// Check Secure Boot
	cmd = exec.CommandContext(ctx, "mokutil", "--sb-state")
	output, err = cmd.Output()
	if err == nil {
		posture.SecureBootEnabled = strings.Contains(string(output), "SecureBoot enabled")
	}

	// Check SELinux/AppArmor
	cmd = exec.CommandContext(ctx, "getenforce")
	output, err = cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "Enforcing") {
			posture.UACEnabled = true
			posture.UACLevel = 3
		} else if strings.Contains(string(output), "Permissive") {
			posture.UACEnabled = true
			posture.UACLevel = 1
		}
	} else {
		// Check AppArmor
		cmd = exec.CommandContext(ctx, "aa-status", "--enabled")
		if err := cmd.Run(); err == nil {
			posture.UACEnabled = true
		}
	}

	// Check ClamAV
	cmd = exec.CommandContext(ctx, "systemctl", "is-active", "clamav-daemon")
	output, err = cmd.Output()
	if err == nil && strings.Contains(string(output), "active") {
		posture.AntivirusProduct = "ClamAV"
		posture.AntivirusEnabled = true
	}

	// Check SSH
	cmd = exec.CommandContext(ctx, "systemctl", "is-active", "sshd")
	output, err = cmd.Output()
	if err == nil {
		posture.RemoteDesktopEnabled = strings.Contains(string(output), "active")
	}

	// Check screen lock (GNOME)
	cmd = exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.screensaver", "lock-enabled")
	output, err = cmd.Output()
	if err == nil {
		posture.ScreenLockEnabled = strings.Contains(string(output), "true")
	}
}

// calculateScore calculates the security score based on posture
func (c *PostureCollector) calculateScore(posture *SecurityPosture) (int, []string) {
	score := 100
	var risks []string

	// Antivirus checks (-20 if disabled)
	if !posture.AntivirusEnabled {
		score -= 20
		risks = append(risks, "Antivirus is disabled")
	}
	if !posture.AntivirusUpToDate && posture.AntivirusEnabled {
		score -= 10
		risks = append(risks, "Antivirus signatures are outdated")
	}
	if !posture.AntivirusRealtimeEnabled && posture.AntivirusEnabled {
		score -= 5
		risks = append(risks, "Real-time protection is disabled")
	}

	// Firewall (-15 if disabled)
	if !posture.FirewallEnabled {
		score -= 15
		risks = append(risks, "Firewall is disabled")
	}

	// Encryption (-15 if disabled)
	if !posture.DiskEncryptionEnabled {
		score -= 15
		risks = append(risks, "Disk encryption is not enabled")
	}

	// Secure Boot (-5 if disabled)
	if !posture.SecureBootEnabled {
		score -= 5
		risks = append(risks, "Secure Boot is disabled")
	}

	// UAC/Gatekeeper (-5 if disabled)
	if !posture.UACEnabled {
		score -= 5
		risks = append(risks, "UAC/Gatekeeper is disabled")
	}

	// Screen lock (-5 if disabled)
	if !posture.ScreenLockEnabled {
		score -= 5
		risks = append(risks, "Screen lock is disabled")
	}

	// Risk factors
	if posture.GuestAccountEnabled {
		score -= 5
		risks = append(risks, "Guest account is enabled")
	}
	if posture.AutoLoginEnabled {
		score -= 10
		risks = append(risks, "Auto-login is enabled")
	}
	if posture.RemoteDesktopEnabled {
		score -= 5
		risks = append(risks, "Remote desktop is enabled")
	}
	if posture.DeveloperModeEnabled {
		score -= 5
		risks = append(risks, "Developer mode is enabled")
	}

	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}

	return score, risks
}

func extractValue(line, key string) string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `",`)
	return value
}
