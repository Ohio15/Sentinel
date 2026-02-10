; Sentinel RMM Agent Installer
; Inno Setup 6 Script
; Professional installer with embedded config support

#define MyAppName "Sentinel Agent"
#define MyAppVersion "1.72.0"
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
; Note: To use custom wizard images, uncomment and provide:
; WizardImageFile=resources\wizard-large.bmp
; WizardSmallImageFile=resources\wizard-small.bmp
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

[Registry]
; Store installation info
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Sentinel"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"; Flags: uninsdeletekey

[Code]
var
  RefID: String;
  LogFile: String;
  ConfigData: String;
  IsUpgrade: Boolean;

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

// Initialize logging
procedure InitLogging;
var
  LogDir: String;
begin
  LogDir := ExpandConstant('{tmp}\Sentinel');
  ForceDirectories(LogDir);
  LogFile := LogDir + '\install-' + RefID + '.log';
end;

// Write to log file
procedure WriteLog(Msg: String);
var
  LogLine: String;
begin
  LogLine := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0) + ' | ' + Msg;
  SaveStringToFile(LogFile, LogLine + #13#10, True);
end;

// Show error with reference ID
procedure ShowInstallError(ErrorMsg: String);
var
  FullMsg: String;
begin
  WriteLog('ERROR: ' + ErrorMsg);
  FullMsg := 'Installation Error' + #13#10 + #13#10 +
             ErrorMsg + #13#10 + #13#10 +
             'Reference ID: ' + RefID + #13#10 +
             'Log file: ' + LogFile + #13#10 + #13#10 +
             'Please contact support with this information.';
  MsgBox(FullMsg, mbError, MB_OK);
end;

// Read embedded config from installer binary
function ReadEmbeddedConfig: String;
var
  InstallerPath: String;
  FileHandle: Integer;
  FileSize: Integer;
  Buffer: AnsiString;
  MarkerPos: Integer;
  ConfigStr: String;
begin
  Result := '';
  InstallerPath := ExpandConstant('{srcexe}');

  WriteLog('Reading embedded config from: ' + InstallerPath);

  // Read last 8KB of file to find config marker
  FileSize := FileSize64(InstallerPath);
  if FileSize <= 0 then
  begin
    WriteLog('Could not determine installer file size');
    Exit;
  end;

  WriteLog('Installer file size: ' + IntToStr(FileSize) + ' bytes');

  // Load file content - Inno Setup doesn't have direct file reading for binary
  // We'll use a different approach: check for config file in temp
  if FileExists(ExpandConstant('{tmp}\sentinel-config.json')) then
  begin
    if LoadStringFromFile(ExpandConstant('{tmp}\sentinel-config.json'), Buffer) then
    begin
      Result := String(Buffer);
      WriteLog('Found config in temp directory');
    end;
  end;
end;

// Alternative: Read config using PowerShell (more reliable for binary reading)
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
    '$bytes = [System.IO.File]::ReadAllBytes($installerPath); ' +
    '$text = [System.Text.Encoding]::UTF8.GetString($bytes); ' +
    '$idx = $text.LastIndexOf($marker); ' +
    'if ($idx -gt 0) { ' +
    '  $config = $text.Substring($idx + $marker.Length).Trim(); ' +
    '  $config | Out-File -FilePath "' + TempConfigPath + '" -Encoding UTF8 -NoNewline; ' +
    '  exit 0; ' +
    '} else { exit 1; }';

  // Execute PowerShell hidden
  if Exec('powershell.exe',
          '-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "' + PSScript + '"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    if (ResultCode = 0) and FileExists(TempConfigPath) then
    begin
      if LoadStringFromFile(TempConfigPath, ConfigContent) then
      begin
        Result := String(ConfigContent);
        WriteLog('Successfully extracted embedded config: ' + IntToStr(Length(Result)) + ' chars');
      end;
    end
    else
    begin
      WriteLog('No embedded config found (ResultCode: ' + IntToStr(ResultCode) + ')');
    end;
  end
  else
  begin
    WriteLog('PowerShell execution failed');
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

// Stop a Windows service
function StopService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
  RetryCount: Integer;
begin
  Result := True;
  WriteLog('Stopping service: ' + ServiceName);

  // First, try to stop gracefully
  Exec('sc.exe', 'stop "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

  // Wait for service to stop (max 30 seconds)
  RetryCount := 0;
  while RetryCount < 30 do
  begin
    Sleep(1000);
    if Exec('sc.exe', 'query "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    begin
      if ResultCode <> 0 then
      begin
        WriteLog('Service stopped successfully');
        Exit;
      end;
    end;
    RetryCount := RetryCount + 1;
  end;

  // Force kill if still running
  WriteLog('Service did not stop gracefully, attempting force termination');
  Exec('taskkill.exe', '/F /IM ' + ServiceName + '.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// Start a Windows service
function StartService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  WriteLog('Starting service: ' + ServiceName);

  if Exec('sc.exe', 'start "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := (ResultCode = 0);
    if Result then
      WriteLog('Service started successfully')
    else
      WriteLog('Failed to start service, error code: ' + IntToStr(ResultCode));
  end;
end;

// Create Windows service
function CreateService(ServiceName, DisplayName, ExePath, Description: String): Boolean;
var
  ResultCode: Integer;
  Params: String;
begin
  Result := False;
  WriteLog('Creating service: ' + ServiceName);

  // Create the service
  Params := 'create "' + ServiceName + '" binPath= "\"' + ExePath + '\"" start= auto DisplayName= "' + DisplayName + '"';
  if Exec('sc.exe', Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    if ResultCode = 0 then
    begin
      WriteLog('Service created successfully');

      // Set description
      Exec('sc.exe', 'description "' + ServiceName + '" "' + Description + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

      // Configure failure recovery (restart on failure)
      Exec('sc.exe', 'failure "' + ServiceName + '" reset= 86400 actions= restart/5000/restart/10000/restart/30000', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

      Result := True;
    end
    else
    begin
      WriteLog('Failed to create service, error code: ' + IntToStr(ResultCode));
    end;
  end;
end;

// Delete Windows service
function DeleteService(ServiceName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  WriteLog('Deleting service: ' + ServiceName);

  // Stop the service first
  StopService(ServiceName);

  // Delete the service
  if Exec('sc.exe', 'delete "' + ServiceName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := (ResultCode = 0);
    if Result then
      WriteLog('Service deleted successfully')
    else
      WriteLog('Failed to delete service, error code: ' + IntToStr(ResultCode));
  end;
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
    WriteLog('Config file written successfully');
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
    WriteLog('Backing up existing config');
    if LoadStringFromFile(ConfigPath, ConfigContent) then
    begin
      Result := String(ConfigContent);
      BackupPath := ConfigPath + '.backup';
      SaveStringToFile(BackupPath, ConfigContent, False);
      WriteLog('Config backed up to: ' + BackupPath);
    end;
  end;
end;

// Initialize installer
function InitializeSetup: Boolean;
begin
  Result := True;

  // Generate reference ID
  RefID := GenerateRefID;

  // Initialize logging
  InitLogging;

  WriteLog('========================================');
  WriteLog('Sentinel Agent Installer v{#MyAppVersion}');
  WriteLog('Reference ID: ' + RefID);
  WriteLog('========================================');

  // Check for existing installation
  IsUpgrade := ServiceExists('SentinelAgent') or ServiceExists('SentinelWatchdog');
  if IsUpgrade then
    WriteLog('Upgrade installation detected')
  else
    WriteLog('Fresh installation');

  // Try to read embedded config
  ConfigData := ReadConfigViaPowerShell;
  if ConfigData = '' then
  begin
    WriteLog('No embedded config found - will check for existing config during install');
  end
  else
  begin
    WriteLog('Embedded config loaded');
  end;
end;

// Pre-install: stop existing services
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ExistingConfig: String;
begin
  Result := '';
  NeedsRestart := False;

  WriteLog('Preparing installation...');

  // If upgrade, stop services and backup config
  if IsUpgrade then
  begin
    WriteLog('Stopping existing services for upgrade...');

    // Backup existing config before stopping services
    ExistingConfig := BackupExistingConfig;
    if (ConfigData = '') and (ExistingConfig <> '') then
    begin
      ConfigData := ExistingConfig;
      WriteLog('Using existing config for upgrade');
    end;

    if ServiceExists('SentinelWatchdog') then
    begin
      if not StopService('SentinelWatchdog') then
      begin
        Result := 'Failed to stop Sentinel Watchdog service. Please stop it manually and retry.';
        ShowInstallError(Result);
        Exit;
      end;
    end;

    if ServiceExists('SentinelAgent') then
    begin
      if not StopService('SentinelAgent') then
      begin
        Result := 'Failed to stop Sentinel Agent service. Please stop it manually and retry.';
        ShowInstallError(Result);
        Exit;
      end;
    end;

    // Give processes time to fully terminate
    Sleep(2000);
    WriteLog('Existing services stopped');
  end;
end;

// Post-install: create services and write config
procedure CurStepChanged(CurStep: TSetupStep);
var
  AgentPath, WatchdogPath: String;
begin
  if CurStep = ssPostInstall then
  begin
    WriteLog('Post-installation steps...');

    AgentPath := ExpandConstant('{app}\sentinel-agent.exe');
    WatchdogPath := ExpandConstant('{app}\sentinel-watchdog.exe');

    // Write config file
    if ConfigData <> '' then
    begin
      if not WriteConfigFile(ConfigData) then
      begin
        ShowInstallError('Failed to write configuration file');
      end;
    end
    else
    begin
      WriteLog('WARNING: No config data available. Agent may need manual configuration.');
    end;

    // Delete existing services if upgrading (to recreate with new paths)
    if IsUpgrade then
    begin
      DeleteService('SentinelWatchdog');
      DeleteService('SentinelAgent');
      Sleep(1000);
    end;

    // Create Sentinel Agent service
    if not CreateService('SentinelAgent',
                         'Sentinel Agent',
                         AgentPath,
                         'Sentinel RMM monitoring agent. Collects system metrics and executes remote commands.') then
    begin
      ShowInstallError('Failed to create Sentinel Agent service');
    end;

    // Create Sentinel Watchdog service
    if not CreateService('SentinelWatchdog',
                         'Sentinel Watchdog',
                         WatchdogPath,
                         'Sentinel Watchdog service. Monitors and restarts the Sentinel Agent if it stops.') then
    begin
      ShowInstallError('Failed to create Sentinel Watchdog service');
    end;

    // Start services
    Sleep(1000);
    if not StartService('SentinelAgent') then
    begin
      WriteLog('WARNING: Failed to start Sentinel Agent service');
    end;

    Sleep(500);
    if not StartService('SentinelWatchdog') then
    begin
      WriteLog('WARNING: Failed to start Sentinel Watchdog service');
    end;

    WriteLog('Installation completed successfully');
  end;
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
    Exec('taskkill.exe', '/F /IM sentinel-agent.exe', '', SW_HIDE, ewWaitUntilTerminated, UninstallResultCode);
    Exec('taskkill.exe', '/F /IM sentinel-watchdog.exe', '', SW_HIDE, ewWaitUntilTerminated, UninstallResultCode);

    WriteLog('Uninstallation completed');
  end;
end;

// Finalize uninstall - cleanup
procedure DeinitializeUninstall;
begin
  // Additional cleanup if needed
end;
