package executor

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// MaxCommandLength is the maximum allowed length for a command
	MaxCommandLength = 10000
	// MaxScriptSize is the maximum allowed size for a script in bytes
	MaxScriptSize = 1024 * 1024 // 1MB
)

// CommandValidator validates commands before execution
type CommandValidator struct {
	whitelistedCommands map[string]bool
	blacklistedPatterns []*regexp.Regexp
	dangerousPatterns   []*regexp.Regexp
}

// NewCommandValidator creates a new command validator
func NewCommandValidator() *CommandValidator {
	return &CommandValidator{
		whitelistedCommands: getWhitelistedCommands(),
		blacklistedPatterns: getBlacklistedPatterns(),
		dangerousPatterns:   getDangerousPatterns(),
	}
}

// getWhitelistedCommands returns a map of allowed commands
func getWhitelistedCommands() map[string]bool {
	commands := []string{
		// Windows System Info
		"systeminfo", "hostname", "whoami", "ver", "date", "time",
		"set", "echo", "type", "more", "find", "findstr", "reg",

		// Windows Process Management
		"tasklist", "taskkill", "wmic",

		// Windows Service Management
		"sc", "sc.exe", "net",

		// Windows Network
		"ipconfig", "netstat", "ping", "tracert", "nslookup", "route",
		"arp", "netsh",

		// Windows File Operations
		"dir", "tree", "where", "attrib", "move", "copy",

		// Windows Disk Info
		"diskpart", "fsutil", "chkdsk",

		// PowerShell Cmdlets
		"Get-Process", "Get-Service", "Get-NetAdapter", "Get-NetIPConfiguration",
		"Get-NetIPAddress", "Get-DnsClientServerAddress", "Get-ComputerInfo",
		"Get-ChildItem", "Get-Item", "Get-Content", "Get-Location",
		"Get-WmiObject", "Get-CimInstance", "Get-EventLog", "Get-HotFix",
		"Get-LocalUser", "Get-LocalGroup", "Get-Volume", "Get-Disk",
		"Get-Partition", "Get-PhysicalDisk", "Test-Connection",
		"Test-NetConnection", "Resolve-DnsName", "Get-FileHash",
		"Get-Date", "Get-Host", "Get-ExecutionPolicy", "Get-PackageProvider",
		"Get-Package", "Get-Module", "Get-Command", "Get-Help",
		"Get-WindowsFeature", "Get-ScheduledTask", "Get-NetFirewallRule",
		"Get-NetFirewallProfile", "Get-SmbShare", "Get-SmbMapping",
		"Select-Object", "Where-Object", "Sort-Object", "Format-Table",
		"Format-List", "Measure-Object", "Group-Object", "Out-File",
		"Out-String", "Write-Output", "Write-Host",

		// Linux/Unix System Info
		"uname", "uptime", "w", "who", "id", "groups", "finger",
		"last", "lastlog", "hostnamectl", "timedatectl",

		// Linux/Unix Process Management
		"ps", "top", "htop", "pstree", "pgrep", "pidof",

		// Linux/Unix Service Management
		"systemctl", "service", "chkconfig", "update-rc.d",

		// Linux/Unix Network
		"ifconfig", "ip", "ss", "netcat", "nc", "curl", "wget",
		"dig", "host", "whois",

		// Linux/Unix File Operations (read-only)
		"ls", "cat", "head", "tail", "less", "file", "stat",
		"find", "locate", "which", "whereis", "grep", "egrep", "fgrep",

		// Linux/Unix Disk Info
		"df", "du", "mount", "lsblk", "blkid", "fdisk", "parted",

		// Linux/Unix Package Management (query only)
		"dpkg", "rpm", "yum", "apt", "apt-get", "apt-cache",
		"dnf", "zypper", "pacman",

		// Common utilities
		"pwd", "cd", "mkdir", "touch", "cp", "mv", "rm",
		"chmod", "chown", "tar", "zip", "unzip", "gzip", "gunzip",
		"awk", "sed", "cut", "sort", "uniq", "wc", "diff",
		"xargs", "tee", "tr", "expr", "basename", "dirname",
	}

	whitelist := make(map[string]bool)
	for _, cmd := range commands {
		whitelist[strings.ToLower(cmd)] = true
	}
	return whitelist
}

