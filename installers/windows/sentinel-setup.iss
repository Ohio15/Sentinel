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
  ConfigContent: AnsiString;
  PSScript: String;
begin
  Result := '';
  TempConfigPath := ExpandConstant('{tmp}\extracted-config.json');

  WriteLog('Attempting to extract embedded config via PowerShell');

  // PowerShell script to find and extract config after marker
  PSScript :=
    '$marker = "---SENTINEL-CONFIG---"; ' +
    '$installerPath = "' + ExpandConstant('{srcexe}') + '"; ' +
    'try { ' +
    '  $bytes = [System.IO.File]::ReadAllBytes($installerPath); ' +
    '  $text = [System.Text.Encoding]::UTF8.GetString($bytes); ' +
    '  $idx = $text.LastIndexOf($marker); ' +
    '  if ($idx -gt 0) { ' +
    '    $config = $text.Substring($idx + $marker.Length).Trim(); ' +
    '    $config | Out-File -FilePath "' + TempConfigPath + '" -Encoding UTF8 -NoNewline; ' +
    '    exit 0; ' +
    '  } else { exit 1; }' +
    '} catch { exit 2; }';

  if Exec('powershell.exe',
          '-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "' + PSScript + '"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    WriteLog('PowerShell exit code: ' + IntToStr(ResultCode));
    if (ResultCode = 0) and FileExists(TempConfigPath) then
    begin
      if LoadStringFromFile(TempConfigPath, ConfigContent) then
      begin
        Result := String(ConfigContent);
        WriteLog('Successfully extracted embedded config: ' + IntToStr(Length(Result)) + ' chars');
      end;
    end
    else if ResultCode = 1 then
      WriteLog('No embedded config marker found in installer')
    else if ResultCode = 2 then
      WriteLog('PowerShell error reading installer file');
  end
  else
  begin
    WriteLog('PowerShell execution failed to start');
  end;
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

    // Verify service started
    RetryCount := 0;
    while RetryCount < 10 do
    begin
      Sleep(1000);
      if Exec('sc.exe', 'query "' + ServiceName + '" | find "RUNNING"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
      begin
        if ResultCode = 0 then
        begin
          WriteLog('Service started and running');
          Result := True;
          Exit;
        end;
      end;
      RetryCount := RetryCount + 1;
    end;

    WriteLog('Service may not be running after start command');
  end
  else
  begin
    WriteLog('Failed to execute start command');
  end;
end;

// Create Windows service with full error handling
function CreateService(ServiceName, DisplayName, ExePath, Description: String): Boolean;
var
  ResultCode: Integer;
  Params: String;
begin
  Result := False;
  WriteLog('Creating service: ' + ServiceName);
  WriteLog('  Path: ' + ExePath);

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

  // Create the service
  Params := 'create "' + ServiceName + '" binPath= "\"' + ExePath + '\"" start= auto DisplayName= "' + DisplayName + '"';
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
function WriteConfigFile(ConfigJson: String): Boolean;
var
  ConfigPath: String;
begin
  Result := False;
  ConfigPath := ExpandConstant('{app}\config.json');
  WriteLog('Writing config to: ' + ConfigPath);

  if SaveStringToFile(ConfigPath, ConfigJson, False) then
  begin
    WriteLog('Config file written successfully (' + IntToStr(Length(ConfigJson)) + ' bytes)');
    Result := True;
  end
  else
  begin
    WriteLog('Failed to write config file');
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

  // Generate reference ID
  RefID := GenerateRefID;

  // Initialize logging
  InitLogging;

  WriteLog('========================================');
  WriteLog('Sentinel Agent Installer v{#MyAppVersion}');
  WriteLog('Reference ID: ' + RefID);
  WriteLog('Installer: ' + ExpandConstant('{srcexe}'));
  WriteLog('Target: ' + ExpandConstant('{app}'));
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
  if CurStep = ssPostInstall then
  begin
    WriteLog('========================================');
    WriteLog('POST-INSTALL');
    WriteLog('========================================');

    AgentPath := ExpandConstant('{app}\sentinel-agent.exe');
    WatchdogPath := ExpandConstant('{app}\sentinel-watchdog.exe');

    WriteLog('Agent path: ' + AgentPath);
    WriteLog('Watchdog path: ' + WatchdogPath);

    // Verify files were extracted
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

    // Write config file
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

    // Delete existing services if upgrading (to recreate with new paths)
    if IsUpgrade then
    begin
      WriteLog('Removing old service registrations...');
      DeleteService('SentinelWatchdog');
      DeleteService('SentinelAgent');
      Sleep(1000);
    end;

    // Create Sentinel Agent service
    WriteLog('Creating SentinelAgent service...');
    AgentCreated := CreateService('SentinelAgent',
                                  'Sentinel Agent',
                                  AgentPath,
                                  'Sentinel RMM monitoring agent. Collects system metrics and executes remote commands.');
    if not AgentCreated then
    begin
      ShowInstallError('Failed to create Sentinel Agent service.' + #13#10 +
                      'Check the log file for details.');
      AttemptRollback;
      Exit;
    end;

    // Create Sentinel Watchdog service
    WriteLog('Creating SentinelWatchdog service...');
    WatchdogCreated := CreateService('SentinelWatchdog',
                                     'Sentinel Watchdog',
                                     WatchdogPath,
                                     'Sentinel Watchdog service. Monitors and restarts the Sentinel Agent if it stops.');
    if not WatchdogCreated then
    begin
      ShowInstallWarning('Failed to create Sentinel Watchdog service.' + #13#10 +
                        'The agent will run without watchdog protection.');
    end;

    // Start services
    WriteLog('Starting services...');
    Sleep(1000);

    if not StartService('SentinelAgent') then
    begin
      WriteLog('WARNING: Failed to start Sentinel Agent service');
      ShowInstallWarning('The Sentinel Agent service was created but failed to start.' + #13#10 +
                        'You may need to start it manually from services.msc');
    end;

    Sleep(500);
    if WatchdogCreated then
    begin
      if not StartService('SentinelWatchdog') then
      begin
        WriteLog('WARNING: Failed to start Sentinel Watchdog service');
      end;
    end;

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
