; ============================================================================
; Sentinel Installer - Error Handler Include
; Inno Setup 6 code for consistent error handling across installers
;
; Usage in main .iss file:
;   #include "error-handler.iss"
;
; Then in [Code] section:
;   InitErrorHandler();
;   ... your installation code ...
;   if SomethingFailed then
;     ShowInstallerError(E_SERVICE_CREATE_FAILED, 'Failed to create service');
; ============================================================================

[Code]

// ============================================================================
// ERROR CODE CONSTANTS
// ============================================================================

const
  // E1xx: Installation errors
  E_INSTALL_GENERAL_FAILURE = 100;
  E_EXTRACT_FAILED = 101;
  E_DISK_SPACE_INSUFFICIENT = 102;
  E_PERMISSION_DENIED = 103;
  E_PATH_NOT_FOUND = 104;
  E_PATH_NOT_WRITABLE = 105;
  E_FILE_IN_USE = 106;
  E_BINARY_CORRUPT = 107;
  E_CHECKSUM_MISMATCH = 108;
  E_PLATFORM_MISMATCH = 109;
  E_PREREQUISITES_MISSING = 110;
  E_TEMP_DIR_CREATION_FAILED = 111;
  E_DOWNLOAD_FAILED = 112;
  E_TIMEOUT = 113;
  E_CLEANUP_FAILED = 114;
  E_VERSION_CONFLICT = 115;

  // E2xx: Service errors
  E_SERVICE_GENERAL_FAILURE = 200;
  E_SERVICE_CREATE_FAILED = 201;
  E_SERVICE_START_FAILED = 202;
  E_SERVICE_STOP_FAILED = 203;
  E_SERVICE_ALREADY_EXISTS = 204;
  E_SERVICE_NOT_FOUND = 205;
  E_SERVICE_TIMEOUT = 206;
  E_SERVICE_DEPENDENCY_FAILED = 207;
  E_SERVICE_DELETE_FAILED = 208;
  E_SERVICE_ACCESS_DENIED = 209;
  E_SYSTEMD_RELOAD_FAILED = 210;
  E_LAUNCHD_LOAD_FAILED = 211;
  E_LAUNCHD_UNLOAD_FAILED = 212;
  E_SERVICE_REGISTRY_ERROR = 213;
  E_SERVICE_HEALTH_CHECK_FAILED = 214;

  // E3xx: Configuration errors
  E_CONFIG_GENERAL_FAILURE = 300;
  E_CONFIG_INVALID_JSON = 301;
  E_CONFIG_MISSING_SERVER = 302;
  E_CONFIG_MISSING_TOKEN = 303;
  E_CONFIG_WRITE_FAILED = 304;
  E_CONFIG_READ_FAILED = 305;
  E_CONFIG_PARSE_ERROR = 306;
  E_CONFIG_INVALID_SERVER_URL = 307;
  E_CONFIG_INVALID_TOKEN = 308;
  E_CONFIG_FILE_LOCKED = 309;
  E_CONFIG_BACKUP_FAILED = 310;
  E_CONFIG_RESTORE_FAILED = 311;
  E_CONFIG_MIGRATION_FAILED = 312;
  E_CONFIG_ENCRYPTION_FAILED = 313;
  E_CONFIG_DECRYPTION_FAILED = 314;

  // E4xx: Network errors
  E_NETWORK_GENERAL_FAILURE = 400;
  E_SERVER_UNREACHABLE = 401;
  E_TOKEN_INVALID = 402;
  E_TOKEN_EXPIRED = 403;
  E_TOKEN_MAX_USES = 404;
  E_SSL_CERTIFICATE_ERROR = 405;
  E_SSL_HANDSHAKE_FAILED = 406;
  E_DNS_RESOLUTION_FAILED = 407;
  E_CONNECTION_TIMEOUT = 408;
  E_CONNECTION_REFUSED = 409;
  E_PROXY_ERROR = 410;
  E_HTTP_ERROR_401 = 411;
  E_HTTP_ERROR_403 = 412;
  E_HTTP_ERROR_404 = 413;
  E_HTTP_ERROR_500 = 414;
  E_DOWNLOAD_INTERRUPTED = 415;
  E_ENROLLMENT_FAILED = 416;

  // E5xx: Upgrade errors
  E_UPGRADE_GENERAL_FAILURE = 500;
  E_UPGRADE_STOP_FAILED = 501;
  E_UPGRADE_BACKUP_FAILED = 502;
  E_UPGRADE_ROLLBACK_FAILED = 503;
  E_UPGRADE_VERSION_DOWNGRADE = 504;
  E_UPGRADE_CONFIG_MIGRATION = 505;
  E_UPGRADE_FILES_IN_USE = 506;
  E_UPGRADE_INCOMPLETE = 507;
  E_UPGRADE_CLEANUP_FAILED = 508;
  E_UPGRADE_VERIFICATION_FAILED = 509;
  E_UPGRADE_PERMISSION_CHANGED = 510;
  E_UPGRADE_DATABASE_MIGRATION = 511;

  // E6xx: Uninstall errors
  E_UNINSTALL_GENERAL_FAILURE = 600;
  E_UNINSTALL_SERVICES_RUNNING = 601;
  E_UNINSTALL_FILES_IN_USE = 602;
  E_UNINSTALL_PERMISSION_DENIED = 603;
  E_UNINSTALL_REGISTRY_CLEANUP = 604;
  E_UNINSTALL_SERVICE_DELETE = 605;
  E_UNINSTALL_FILES_REMAINING = 606;
  E_UNINSTALL_CONFIG_PRESERVED = 607;
  E_UNINSTALL_LOG_PRESERVED = 608;
  E_UNINSTALL_INCOMPLETE = 609;