// getBlacklistedPatterns returns dangerous command patterns
func getBlacklistedPatterns() []*regexp.Regexp {
	patterns := []string{
		// Dangerous deletion commands
		`rm\s+-rf\s+/`,
		`del\s+/[sq]\s+[a-z]:\\`,
		`format\s+[a-z]:`,
		`mkfs\.`,

		// System modification
		`dd\s+if=.*of=/dev/`,
		`fdisk\s+/dev/`,
		`parted\s+/dev/.*rm`,

		// User/permission manipulation
		`useradd`,
		`userdel`,
		`usermod`,
		`passwd`,
		`chpasswd`,

		// Service manipulation (dangerous operations)
		`systemctl\s+(stop|disable|mask)`,
		`service\s+.*\s+stop`,

		// Firewall manipulation
		`iptables\s+-F`,
		`ufw\s+disable`,
		`netsh\s+advfirewall\s+(set|reset)`,

		// Boot/startup manipulation
		`update-grub`,
		`grub-install`,
		`bcdedit`,

		// Crypto/ransomware indicators
		`openssl\s+enc`,
		`gpg\s+-c`,
		`\.encrypt`,
		`\.decrypt`,

		// Network attacks
		`nmap\s+.*-s[STAUX]`,
		`metasploit`,
		`msfconsole`,

		// Code execution
		`eval\s*\(`,
		`exec\s*\(`,
		`system\s*\(`,

		// PowerShell dangerous operations
		`Invoke-Expression`,
		`IEX\s+`,
		`Invoke-WebRequest.*\|\s*IEX`,
		`DownloadString.*\|\s*IEX`,
		`Remove-Item\s+.*-Recurse\s+-Force`,
		`Set-ExecutionPolicy\s+Unrestricted`,
		`Disable-WindowsDefender`,
		`Stop-Service\s+.*Defender`,
		`Add-MpPreference\s+-ExclusionPath`,

		// Registry manipulation (dangerous)
		`reg\s+delete`,
		`Remove-Item.*HKLM:`,
		`Remove-ItemProperty.*HKLM:`,

		// Download and execute
		`curl.*\|\s*bash`,
		`wget.*\|\s*sh`,
		`certutil.*-decode`,
		`bitsadmin\s+/transfer`,
	}

	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		if re, err := regexp.Compile(`(?i)` + pattern); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// getDangerousPatterns returns patterns that indicate potentially dangerous operations
func getDangerousPatterns() []*regexp.Regexp {
	patterns := []string{
		// Script injection attempts
		`[;&|]\s*rm\s`,
		`[;&|]\s*del\s`,
		`[;&|]\s*format\s`,

		// Command chaining with dangerous commands
		`&&\s*rm\s`,
		`&&\s*del\s`,

		// Redirection to system files
		`>\s*/etc/`,
		`>\s*/boot/`,
		`>\s*[a-z]:\\windows\\system32`,

		// Privilege escalation attempts
		`sudo\s+-i`,
		`su\s+-`,

		// Obfuscation attempts
		`base64\s+-d`,
		`[a-zA-Z0-9+/]{100,}`,

		// Suspicious command separators (but allow || for fallback chains and && for sequential)
		`;\s*;\s*;`,  // Triple semicolons
		`\|\s*\|\s*\|`, // Triple pipes (not logical OR fallback)
	}

	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		if re, err := regexp.Compile(`(?i)` + pattern); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// ValidateCommand validates a command before execution
func ValidateCommand(command string, cmdType string) error {
	validator := NewCommandValidator()
	return validator.Validate(command, cmdType)
}

// Validate performs comprehensive command validation
func (cv *CommandValidator) Validate(command string, cmdType string) error {
	// Check command length
	if len(command) > MaxCommandLength {
		return fmt.Errorf("command exceeds maximum length of %d characters", MaxCommandLength)
	}

	// Check for empty command
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command cannot be empty")
	}

	// Check for null bytes (command injection)
	if strings.Contains(command, "\x00") {
		return fmt.Errorf("command contains null bytes")
	}

	// Validate cmdType
	validTypes := []string{"powershell", "cmd", "bash", "sh", ""}
	isValidType := false
	for _, t := range validTypes {
		if cmdType == t {
			isValidType = true
			break
		}
	}
	if !isValidType {
		return fmt.Errorf("invalid command type: %s", cmdType)
	}

	// Check against blacklisted patterns
	for _, pattern := range cv.blacklistedPatterns {
		if pattern.MatchString(command) {
			return fmt.Errorf("command contains blacklisted pattern: %s", pattern.String())
		}
	}

	// Check against dangerous patterns (warning level)
	for _, pattern := range cv.dangerousPatterns {
		if pattern.MatchString(command) {
			return fmt.Errorf("command contains potentially dangerous pattern: %s", pattern.String())
		}
	}

	// Extract ALL base commands from the full command string (including chained commands)
	baseCmds := extractAllBaseCommands(command, cmdType)

	// Every sub-command must be whitelisted; reject if any fails
	for _, baseCmd := range baseCmds {
		if baseCmd != "" {
			if !cv.whitelistedCommands[strings.ToLower(baseCmd)] {
				return fmt.Errorf("command '%s' is not in the whitelist of allowed commands", baseCmd)
			}
		}
	}

	// Additional sanitization
	if err := sanitizeArguments(command); err != nil {
		return err
	}

	return nil
}

// shellSeparatorPattern matches shell command separators: &&, ||, ;, |
// and standalone & (background operator). Order matters: longer operators
// must come before shorter ones to avoid partial matches.
// IMPORTANT: Bare & is matched only when preceded by whitespace or start-of-string
// to avoid splitting on & inside URLs (e.g., "?platform=windows&arch=amd64").
// The ; and | are always separators regardless of context.
var shellSeparatorPattern = regexp.MustCompile(`\s*(\|\||&&)\s*|(?:^|\s)(&)(?:\s|$)|\s*([;|])\s*`)

// extractAllBaseCommands splits a command string on shell separators and extracts
// the base command from each sub-command. This ensures every command in a chain
// like "tasklist && curl ... && evil.exe" is individually validated.
func extractAllBaseCommands(command string, cmdType string) []string {
	command = strings.TrimSpace(command)

	// For PowerShell, pipes are part of the pipeline (e.g., Get-Process | Where-Object).
	// PowerShell cmdlets connected by pipes are still PowerShell — validate each segment.
	if cmdType == "powershell" {
		return extractAllPowerShellCommands(command)
	}

	// Split the full command on all shell separators
	segments := shellSeparatorPattern.Split(command, -1)

	var baseCmds []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		baseCmd := extractBaseCommandSingle(seg)
		if baseCmd != "" {
			baseCmds = append(baseCmds, baseCmd)
		}
	}
	return baseCmds
}

