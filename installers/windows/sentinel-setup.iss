; Sentinel RMM Agent Installer
; Inno Setup 6 Script
; Professional installer with embedded config support, robust logging, and rollback

#define MyAppName "Sentinel Agent"
#define MyAppVersion "1.73.0"
#define MyAppPublisher "Sentinel RMM"
#define MyAppURL "https://sentinelrmm.us"
#define MyAppExeName "sentinel-agent.exe"
#define ConfigMarker "---SENTINEL-CONFIG---"

[Setup]
; Basic app info
AppId={{8A7C3B2D-4E5F-6789-ABCD-EF0123456789}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/support
AppUpdatesURL={#MyAppURL}/updates
DefaultDirName={autopf}\Sentinel
DefaultGroupName=Sentinel
DisableProgramGroupPage=yes
LicenseFile=resources\license.rtf
; Output settings
OutputDir=..\..\release\agent
OutputBaseFilename=sentinel-installer-template
SetupIconFile=resources\sentinel.ico
; Compression
Compression=lzma2/ultra64
SolidCompression=yes
; UI
WizardStyle=modern
; Privileges
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=
; Misc
Uninstallable=yes
UninstallDisplayIcon={app}\sentinel-agent.exe
UninstallDisplayName=Sentinel Agent
CreateUninstallRegKey=yes
; Disable restart prompt
RestartIfNeededByRun=no
CloseApplications=force
CloseApplicationsFilter=sentinel-agent.exe,sentinel-watchdog.exe

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; Main executables
Source: "..\..\release\agent\sentinel-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\release\agent\sentinel-watchdog.exe"; DestDir: "{app}"; Flags: ignoreversion
; Desktop helper for tray icon (if exists)
Source: "..\..\release\agent\sentinel-desktop.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\release\agent\sentinel-desktop-helper.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Dirs]
Name: "{app}\logs"; Permissions: users-modify
Name: "{commonappdata}\Sentinel\logs"; Permissions: users-modify

[Registry]
; Store installation info
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "InstallDate"; ValueData: "{code:GetInstallDate}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "LastRefID"; ValueData: "{code:GetRefID}"; Flags: uninsdeletekey

[Code]
var
  RefID: String;
  LogFile: String;
  ConfigData: String;
  IsUpgrade: Boolean;
  ServicesWereStopped: Boolean;
  BackedUpConfig: String;
  OldAgentPath: String;
  OldWatchdogPath: String;
  InstallationFailed: Boolean;
  // Progress tracking
  TotalInstallSteps: Integer;
  CurrentInstallStep: Integer;
  FileExtractionComplete: Boolean;

// Generate unique reference ID for error tracking
function GenerateRefID: String;
var
  GUID: String;
  DateStr: String;
begin
  GUID := GetDateTimeString('hhnnss', #0, #0);
  DateStr := GetDateTimeString('yyyymmdd', #0, #0);
  Result := 'INS-' + GUID + '-' + DateStr;
end;

// Get reference ID for registry
function GetRefID(Param: String): String;
begin
  Result := RefID;
end;

// Get install date for registry
function GetInstallDate(Param: String): String;
begin
  Result := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0);
end;

// Initialize logging - use ProgramData for persistent logs
procedure InitLogging;
var
  LogDir: String;
begin
  // Use ProgramData for persistent logs that survive temp cleanup
  LogDir := ExpandConstant('{commonappdata}\Sentinel\logs');
  ForceDirectories(LogDir);
  LogFile := LogDir + '\install-' + RefID + '.log';

  // Also keep a copy in temp for immediate access
  ForceDirectories(ExpandConstant('{tmp}\Sentinel'));
end;

// Write to log file with flush
procedure WriteLog(Msg: String);
var
  LogLine: String;
  TempLog: String;
begin
  LogLine := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0) + ' | ' + Msg;

  // Write to persistent log
  SaveStringToFile(LogFile, LogLine + #13#10, True);

  // Also write to temp log for immediate debugging
  TempLog := ExpandConstant('{tmp}\Sentinel\install-' + RefID + '.log');
  SaveStringToFile(TempLog, LogLine + #13#10, True);
end;

// ============================================================================
// Progress Tracking Functions
// ============================================================================

// Update the installation progress bar and status text
procedure UpdateInstallProgress(StepNumber: Integer; StatusText: String);
var
  ProgressPercent: Integer;
begin
  CurrentInstallStep := StepNumber;

  // Calculate progress: file extraction is 0-60%, post-install is 60-100%
  if FileExtractionComplete then
    ProgressPercent := 60 + ((StepNumber * 40) div TotalInstallSteps)
  else
    ProgressPercent := (StepNumber * 60) div TotalInstallSteps;

  // Clamp to 100%
  if ProgressPercent > 100 then
    ProgressPercent := 100;

  // Update the wizard progress gauge
  WizardForm.ProgressGauge.Position := (ProgressPercent * WizardForm.ProgressGauge.Max) div 100;

  // Update status label
  WizardForm.StatusLabel.Caption := StatusText;

  // Force UI update
  WizardForm.Refresh;

  WriteLog('Progress [' + IntToStr(ProgressPercent) + '%]: ' + StatusText);
end;

// Called during file extraction to track progress
procedure CurInstallProgressChanged(CurProgress, MaxProgress: Integer);
var
  Percent: Integer;
begin
  // Calculate percentage of file extraction (0-60% of total)
  if MaxProgress > 0 then
  begin
    Percent := (CurProgress * 60) div MaxProgress;
    WizardForm.ProgressGauge.Position := (Percent * WizardForm.ProgressGauge.Max) div 100;
  end;
end;

// Show error with reference ID - keeps window open
procedure ShowInstallError(ErrorMsg: String);
var
  FullMsg: String;
begin
  WriteLog('CRITICAL ERROR: ' + ErrorMsg);
  InstallationFailed := True;

  FullMsg := 'Installation Error' + #13#10 + #13#10 +
             ErrorMsg + #13#10 + #13#10 +
             '----------------------------------------' + #13#10 +
             'Reference ID: ' + RefID + #13#10 +
             'Log file: ' + LogFile + #13#10 +
             '----------------------------------------' + #13#10 + #13#10 +
             'The log file contains detailed information.' + #13#10 +
             'Please contact support with the Reference ID.';
  MsgBox(FullMsg, mbCriticalError, MB_OK);
end;

// Show warning but continue
procedure ShowInstallWarning(WarningMsg: String);
var
  FullMsg: String;
begin
  WriteLog('WARNING: ' + WarningMsg);

  FullMsg := 'Installation Warning' + #13#10 + #13#10 +
             WarningMsg + #13#10 + #13#10 +
             'Reference ID: ' + RefID;
  MsgBox(FullMsg, mbInformation, MB_OK);
end;

// Read embedded config using PowerShell
function ReadConfigViaPowerShell: String;
var
  ResultCode: Integer;
  TempConfigPath: String;
  TempScriptPath: String;
  ConfigContent: AnsiString;
  PSScript: String;
begin
  Result := '';
  TempConfigPath := ExpandConstant('{tmp}\extracted-config.json');
  TempScriptPath := ExpandConstant('{tmp}\extract-config.ps1');

  WriteLog('Attempting to extract embedded config via PowerShell');
  WriteLog('Installer path: ' + ExpandConstant('{srcexe}'));

  // PowerShell script to find and extract config after marker
  // Uses byte-level search to avoid encoding issues with binary EXE data
  PSScript :=
    '$ErrorActionPreference = "Stop"' + #13#10 +
    '$marker = [System.Text.Encoding]::UTF8.GetBytes("---SENTINEL-CONFIG---")' + #13#10 +
    '$installerPath = "' + ExpandConstant('{srcexe}') + '"' + #13#10 +
    '$outputPath = "' + TempConfigPath + '"' + #13#10 +
    'try {' + #13#10 +
    '  $bytes = [System.IO.File]::ReadAllBytes($installerPath)' + #13#10 +
    '  # Search from end of file for marker (more efficient)' + #13#10 +
    '  $markerLen = $marker.Length' + #13#10 +
    '  $found = -1' + #13#10 +
    '  for ($i = $bytes.Length - $markerLen - 1; $i -ge [Math]::Max(0, $bytes.Length - 10000); $i--) {' + #13#10 +
    '    $match = $true' + #13#10 +
    '    for ($j = 0; $j -lt $markerLen; $j++) {' + #13#10 +
    '      if ($bytes[$i + $j] -ne $marker[$j]) { $match = $false; break }' + #13#10 +
    '    }' + #13#10 +
    '    if ($match) { $found = $i; break }' + #13#10 +
    '  }' + #13#10 +
    '  if ($found -gt 0) {' + #13#10 +
    '    $configStart = $found + $markerLen' + #13#10 +
    '    $configBytes = $bytes[$configStart..($bytes.Length-1)]' + #13#10 +
    '    $config = [System.Text.Encoding]::UTF8.GetString($configBytes).Trim()' + #13#10 +
    '    # Validate it looks like JSON' + #13#10 +
    '    if ($config.StartsWith("{") -and $config.Contains("server_url")) {' + #13#10 +
    '      [System.IO.File]::WriteAllText($outputPath, $config)' + #13#10 +
    '      exit 0' + #13#10 +
    '    } else {' + #13#10 +
    '      Write-Host "Config does not look like valid JSON: $config"' + #13#10 +
    '      exit 3' + #13#10 +
    '    }' + #13#10 +
    '  } else { exit 1 }' + #13#10 +
    '} catch {' + #13#10 +
    '  Write-Host "Error: $_"' + #13#10 +
    '  exit 2' + #13#10 +
    '}';

  // Write script to file to avoid command-line escaping issues
  SaveStringToFile(TempScriptPath, PSScript, False);

  if Exec('powershell.exe',
          '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + TempScriptPath + '"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    WriteLog('PowerShell exit code: ' + IntToStr(ResultCode));
    if (ResultCode = 0) and FileExists(TempConfigPath) then
    begin
      if LoadStringFromFile(TempConfigPath, ConfigContent) then
      begin
        Result := String(ConfigContent);
        WriteLog('Successfully extracted embedded config: ' + IntToStr(Length(Result)) + ' chars');
        WriteLog('Config preview: ' + Copy(Result, 1, 100) + '...');
      end;
    end
    else if ResultCode = 1 then
      WriteLog('No embedded config marker found in installer (searched last 10KB)')
    else if ResultCode = 2 then
      WriteLog('PowerShell error reading installer file')
    else if ResultCode = 3 then
      WriteLog('Config marker found but content is not valid JSON');
  end
  else
  begin
    WriteLog('PowerShell execution failed to start');
  end;

  // Cleanup script file
  DeleteFile(TempScriptPath);
end;

// Check if services exist (upgrade scenario)
function ServiceExists(ServiceName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  if Exec('sc.exe', 'query "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := (ResultCode = 0);
  end;
end;

// Get service executable path from registry
function GetServicePath(ServiceName: String): String;
var
  Path: String;
begin
  Result := '';
  if RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Services\' + ServiceName, 'ImagePath', Path) then
  begin
    // Remove quotes if present
    Result := RemoveQuotes(Path);
  end;
end;

// Stop a Windows service with detailed logging
function StopService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
  RetryCount: Integer;
  QueryResult: String;
begin
  Result := True;
  WriteLog('Stopping service: ' + ServiceName);

  // Check if service exists first
  if not ServiceExists(ServiceName) then
  begin
    WriteLog('Service does not exist, skipping stop');
    Exit;
  end;

  // Try to stop gracefully
  if Exec('sc.exe', 'stop "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    WriteLog('Stop command sent, result: ' + IntToStr(ResultCode))
  else
    WriteLog('Failed to send stop command');

  // Wait for service to stop (max 30 seconds)
  RetryCount := 0;
  while RetryCount < 30 do
  begin
    Sleep(1000);
    if not ServiceExists(ServiceName) then
    begin
      WriteLog('Service stopped and removed');
      Exit;
    end;

    // Check if stopped but still registered
    if Exec('sc.exe', 'query "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    begin
      if ResultCode <> 0 then
      begin
        WriteLog('Service stopped successfully after ' + IntToStr(RetryCount) + ' seconds');
        Exit;
      end;
    end;
    RetryCount := RetryCount + 1;
    WriteLog('Waiting for service to stop... (' + IntToStr(RetryCount) + '/30)');
  end;

  // Force kill if still running
  WriteLog('Service did not stop gracefully after 30s, attempting force termination');
  Exec('taskkill.exe', '/F /IM sentinel-agent.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Exec('taskkill.exe', '/F /IM sentinel-watchdog.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(2000);

  WriteLog('Force termination completed');
end;

// Check if a service is currently running
function IsServiceRunning(ServiceName: String): Boolean;
var
  ResultCode: Integer;
  TempFile: String;
  QueryOutput: AnsiString;
begin
  Result := False;
  TempFile := ExpandConstant('{tmp}\sc-query-' + ServiceName + '.txt');

  // Query service and redirect output to file
  if Exec('cmd.exe', '/C sc.exe query "' + ServiceName + '" > "' + TempFile + '" 2>&1', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    if FileExists(TempFile) then
    begin
      if LoadStringFromFile(TempFile, QueryOutput) then
      begin
        // Check if output contains "RUNNING"
        Result := (Pos('RUNNING', String(QueryOutput)) > 0);
      end;
      DeleteFile(TempFile);
    end;
  end;
end;

// Start a Windows service with verification
function StartService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
  RetryCount: Integer;
begin
  Result := False;
  WriteLog('Starting service: ' + ServiceName);

  if Exec('sc.exe', 'start "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    WriteLog('Start command result: ' + IntToStr(ResultCode));

    // Verify service started by checking status
    RetryCount := 0;
    while RetryCount < 10 do
    begin
      Sleep(1000);
      if IsServiceRunning(ServiceName) then
      begin
        WriteLog('Service started and running');
        Result := True;
        Exit;
      end;
      RetryCount := RetryCount + 1;
      WriteLog('Waiting for service to start... (' + IntToStr(RetryCount) + '/10)');
    end;

    // One final check - the service might have started but we missed it
    if IsServiceRunning(ServiceName) then
    begin
      WriteLog('Service confirmed running after extended wait');
      Result := True;
    end
    else
    begin
      WriteLog('Service may not be running after start command');
    end;
  end
  else
  begin
    WriteLog('Failed to execute start command');
  end;
end;

// Create Windows service with full error handling.
//
// ExeArgs is appended after the quoted executable path inside binPath so the
// agent receives flags like '--service' on startup. The agent (kardianos/service)
// only registers with SCM when --service is present; without it it runs in
// interactive mode and SCM times out after 30s with error 1053. Bug observed
// 2026-05-22 on INS-055750. Pass an empty string for binaries that
// auto-detect service mode (e.g. sentinel-watchdog uses svc.IsWindowsService()).
function CreateService(ServiceName, DisplayName, ExePath, ExeArgs, Description: String): Boolean;
var
  ResultCode: Integer;
  Params: String;
  BinPath: String;
begin
  Result := False;
  WriteLog('Creating service: ' + ServiceName);
  WriteLog('  Path: ' + ExePath);
  WriteLog('  Args: ' + ExeArgs);

  // Verify executable exists
  if not FileExists(ExePath) then
  begin
    WriteLog('ERROR: Executable not found: ' + ExePath);
    Exit;
  end;

  // Delete existing service if present
  if ServiceExists(ServiceName) then
  begin
    WriteLog('Service already exists, deleting first');
    Exec('sc.exe', 'delete "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(1000);
  end;

  // Build binPath. Per sc.exe quoting rules the entire path-plus-args goes
  // inside the outer "..." that wraps the binPath value, with the exe path
  // itself escaped-quoted to survive the space in 'Program Files'.
  BinPath := '\"' + ExePath + '\"';
  if Length(ExeArgs) > 0 then
    BinPath := BinPath + ' ' + ExeArgs;

  Params := 'create "' + ServiceName + '" binPath= "' + BinPath + '" start= auto DisplayName= "' + DisplayName + '"';
  WriteLog('sc.exe ' + Params);

  if Exec('sc.exe', Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    WriteLog('Create command result: ' + IntToStr(ResultCode));
    if ResultCode = 0 then
    begin
      WriteLog('Service created successfully');

      // Set description
      Exec('sc.exe', 'description "' + ServiceName + '" "' + Description + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

      // Configure failure recovery (restart on failure)
      Exec('sc.exe', 'failure "' + ServiceName + '" reset= 86400 actions= restart/5000/restart/10000/restart/30000', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
      WriteLog('Failure recovery configured');

      Result := True;
    end
    else
    begin
      WriteLog('Failed to create service, error code: ' + IntToStr(ResultCode));
    end;
  end
  else
  begin
    WriteLog('Failed to execute sc.exe create command');
  end;
end;

// Delete Windows service
function DeleteService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;

  if not ServiceExists(ServiceName) then
  begin
    WriteLog('Service ' + ServiceName + ' does not exist, skipping delete');
    Result := True;
    Exit;
  end;

  WriteLog('Deleting service: ' + ServiceName);

  // Stop the service first
  StopService(ServiceName);

  // Delete the service
  if Exec('sc.exe', 'delete "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := (ResultCode = 0) or (ResultCode = 1060); // 1060 = service doesn't exist
    if Result then
      WriteLog('Service deleted successfully')
    else
      WriteLog('Failed to delete service, error code: ' + IntToStr(ResultCode));
  end;

  Sleep(1000); // Give SCM time to clean up
end;

// Write config file
//
// The agent reads its config from %ProgramData%\Sentinel\config.json
// (internal/config/config.go:109). Writing to {app}\config.json instead
// caused the agent to start with no config — root cause of the 1053 timeout
// observed on INS-055750-20260522. We write to ProgramData first; falling
// back to {app}\config.json only if the canonical write fails.
function WriteConfigFile(ConfigJson: String): Boolean;
var
  ConfigDataPath: String;
  ConfigAppPath: String;
  ConfigDir: String;
begin
  Result := False;
  ConfigDataPath := ExpandConstant('{commonappdata}\Sentinel\config.json');
  ConfigAppPath  := ExpandConstant('{app}\config.json');
  ConfigDir := ExpandConstant('{commonappdata}\Sentinel');

  // Ensure ProgramData\Sentinel exists before writing
  if not DirExists(ConfigDir) then
    ForceDirectories(ConfigDir);

  WriteLog('Writing config to: ' + ConfigDataPath);
  if SaveStringToFile(ConfigDataPath, ConfigJson, False) then
  begin
    WriteLog('Config file written to ProgramData (' + IntToStr(Length(ConfigJson)) + ' bytes)');
    // Also drop a copy in the install dir for diagnostics / manual edit
    SaveStringToFile(ConfigAppPath, ConfigJson, False);
    Result := True;
  end
  else
  begin
    WriteLog('Failed to write config to ProgramData, falling back to install dir');
    if SaveStringToFile(ConfigAppPath, ConfigJson, False) then
    begin
      WriteLog('Config file written to install dir (' + IntToStr(Length(ConfigJson)) + ' bytes) — agent may not find it');
      Result := True;
    end
    else
    begin
      WriteLog('Failed to write config file to either location');
    end;
  end;
end;

// Backup existing config
function BackupExistingConfig: String;
var
  ConfigPath, BackupPath: String;
  ConfigContent: AnsiString;
begin
  Result := '';
  ConfigPath := ExpandConstant('{app}\config.json');

  if FileExists(ConfigPath) then
  begin
    WriteLog('Backing up existing config from: ' + ConfigPath);
    if LoadStringFromFile(ConfigPath, ConfigContent) then
    begin
      Result := String(ConfigContent);
      BackupPath := ExpandConstant('{commonappdata}\Sentinel\config-backup-' + RefID + '.json');
      SaveStringToFile(BackupPath, ConfigContent, False);
      WriteLog('Config backed up to: ' + BackupPath);
    end
    else
    begin
      WriteLog('Failed to read existing config file');
    end;
  end
  else
  begin
    WriteLog('No existing config file found');
  end;
end;

// Attempt rollback on failure
procedure AttemptRollback;
begin
  WriteLog('========================================');
  WriteLog('ATTEMPTING ROLLBACK');
  WriteLog('========================================');

  if ServicesWereStopped then
  begin
    WriteLog('Services were stopped during install, attempting to restart...');

    // Try to restart the agent service if it still exists
    if ServiceExists('SentinelAgent') then
    begin
      WriteLog('SentinelAgent service exists, attempting restart');
      StartService('SentinelAgent');
    end
    else
    begin
      WriteLog('SentinelAgent service no longer exists');
    end;

    if ServiceExists('SentinelWatchdog') then
    begin
      WriteLog('SentinelWatchdog service exists, attempting restart');
      StartService('SentinelWatchdog');
    end;
  end;

  WriteLog('Rollback attempt completed');
end;

// Initialize installer
function InitializeSetup: Boolean;
begin
  Result := True;
  InstallationFailed := False;
  ServicesWereStopped := False;
  FileExtractionComplete := False;

  // Initialize progress tracking
  // Post-install steps: verify files, write config, remove old services,
  // create agent service, create watchdog service, start agent, start watchdog
  TotalInstallSteps := 7;
  CurrentInstallStep := 0;

  // Generate reference ID
  RefID := GenerateRefID;

  // Initialize logging
  InitLogging;

  WriteLog('========================================');
  WriteLog('Sentinel Agent Installer v{#MyAppVersion}');
  WriteLog('Reference ID: ' + RefID);
  WriteLog('Installer: ' + ExpandConstant('{srcexe}'));
  WriteLog('Default target: ' + ExpandConstant('{autopf}\Sentinel'));
  WriteLog('========================================');

  // Check for existing installation
  IsUpgrade := ServiceExists('SentinelAgent') or ServiceExists('SentinelWatchdog');
  if IsUpgrade then
  begin
    WriteLog('UPGRADE: Existing installation detected');
    OldAgentPath := GetServicePath('SentinelAgent');
    OldWatchdogPath := GetServicePath('SentinelWatchdog');
    WriteLog('  Old agent path: ' + OldAgentPath);
    WriteLog('  Old watchdog path: ' + OldWatchdogPath);
  end
  else
  begin
    WriteLog('FRESH INSTALL: No existing installation');
  end;

  // Try to read embedded config
  ConfigData := ReadConfigViaPowerShell;
  if ConfigData = '' then
  begin
    WriteLog('No embedded config found in installer');
  end
  else
  begin
    WriteLog('Embedded config loaded successfully');
  end;
end;

// Pre-install: stop existing services
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ExistingConfig: String;
begin
  Result := '';
  NeedsRestart := False;

  WriteLog('========================================');
  WriteLog('PREPARE TO INSTALL');
  WriteLog('========================================');

  // If upgrade, stop services and backup config
  if IsUpgrade then
  begin
    // Backup existing config FIRST before any changes
    ExistingConfig := BackupExistingConfig;
    if (ConfigData = '') and (ExistingConfig <> '') then
    begin
      ConfigData := ExistingConfig;
      WriteLog('Using existing config for upgrade');
    end;

    BackedUpConfig := ExistingConfig;

    WriteLog('Stopping existing services for upgrade...');

    if ServiceExists('SentinelWatchdog') then
    begin
      if not StopService('SentinelWatchdog') then
      begin
        Result := 'Failed to stop Sentinel Watchdog service.' + #13#10 +
                  'Please stop it manually (services.msc) and retry.' + #13#10 +
                  'Reference ID: ' + RefID;
        ShowInstallError(Result);
        Exit;
      end;
      ServicesWereStopped := True;
    end;

    if ServiceExists('SentinelAgent') then
    begin
      if not StopService('SentinelAgent') then
      begin
        Result := 'Failed to stop Sentinel Agent service.' + #13#10 +
                  'Please stop it manually (services.msc) and retry.' + #13#10 +
                  'Reference ID: ' + RefID;
        ShowInstallError(Result);
        Exit;
      end;
      ServicesWereStopped := True;
    end;

    // Give processes time to fully terminate
    Sleep(2000);
    WriteLog('Existing services stopped successfully');
  end;

  WriteLog('PrepareToInstall completed');
end;

// Post-install: create services and write config
procedure CurStepChanged(CurStep: TSetupStep);
var
  AgentPath, WatchdogPath: String;
  AgentCreated, WatchdogCreated: Boolean;
begin
  // Mark file extraction complete when entering post-install
  if CurStep = ssPostInstall then
  begin
    FileExtractionComplete := True;

    WriteLog('========================================');
    WriteLog('POST-INSTALL');
    WriteLog('========================================');

    AgentPath := ExpandConstant('{app}\sentinel-agent.exe');
    WatchdogPath := ExpandConstant('{app}\sentinel-watchdog.exe');

    WriteLog('Agent path: ' + AgentPath);
    WriteLog('Watchdog path: ' + WatchdogPath);

    // Step 1: Verify files were extracted
    UpdateInstallProgress(1, 'Verifying extracted files...');

    if not FileExists(AgentPath) then
    begin
      WriteLog('CRITICAL: Agent executable not found after extraction!');
      ShowInstallError('Agent executable was not extracted properly.' + #13#10 +
                       'File not found: ' + AgentPath);
      AttemptRollback;
      Exit;
    end;

    if not FileExists(WatchdogPath) then
    begin
      WriteLog('CRITICAL: Watchdog executable not found after extraction!');
      ShowInstallError('Watchdog executable was not extracted properly.' + #13#10 +
                       'File not found: ' + WatchdogPath);
      AttemptRollback;
      Exit;
    end;

    WriteLog('Both executables verified present');

    // Step 2: Write config file
    UpdateInstallProgress(2, 'Writing configuration...');

    if ConfigData <> '' then
    begin
      if not WriteConfigFile(ConfigData) then
      begin
        ShowInstallWarning('Failed to write configuration file.' + #13#10 +
                          'You may need to configure the agent manually.');
      end;
    end
    else
    begin
      WriteLog('WARNING: No config data available. Agent will need manual configuration.');
      ShowInstallWarning('No configuration was embedded in this installer.' + #13#10 +
                        'The agent may need manual configuration via config.json');
    end;

    // Step 3: Delete existing services if upgrading
    if IsUpgrade then
    begin
      UpdateInstallProgress(3, 'Removing old services...');
      WriteLog('Removing old service registrations...');
      DeleteService('SentinelWatchdog');
      DeleteService('SentinelAgent');
      Sleep(1000);
    end
    else
    begin
      UpdateInstallProgress(3, 'Preparing services...');
      Sleep(200);
    end;

    // Step 4: Create Sentinel Agent service
    UpdateInstallProgress(4, 'Creating Sentinel Agent service...');
    WriteLog('Creating SentinelAgent service...');
    AgentCreated := CreateService('SentinelAgent',
                                  'Sentinel Agent',
                                  AgentPath,
                                  '--service',
                                  'Sentinel RMM monitoring agent. Collects system metrics and executes remote commands.');
    if not AgentCreated then
    begin
      ShowInstallError('Failed to create Sentinel Agent service.' + #13#10 +
                      'Check the log file for details.');
      AttemptRollback;
      Exit;
    end;

    // Step 5: Create Sentinel Watchdog service
    UpdateInstallProgress(5, 'Creating Sentinel Watchdog service...');
    WriteLog('Creating SentinelWatchdog service...');
    WatchdogCreated := CreateService('SentinelWatchdog',
                                     'Sentinel Watchdog',
                                     WatchdogPath,
                                     '',
                                     'Sentinel Watchdog service. Monitors and restarts the Sentinel Agent if it stops.');
    if not WatchdogCreated then
    begin
      ShowInstallWarning('Failed to create Sentinel Watchdog service.' + #13#10 +
                        'The agent will run without watchdog protection.');
    end;

    // Step 6: Start Sentinel Agent service
    UpdateInstallProgress(6, 'Starting Sentinel Agent...');
    WriteLog('Starting services...');

    if not StartService('SentinelAgent') then
    begin
      WriteLog('WARNING: Failed to start Sentinel Agent service');
      ShowInstallWarning('The Sentinel Agent service was created but failed to start.' + #13#10 +
                        'You may need to start it manually from services.msc');
    end;

    // Step 7: Start Watchdog service
    if WatchdogCreated then
    begin
      UpdateInstallProgress(7, 'Starting Sentinel Watchdog...');
      if not StartService('SentinelWatchdog') then
      begin
        WriteLog('WARNING: Failed to start Sentinel Watchdog service');
      end;
    end
    else
    begin
      UpdateInstallProgress(7, 'Finalizing installation...');
    end;

    // Final: Complete
    UpdateInstallProgress(7, 'Installation complete!');

    WriteLog('========================================');
    WriteLog('INSTALLATION COMPLETED');
    WriteLog('========================================');
  end;
end;

// Handle install failure
procedure CurPageChanged(CurPageID: Integer);
begin
  // Log page transitions for debugging
  WriteLog('Page changed to: ' + IntToStr(CurPageID));
end;

// Cleanup on cancel or failure
procedure DeinitializeSetup;
begin
  if InstallationFailed then
  begin
    WriteLog('Installation failed, cleanup initiated');
    AttemptRollback;
  end;

  WriteLog('Setup deinitialize complete');
  WriteLog('Log file: ' + LogFile);
end;

// Uninstall: stop and remove services
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  UninstallResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    // Initialize for uninstall logging
    RefID := GenerateRefID;
    InitLogging;

    WriteLog('========================================');
    WriteLog('Sentinel Agent Uninstaller');
    WriteLog('Reference ID: ' + RefID);
    WriteLog('========================================');

    // Stop and delete services
    WriteLog('Removing services...');

    if ServiceExists('SentinelWatchdog') then
      DeleteService('SentinelWatchdog');

    if ServiceExists('SentinelAgent') then
      DeleteService('SentinelAgent');

    // Give time for services to fully stop
    Sleep(2000);

    // Force kill any remaining processes
    WriteLog('Force killing any remaining processes...');
    Exec('taskkill.exe', '/F /IM sentinel-agent.exe', '', SW_HIDE, ewWaitUntilTerminated, UninstallResultCode);
    Exec('taskkill.exe', '/F /IM sentinel-watchdog.exe', '', SW_HIDE, ewWaitUntilTerminated, UninstallResultCode);
    Exec('taskkill.exe', '/F /IM sentinel-desktop.exe', '', SW_HIDE, ewWaitUntilTerminated, UninstallResultCode);

    WriteLog('Uninstallation completed');
  end;
end;

// Finalize uninstall - cleanup
procedure DeinitializeUninstall;
begin
  WriteLog('Uninstall deinitialize complete');
end;