// ============================================================================
// GLOBAL VARIABLES
// ============================================================================

var
  SentinelRefID: String;
  SentinelLogFile: String;
  SentinelLogDir: String;
  SentinelSilentMode: Boolean;
  SentinelInstallStartTime: String;
  SentinelLastErrorCode: Integer;
  SentinelLastErrorMessage: String;

// ============================================================================
// REFERENCE ID GENERATION
// ============================================================================

// Generate a unique reference ID in format INS-XXXXXX-YYYYMMDD
function GenerateReferenceID: String;
var
  RandomPart: String;
  DatePart: String;
  i: Integer;
  CharSet: String;
  RandomIdx: Integer;
begin
  CharSet := '0123456789ABCDEF';
  RandomPart := '';

  // Generate 6 random hex characters using time-based seed
  for i := 1 to 6 do
  begin
    RandomIdx := (GetTickCount + Random(100)) mod 16;
    RandomPart := RandomPart + CharSet[RandomIdx + 1];
  end;

  // Get date in YYYYMMDD format
  DatePart := GetDateTimeString('yyyymmdd', #0, #0);

  Result := 'INS-' + RandomPart + '-' + DatePart;
end;

// Get the current reference ID (generates if not yet created)
function GetReferenceID: String;
begin
  if SentinelRefID = '' then
    SentinelRefID := GenerateReferenceID;
  Result := SentinelRefID;
end;

// ============================================================================
// LOGGING FUNCTIONS
// ============================================================================

// Initialize the logging system
procedure InitLogging;
begin
  // Set log directory
  SentinelLogDir := ExpandConstant('{tmp}\Sentinel');
  ForceDirectories(SentinelLogDir);

  // Create log file with reference ID
  SentinelLogFile := SentinelLogDir + '\install-' + GetReferenceID + '.log';
  SentinelInstallStartTime := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0);

  // Write log header
  SaveStringToFile(SentinelLogFile,
    '========================================' + #13#10 +
    'Sentinel Agent Installer Log' + #13#10 +
    'Reference ID: ' + SentinelRefID + #13#10 +
    'Started: ' + SentinelInstallStartTime + #13#10 +
    'Platform: Windows' + #13#10 +
    '========================================' + #13#10 + #13#10,
    False);