// extractAllPowerShellCommands handles PowerShell command chains.
// PowerShell uses | for pipelines and ; for statement separation.
// && and || are also supported in PowerShell 7+.
func extractAllPowerShellCommands(command string) []string {
	// Split on PowerShell separators
	segments := shellSeparatorPattern.Split(command, -1)

	var baseCmds []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		baseCmd := extractPowerShellCmdlet(seg)
		if baseCmd != "" {
			baseCmds = append(baseCmds, baseCmd)
		}
	}
	return baseCmds
}

// extractPowerShellCmdlet extracts the cmdlet or command name from a single
// PowerShell command segment (no separators).
func extractPowerShellCmdlet(segment string) string {
	parts := strings.Fields(segment)
	for _, part := range parts {
		// Skip common PowerShell parameters
		if strings.HasPrefix(part, "-") || strings.HasPrefix(part, "$") {
			continue
		}
		// PowerShell cmdlets typically have Verb-Noun format
		if strings.Contains(part, "-") && !strings.HasPrefix(part, "-") {
			return part
		}
		// Or simple commands
		if !strings.Contains(part, "=") && !strings.Contains(part, "|") {
			return part
		}
	}
	return ""
}

// extractBaseCommandSingle extracts the base command name from a single command
// segment (no shell separators). This handles sudo prefixes, paths, and extensions.
func extractBaseCommandSingle(segment string) string {
	segment = strings.TrimSpace(segment)

	// Remove common prefixes
	segment = strings.TrimPrefix(segment, "sudo ")
	segment = strings.TrimPrefix(segment, "sudo\t")

	// Get first word
	parts := strings.Fields(segment)
	if len(parts) > 0 {
		baseCmd := parts[0]
		// CW-002: Clean path before extracting base to prevent traversal bypass
		// Paths like /usr/bin/../../evil/rm would resolve to /evil/rm
		if strings.Contains(baseCmd, "/") || strings.Contains(baseCmd, "\\") {
			cleanedPath := filepath.Clean(baseCmd)
			// Reject if path still contains traversal after cleaning
			if strings.Contains(cleanedPath, "..") {
				return "" // Invalid path, will fail whitelist check
			}
			baseCmd = filepath.Base(cleanedPath)
		}
		// Remove file extension if present
		if idx := strings.LastIndex(baseCmd, "."); idx > 0 {
			baseCmd = baseCmd[:idx]
		}
		return baseCmd
	}

	return ""
}

