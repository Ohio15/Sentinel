{
  Sentinel Config Reader
  Pascal helper code for reading embedded configuration from installer binary

  This file contains additional Pascal procedures that can be included in the
  main Inno Setup script if needed. The primary config reading logic is
  already embedded in sentinel-setup.iss.

  Config Format:
  The installer binary has JSON config appended after the marker:
  ---SENTINEL-CONFIG---
  {"server_url":"...","grpc_endpoint":"...","enrollment_token":"...","organization_id":"..."}
}

const
  CONFIG_MARKER = '---SENTINEL-CONFIG---';
  MAX_CONFIG_SIZE = 8192;  // Max 8KB for config

type
  TConfigData = record
    ServerURL: String;
    GrpcEndpoint: String;
    EnrollmentToken: String;
    OrganizationId: String;
    RawJson: String;
    IsValid: Boolean;
  end;

var
  GlobalConfig: TConfigData;

// Parse JSON value (simple parser for our known structure)
function ExtractJsonValue(const Json, Key: String): String;
var
  SearchKey: String;
  StartPos, EndPos: Integer;
  InString: Boolean;
  Ch: Char;
begin
  Result := '';
  SearchKey := '"' + Key + '":';

  StartPos := Pos(SearchKey, Json);
  if StartPos = 0 then
    Exit;

  StartPos := StartPos + Length(SearchKey);

  // Skip whitespace
  while (StartPos <= Length(Json)) and ((Json[StartPos] = ' ') or (Json[StartPos] = #9)) do
    StartPos := StartPos + 1;

  if StartPos > Length(Json) then
    Exit;

  // Check if value is quoted string
  if Json[StartPos] = '"' then
  begin
    StartPos := StartPos + 1;
    EndPos := StartPos;

    // Find closing quote (handle escaped quotes)
    while EndPos <= Length(Json) do
    begin
      if (Json[EndPos] = '"') and ((EndPos = 1) or (Json[EndPos - 1] <> '\')) then
      begin
        Result := Copy(Json, StartPos, EndPos - StartPos);
        Exit;
      end;
      EndPos := EndPos + 1;
    end;
  end
  else
  begin
    // Unquoted value (number, boolean, null)
    EndPos := StartPos;
    while (EndPos <= Length(Json)) and (Json[EndPos] <> ',') and (Json[EndPos] <> '}') do
      EndPos := EndPos + 1;

    Result := Trim(Copy(Json, StartPos, EndPos - StartPos));
  end;
end;

// Parse the config JSON into structured data
function ParseConfigJson(const JsonStr: String): TConfigData;
begin
  Result.RawJson := JsonStr;
  Result.IsValid := False;

  if JsonStr = '' then
    Exit;

  Result.ServerURL := ExtractJsonValue(JsonStr, 'server_url');
  Result.GrpcEndpoint := ExtractJsonValue(JsonStr, 'grpc_endpoint');
  Result.EnrollmentToken := ExtractJsonValue(JsonStr, 'enrollment_token');
  Result.OrganizationId := ExtractJsonValue(JsonStr, 'organization_id');

  // Validate required fields
  if (Result.ServerURL <> '') and (Result.GrpcEndpoint <> '') then
    Result.IsValid := True;
end;

// Read config from binary file using PowerShell
function ExtractConfigFromBinary(const BinaryPath: String; var Config: TConfigData): Boolean;
var
  ResultCode: Integer;
  TempPath: String;
  JsonContent: AnsiString;
  PSCommand: String;
begin
  Result := False;
  TempPath := ExpandConstant('{tmp}\sentinel-config-extracted.json');

  // PowerShell command to extract config after marker
  PSCommand :=
    '$ErrorActionPreference = "Stop"; ' +
    'try { ' +
    '  $bytes = [System.IO.File]::ReadAllBytes("' + BinaryPath + '"); ' +
    '  $text = [System.Text.Encoding]::UTF8.GetString($bytes); ' +
    '  $marker = "' + CONFIG_MARKER + '"; ' +
    '  $idx = $text.LastIndexOf($marker); ' +
    '  if ($idx -ge 0) { ' +
    '    $configStart = $idx + $marker.Length; ' +
    '    $config = $text.Substring($configStart).Trim(); ' +
    '    if ($config.StartsWith("{")) { ' +
    '      $endBrace = $config.IndexOf("}"); ' +
    '      if ($endBrace -gt 0) { ' +
    '        $config = $config.Substring(0, $endBrace + 1); ' +
    '      } ' +
    '    } ' +
    '    [System.IO.File]::WriteAllText("' + TempPath + '", $config); ' +
    '    exit 0; ' +
    '  } else { exit 1; } ' +
    '} catch { exit 2; }';

  if Exec('powershell.exe',
          '-NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -Command "' + PSCommand + '"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    if (ResultCode = 0) and FileExists(TempPath) then
    begin
      if LoadStringFromFile(TempPath, JsonContent) then
      begin
        Config := ParseConfigJson(String(JsonContent));
        Result := Config.IsValid;
        DeleteFile(TempPath);
      end;
    end;
  end;
end;

// Validate config data
function ValidateConfig(const Config: TConfigData): Boolean;
begin
  Result := Config.IsValid;

  if not Result then
    Exit;

  // Additional validation
  if Length(Config.ServerURL) < 10 then  // At least "https://x"
    Result := False
  else if (Pos('http://', Config.ServerURL) <> 1) and (Pos('https://', Config.ServerURL) <> 1) then
    Result := False;

  if Length(Config.GrpcEndpoint) < 5 then  // At least "x:123"
    Result := False;
end;

// Create default config structure
function CreateDefaultConfig(ServerURL, GrpcEndpoint, Token, OrgId: String): String;
begin
  Result := '{' + #13#10 +
            '  "server_url": "' + ServerURL + '",' + #13#10 +
            '  "grpc_endpoint": "' + GrpcEndpoint + '",' + #13#10 +
            '  "enrollment_token": "' + Token + '",' + #13#10 +
            '  "organization_id": "' + OrgId + '"' + #13#10 +
            '}';
end;

// Format config for display (hide sensitive parts of token)
function FormatConfigForDisplay(const Config: TConfigData): String;
var
  MaskedToken: String;
begin
  if Length(Config.EnrollmentToken) > 8 then
    MaskedToken := Copy(Config.EnrollmentToken, 1, 4) + '****' + Copy(Config.EnrollmentToken, Length(Config.EnrollmentToken) - 3, 4)
  else
    MaskedToken := '********';

  Result := 'Server: ' + Config.ServerURL + #13#10 +
            'gRPC: ' + Config.GrpcEndpoint + #13#10 +
            'Token: ' + MaskedToken + #13#10 +
            'Org ID: ' + Config.OrganizationId;
end;

// Write config to file with proper formatting
function WriteConfigToFile(const FilePath: String; const Config: TConfigData): Boolean;
var
  FormattedJson: String;
begin
  Result := False;

  if not Config.IsValid then
    Exit;

  // Format JSON nicely
  FormattedJson := '{' + #13#10 +
                   '  "server_url": "' + Config.ServerURL + '",' + #13#10 +
                   '  "grpc_endpoint": "' + Config.GrpcEndpoint + '",' + #13#10 +
                   '  "enrollment_token": "' + Config.EnrollmentToken + '",' + #13#10 +
                   '  "organization_id": "' + Config.OrganizationId + '"' + #13#10 +
                   '}';

  Result := SaveStringToFile(FilePath, FormattedJson, False);
end;

// Read existing config from file
function ReadConfigFromFile(const FilePath: String; var Config: TConfigData): Boolean;
var
  Content: AnsiString;
begin
  Result := False;

  if not FileExists(FilePath) then
    Exit;

  if LoadStringFromFile(FilePath, Content) then
  begin
    Config := ParseConfigJson(String(Content));
    Result := Config.IsValid;
  end;
end;

// Merge configs (new overrides old, but preserves missing fields)
function MergeConfigs(const OldConfig, NewConfig: TConfigData): TConfigData;
begin
  Result := NewConfig;

  // If new config is missing fields, use old config values
  if Result.ServerURL = '' then
    Result.ServerURL := OldConfig.ServerURL;

  if Result.GrpcEndpoint = '' then
    Result.GrpcEndpoint := OldConfig.GrpcEndpoint;

  if Result.EnrollmentToken = '' then
    Result.EnrollmentToken := OldConfig.EnrollmentToken;

  if Result.OrganizationId = '' then
    Result.OrganizationId := OldConfig.OrganizationId;

  // Regenerate JSON
  Result.RawJson := CreateDefaultConfig(Result.ServerURL, Result.GrpcEndpoint,
                                         Result.EnrollmentToken, Result.OrganizationId);

  Result.IsValid := (Result.ServerURL <> '') and (Result.GrpcEndpoint <> '');
end;