end;

// Write a log entry
procedure WriteLogEntry(Level, Message: String);
var
  Timestamp, LogLine: String;
begin
  Timestamp := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0);
  LogLine := Timestamp + ' | ' + Level + ' | ' + Message + #13#10;
  SaveStringToFile(SentinelLogFile, LogLine, True);
end;

// Log debug message
procedure LogDebug(Message: String);
begin
  WriteLogEntry('DEBUG', Message);
end;

// Log info message
procedure LogInfo(Message: String);
begin
  WriteLogEntry('INFO ', Message);
end;

// Log warning message
procedure LogWarn(Message: String);
begin
  WriteLogEntry('WARN ', Message);
end;

// Log error message
procedure LogError(Message: String);
begin
  WriteLogEntry('ERROR', Message);
end;

// Log error with error code
procedure LogErrorCode(ErrorCode: Integer; Message: String);
begin
  WriteLogEntry('ERROR', '[E' + IntToStr(ErrorCode) + '] ' + Message);
end;

// Log step/progress
procedure LogStep(StepNum, TotalSteps: Integer; Message: String);
begin
  WriteLogEntry('INFO ', '[' + IntToStr(StepNum) + '/' + IntToStr(TotalSteps) + '] ' + Message);
end;

// ============================================================================
// ERROR CODE DESCRIPTIONS
// ============================================================================

// Get human-readable description for error code
function GetErrorDescription(ErrorCode: Integer): String;
begin
  case ErrorCode of
    // E1xx
    100: Result := 'General installation failure';
    101: Result := 'Failed to extract installation files';
    102: Result := 'Insufficient disk space';
    103: Result := 'Permission denied';
    104: Result := 'Installation path not found';
    105: Result := 'Installation path is not writable';
    106: Result := 'Installation files are in use';
    107: Result := 'Downloaded binary is corrupted';
    108: Result := 'File checksum verification failed';
    109: Result := 'Installer architecture mismatch';
    110: Result := 'Required system components missing';
    111: Result := 'Cannot create temporary directory';
    112: Result := 'Failed to download installer components';
    113: Result := 'Installation timed out';
    114: Result := 'Failed to clean up temporary files';
    115: Result := 'Incompatible version detected';

    // E2xx
    200: Result := 'General service operation failure';
    201: Result := 'Failed to create service';
    202: Result := 'Failed to start service';
    203: Result := 'Failed to stop service';
    204: Result := 'Service with same name already exists';
    205: Result := 'Service does not exist';
    206: Result := 'Service operation timed out';
    207: Result := 'Service dependency not met';
    208: Result := 'Failed to delete service';
    209: Result := 'Insufficient privileges for service operation';
    213: Result := 'Windows service registry error';
    214: Result := 'Service started but health check failed';

    // E3xx
    300: Result := 'General configuration error';
    301: Result := 'Configuration file is not valid JSON';
    302: Result := 'Server URL not specified';
    303: Result := 'Enrollment token not specified';
    304: Result := 'Failed to write configuration file';
    305: Result := 'Failed to read configuration file';
    306: Result := 'Failed to parse configuration';
    307: Result := 'Server URL format is invalid';
    308: Result := 'Enrollment token format invalid';
    309: Result := 'Configuration file is locked';
    310: Result := 'Failed to backup existing configuration';
    311: Result := 'Failed to restore configuration from backup';

    // E4xx
    400: Result := 'General network error';
    401: Result := 'Cannot reach Sentinel server';
    402: Result := 'Enrollment token is invalid';
    403: Result := 'Enrollment token has expired';
    404: Result := 'Enrollment token has reached maximum uses';
    405: Result := 'SSL/TLS certificate verification failed';
    406: Result := 'TLS handshake failed';
    407: Result := 'Cannot resolve server hostname';
    408: Result := 'Connection to server timed out';
    409: Result := 'Server actively refused connection';
    410: Result := 'Proxy configuration error';
    416: Result := 'Device enrollment failed';

    // E5xx
    500: Result := 'General upgrade failure';
    501: Result := 'Failed to stop existing services';
    502: Result := 'Failed to backup existing installation';
    503: Result := 'Failed to rollback after upgrade failure';
    504: Result := 'Cannot downgrade to older version';
    506: Result := 'Upgrade files are in use';
    507: Result := 'Previous upgrade was incomplete';
    509: Result := 'Post-upgrade verification failed';

    // E6xx
    600: Result := 'General uninstall failure';
    601: Result := 'Cannot uninstall while services are running';
    602: Result := 'Installation files are in use';
    603: Result := 'Insufficient permissions for uninstall';
    604: Result := 'Failed to clean registry entries';
    605: Result := 'Failed to delete service';
    606: Result := 'Some files could not be deleted';
  else
    Result := 'Unknown error';
  end;