// extractBaseCommand extracts the base command from the FIRST segment only.
// Deprecated: Use extractAllBaseCommands for security — this only returns the first command.
// Kept for backward compatibility with tests.
func extractBaseCommand(command string, cmdType string) string {
	cmds := extractAllBaseCommands(command, cmdType)
	if len(cmds) > 0 {
		return cmds[0]
	}
	return ""
}

// sanitizeArguments performs additional argument sanitization
func sanitizeArguments(command string) error {
	// Check for excessive special characters
	specialChars := 0
	for _, ch := range command {
		if ch == ';' || ch == '|' || ch == '&' || ch == '>' || ch == '<' {
			specialChars++
		}
	}

	if specialChars > 10 {
		return fmt.Errorf("command contains excessive special characters")
	}

	// Check for suspicious unicode characters
	for _, ch := range command {
		// Block non-printable characters except common whitespace
		if ch < 32 && ch != '\t' && ch != '\n' && ch != '\r' {
			return fmt.Errorf("command contains non-printable characters")
		}

		// Block some suspicious unicode ranges
		if ch >= 0x202A && ch <= 0x202E { // BiDi override characters
			return fmt.Errorf("command contains bidirectional override characters")
		}
	}

	return nil
}

// ValidateScript validates script content before execution
func ValidateScript(script string, language string) error {
	// Check script size
	if len(script) > MaxScriptSize {
		return fmt.Errorf("script exceeds maximum size of %d bytes", MaxScriptSize)
	}

	// Check for empty script
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("script cannot be empty")
	}

	// Validate language parameter
	validLanguages := []string{"powershell", "ps1", "batch", "bat", "cmd", "bash", "python", "python3", "py"}
	isValidLanguage := false
	for _, lang := range validLanguages {
		if strings.ToLower(language) == lang {
			isValidLanguage = true
			break
		}
	}
	if !isValidLanguage {
		return fmt.Errorf("unsupported script language: %s", language)
	}

	// Check for null bytes
	if strings.Contains(script, "\x00") {
		return fmt.Errorf("script contains null bytes")
	}

	// Check against dangerous patterns
	validator := NewCommandValidator()

	for _, pattern := range validator.blacklistedPatterns {
		if pattern.MatchString(script) {
			return fmt.Errorf("script contains blacklisted pattern: %s", pattern.String())
		}
	}

	// Check for common ransomware/malware indicators
	dangerousScriptPatterns := []string{
		`\.encrypt`,
		`\.locked`,
		`ransom`,
		`bitcoin`,
		`decrypt.*payment`,
		`Remove-Item.*-Recurse.*-Force.*\\`,
		`del /s /q`,
		`format [a-z]:`,
		`cipher\s+/w`,
		// Disable security features
		`Set-MpPreference.*DisableRealtimeMonitoring`,
		`Stop-Service.*WinDefend`,
		`netsh.*firewall.*disable`,
		// Persistence mechanisms
		`schtasks.*\/create`,
		`reg.*add.*\\Run`,
		`New-ItemProperty.*Run`,
		// Data exfiltration
		`Invoke-WebRequest.*-Method\s+Post.*-Body`,
		`curl.*-X\s+POST.*--data`,
		// Credential theft
		`mimikatz`,
		`procdump.*lsass`,
		`comsvcs\.dll.*MiniDump`,
	}

	for _, pattern := range dangerousScriptPatterns {
		if matched, _ := regexp.MatchString(`(?i)`+pattern, script); matched {
			return fmt.Errorf("script contains dangerous pattern: %s", pattern)
		}
	}

	return nil
}