end;

// ============================================================================
// ERROR DISPLAY FUNCTIONS
// ============================================================================

// Show installer error with reference ID
procedure ShowInstallerError(ErrorCode: Integer; Message: String);
var
  FullMessage: String;
begin
  // Store error details
  SentinelLastErrorCode := ErrorCode;
  SentinelLastErrorMessage := Message;

  // Log the error
  LogErrorCode(ErrorCode, Message);

  // Build full error message
  FullMessage := 'Installation Error' + #13#10 + #13#10 +
                 '[E' + IntToStr(ErrorCode) + '] ' + Message + #13#10 + #13#10 +
                 'Description: ' + GetErrorDescription(ErrorCode) + #13#10 + #13#10 +
                 'Reference ID: ' + GetReferenceID + #13#10 +
                 'Log file: ' + SentinelLogFile + #13#10 + #13#10 +
                 'Please contact support with this information.';

  // Show message box
  if not SentinelSilentMode then
    MsgBox(FullMessage, mbError, MB_OK);
end;

// Show installer error with additional details
procedure ShowInstallerErrorWithDetails(ErrorCode: Integer; Message, Details: String);
var
  FullMessage: String;
begin
  // Store error details
  SentinelLastErrorCode := ErrorCode;
  SentinelLastErrorMessage := Message;

  // Log the error
  LogErrorCode(ErrorCode, Message);
  if Details <> '' then
    LogError('  Details: ' + Details);

  // Build full error message
  FullMessage := 'Installation Error' + #13#10 + #13#10 +
                 '[E' + IntToStr(ErrorCode) + '] ' + Message + #13#10;

  if Details <> '' then
    FullMessage := FullMessage + #13#10 + 'Details: ' + Details + #13#10;

  FullMessage := FullMessage + #13#10 +
                 'Description: ' + GetErrorDescription(ErrorCode) + #13#10 + #13#10 +
                 'Reference ID: ' + GetReferenceID + #13#10 +
                 'Log file: ' + SentinelLogFile + #13#10 + #13#10 +
                 'Please contact support with this information.';

  // Show message box
  if not SentinelSilentMode then
    MsgBox(FullMessage, mbError, MB_OK);
end;

// Show warning message (non-fatal)
procedure ShowInstallerWarning(Message: String);
var
  FullMessage: String;
begin
  // Log the warning
  LogWarn(Message);

  // Build message
  FullMessage := 'Warning' + #13#10 + #13#10 +
                 Message + #13#10 + #13#10 +
                 'Reference ID: ' + GetReferenceID;

  // Show message box
  if not SentinelSilentMode then
    MsgBox(FullMessage, mbInformation, MB_OK);
end;

// Show success dialog
procedure ShowInstallerSuccess(Message: String);
var
  FullMessage: String;
begin
  // Log success
  LogInfo('Installation completed successfully');
  LogInfo(Message);

  // Build message
  FullMessage := 'Installation Complete' + #13#10 + #13#10 +
                 Message + #13#10 + #13#10 +
                 'Reference ID: ' + GetReferenceID;

  // Show message box
  if not SentinelSilentMode then
    MsgBox(FullMessage, mbInformation, MB_OK);
end;

// ============================================================================
// PROGRESS TRACKING
// ============================================================================

var
  ProgressTotalSteps: Integer;
  ProgressCurrentStep: Integer;

// Initialize progress tracking
procedure InitProgress(TotalSteps: Integer);
begin
  ProgressTotalSteps := TotalSteps;
  ProgressCurrentStep := 0;
  LogInfo('Installation starting with ' + IntToStr(TotalSteps) + ' steps');
end;

// Update progress
procedure UpdateProgress(Message: String);
begin
  ProgressCurrentStep := ProgressCurrentStep + 1;
  LogStep(ProgressCurrentStep, ProgressTotalSteps, Message);
end;

// Complete progress
procedure CompleteProgress;
begin
  LogInfo('All installation steps completed');
end;

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// Check if sufficient disk space is available
function CheckDiskSpace(Path: String; RequiredMB: Integer): Boolean;
var
  FreeBytes: Int64;
begin
  Result := True;

  // Get free space
  if GetSpaceOnDisk(ExtractFileDrive(Path), True, FreeBytes, FreeBytes) then
  begin
    if FreeBytes < (RequiredMB * 1024 * 1024) then
    begin
      ShowInstallerErrorWithDetails(E_DISK_SPACE_INSUFFICIENT,
        'Insufficient disk space',
        'Required: ' + IntToStr(RequiredMB) + ' MB, Available: ' + IntToStr(FreeBytes div (1024 * 1024)) + ' MB');
      Result := False;
    end;
  end;
end;

// Check if running with administrator privileges
function CheckAdminPrivileges: Boolean;
begin
  Result := IsAdmin;
  if not Result then
  begin
    ShowInstallerError(E_PERMISSION_DENIED,
      'Administrator privileges required');
  end;
end;

// Finalize logging (call at end of install)
procedure FinalizeLogging(Success: Boolean);
var
  EndTime, Duration: String;
begin
  EndTime := GetDateTimeString('yyyy-mm-dd hh:nn:ss', #0, #0);

  SaveStringToFile(SentinelLogFile,
    #13#10 + '========================================' + #13#10,
    True);

  if Success then
    SaveStringToFile(SentinelLogFile, 'INSTALLATION COMPLETED SUCCESSFULLY' + #13#10, True)
  else
    SaveStringToFile(SentinelLogFile, 'INSTALLATION FAILED' + #13#10, True);

  SaveStringToFile(SentinelLogFile,
    'End Time: ' + EndTime + #13#10 +
    'Reference ID: ' + SentinelRefID + #13#10 +
    '========================================' + #13#10,
    True);
end;

// ============================================================================
// INITIALIZATION
// ============================================================================

// Initialize the error handler (call early in InitializeSetup)
procedure InitErrorHandler;
begin
  // Generate reference ID
  SentinelRefID := GenerateReferenceID;

  // Initialize logging
  InitLogging;

  // Check for silent mode
  SentinelSilentMode := WizardSilent;

  // Initialize error tracking
  SentinelLastErrorCode := 0;
  SentinelLastErrorMessage := '';

  // Log initialization
  LogInfo('Error handler initialized');
  LogInfo('Reference ID: ' + SentinelRefID);
  LogInfo('Silent mode: ' + BoolToStr(SentinelSilentMode));
end;

// Get the last error code (for exit code handling)
function GetLastErrorCode: Integer;
begin
  Result := SentinelLastErrorCode;
end;

// Get the last error message
function GetLastErrorMessage: String;
begin
  Result := SentinelLastErrorMessage;
end;

// Get the log file path
function GetLogFilePath: String;
begin
  Result := SentinelLogFile;
end;

// BoolToStr helper (Inno Setup doesn't have this built-in)
function BoolToStr(Value: Boolean): String;
begin
  if Value then
    Result := 'True'
  else
    Result := 'False';
end;
