-- Preset device management scripts
-- These scripts are seeded on first run to provide useful out-of-the-box functionality

-- System Information (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'System Information', 'Get detailed system information including OS, hardware, and memory', 'powershell',
'$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_Processor
$mem = Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum

Write-Output "=== SYSTEM INFORMATION ==="
Write-Output "Computer Name: $env:COMPUTERNAME"
Write-Output "OS: $($os.Caption) $($os.Version)"
Write-Output "Build: $($os.BuildNumber)"
Write-Output "Architecture: $($os.OSArchitecture)"
Write-Output ""
Write-Output "=== HARDWARE ==="
Write-Output "CPU: $($cpu.Name)"
Write-Output "Cores: $($cpu.NumberOfCores) | Threads: $($cpu.NumberOfLogicalProcessors)"
Write-Output "RAM: $([math]::Round($mem.Sum / 1GB, 2)) GB"
Write-Output "Free RAM: $([math]::Round($os.FreePhysicalMemory / 1MB, 2)) GB"
Write-Output ""
Write-Output "=== DISK USAGE ==="
Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object {
    $free = [math]::Round($_.FreeSpace / 1GB, 2)
    $total = [math]::Round($_.Size / 1GB, 2)
    $used = $total - $free
    Write-Output "$($_.DeviceID) - Used: $used GB / $total GB (Free: $free GB)"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Disk Cleanup (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Disk Cleanup', 'Clear temporary files, Windows Update cache, and empty Recycle Bin', 'powershell',
'Write-Output "Starting disk cleanup..."

# Clear Windows Temp
$tempPath = "$env:TEMP"
$tempSize = (Get-ChildItem $tempPath -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum / 1MB
Remove-Item "$tempPath\*" -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "Cleared Windows Temp: $([math]::Round($tempSize, 2)) MB"

# Clear Windows Update Cache
Stop-Service wuauserv -Force -ErrorAction SilentlyContinue
$wuPath = "C:\Windows\SoftwareDistribution\Download"
$wuSize = (Get-ChildItem $wuPath -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum / 1MB
Remove-Item "$wuPath\*" -Recurse -Force -ErrorAction SilentlyContinue
Start-Service wuauserv -ErrorAction SilentlyContinue
Write-Output "Cleared Windows Update Cache: $([math]::Round($wuSize, 2)) MB"

# Empty Recycle Bin
$shell = New-Object -ComObject Shell.Application
$recycleBin = $shell.NameSpace(0xA)
$rbSize = ($recycleBin.Items() | Measure-Object -Property Size -Sum).Sum / 1MB
Clear-RecycleBin -Force -ErrorAction SilentlyContinue
Write-Output "Emptied Recycle Bin: $([math]::Round($rbSize, 2)) MB"

# Clear browser caches
$chromePath = "$env:LOCALAPPDATA\Google\Chrome\User Data\Default\Cache"
if (Test-Path $chromePath) {
    $chromeSize = (Get-ChildItem $chromePath -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum / 1MB
    Remove-Item "$chromePath\*" -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output "Cleared Chrome Cache: $([math]::Round($chromeSize, 2)) MB"
}

Write-Output ""
Write-Output "Disk cleanup completed!"', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Check Windows Updates (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Check Windows Updates', 'List pending Windows updates and last update time', 'powershell',
'Write-Output "=== WINDOWS UPDATE STATUS ==="

# Get last update time
$lastUpdate = Get-HotFix | Sort-Object InstalledOn -Descending | Select-Object -First 1
Write-Output "Last Update Installed: $($lastUpdate.InstalledOn) - $($lastUpdate.HotFixID)"
Write-Output ""

# Check for pending updates
$UpdateSession = New-Object -ComObject Microsoft.Update.Session
$UpdateSearcher = $UpdateSession.CreateUpdateSearcher()

Write-Output "Searching for pending updates..."
$SearchResult = $UpdateSearcher.Search("IsInstalled=0 and Type=''Software''")

if ($SearchResult.Updates.Count -eq 0) {
    Write-Output "No pending updates found. System is up to date!"
} else {
    Write-Output "Found $($SearchResult.Updates.Count) pending update(s):"
    Write-Output ""
    foreach ($Update in $SearchResult.Updates) {
        $size = [math]::Round($Update.MaxDownloadSize / 1MB, 2)
        Write-Output "- $($Update.Title)"
        Write-Output "  Size: $size MB | Severity: $($Update.MsrcSeverity)"
    }
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- List Installed Software (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'List Installed Software', 'Get list of all installed applications with versions', 'powershell',
'Write-Output "=== INSTALLED SOFTWARE ==="
Write-Output ""

$software = Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*,
    HKLM:\Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall\* -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -and $_.DisplayName -notmatch "Update|Hotfix|KB\d+" } |
    Select-Object DisplayName, DisplayVersion, Publisher, InstallDate |
    Sort-Object DisplayName

$software | ForEach-Object {
    $date = if ($_.InstallDate) { $_.InstallDate } else { "Unknown" }
    Write-Output "$($_.DisplayName)"
    Write-Output "  Version: $($_.DisplayVersion) | Publisher: $($_.Publisher) | Installed: $date"
}

Write-Output ""
Write-Output "Total: $($software.Count) applications"', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Network Configuration (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Network Configuration', 'Display network adapter settings, IP addresses, and DNS configuration', 'powershell',
'Write-Output "=== NETWORK CONFIGURATION ==="
Write-Output ""

Get-NetAdapter | Where-Object Status -eq "Up" | ForEach-Object {
    $adapter = $_
    $config = Get-NetIPConfiguration -InterfaceIndex $adapter.ifIndex
    $dns = Get-DnsClientServerAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4

    Write-Output "Adapter: $($adapter.Name) ($($adapter.InterfaceDescription))"
    Write-Output "  Status: $($adapter.Status) | Speed: $($adapter.LinkSpeed)"
    Write-Output "  MAC: $($adapter.MacAddress)"
    Write-Output "  IPv4: $($config.IPv4Address.IPAddress)"
    Write-Output "  Gateway: $($config.IPv4DefaultGateway.NextHop)"
    Write-Output "  DNS: $($dns.ServerAddresses -join '', '')"
    Write-Output ""
}

Write-Output "=== PUBLIC IP ==="
try {
    $publicIP = (Invoke-WebRequest -Uri "https://api.ipify.org" -UseBasicParsing -TimeoutSec 5).Content
    Write-Output "Public IP: $publicIP"
} catch {
    Write-Output "Could not determine public IP"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Running Services (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Running Services', 'List all running Windows services', 'powershell',
'Write-Output "=== RUNNING SERVICES ==="
Write-Output ""

$services = Get-Service | Where-Object Status -eq "Running" | Sort-Object DisplayName

$services | ForEach-Object {
    Write-Output "$($_.DisplayName)"
    Write-Output "  Name: $($_.Name) | Status: $($_.Status) | StartType: $($_.StartType)"
}

Write-Output ""
Write-Output "Total: $($services.Count) running services"', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Recent Event Log Errors (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Recent Event Log Errors', 'Get recent error and critical events from System and Application logs', 'powershell',
'Write-Output "=== RECENT EVENT LOG ERRORS (Last 24 Hours) ==="
Write-Output ""

$startTime = (Get-Date).AddHours(-24)

Write-Output "--- SYSTEM LOG ---"
Get-WinEvent -FilterHashtable @{LogName="System"; Level=1,2; StartTime=$startTime} -MaxEvents 20 -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Output "[$($_.TimeCreated)] $($_.ProviderName)"
    Write-Output "  Level: $($_.LevelDisplayName) | EventID: $($_.Id)"
    $msg = $_.Message -replace "`r`n", " " | Select-Object -First 200
    Write-Output "  $($msg.Substring(0, [Math]::Min(200, $msg.Length)))..."
    Write-Output ""
}

Write-Output "--- APPLICATION LOG ---"
Get-WinEvent -FilterHashtable @{LogName="Application"; Level=1,2; StartTime=$startTime} -MaxEvents 20 -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Output "[$($_.TimeCreated)] $($_.ProviderName)"
    Write-Output "  Level: $($_.LevelDisplayName) | EventID: $($_.Id)"
    $msg = $_.Message -replace "`r`n", " " | Select-Object -First 200
    Write-Output "  $($msg.Substring(0, [Math]::Min(200, $msg.Length)))..."
    Write-Output ""
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Restart Computer (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Restart Computer', 'Restart the computer with a 60-second warning', 'powershell',
'Write-Output "Initiating system restart in 60 seconds..."
Write-Output "Users will be notified."
shutdown /r /t 60 /c "System restart initiated by administrator via Sentinel RMM"
Write-Output "Restart scheduled. Use ''shutdown /a'' to abort."', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Clear DNS Cache (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Clear DNS Cache', 'Flush the DNS resolver cache and display results', 'powershell',
'Write-Output "=== DNS CACHE FLUSH ==="
Write-Output ""
Write-Output "Current DNS cache entries:"
$cacheCount = (Get-DnsClientCache | Measure-Object).Count
Write-Output "Total cached entries: $cacheCount"
Write-Output ""
Write-Output "Flushing DNS cache..."
Clear-DnsClientCache
Write-Output "DNS cache cleared successfully!"
Write-Output ""
Write-Output "Verifying..."
$newCount = (Get-DnsClientCache | Measure-Object).Count
Write-Output "Cached entries after flush: $newCount"', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- System Uptime (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'System Uptime', 'Display system uptime and last boot time', 'powershell',
'$os = Get-CimInstance Win32_OperatingSystem
$bootTime = $os.LastBootUpTime
$uptime = (Get-Date) - $bootTime

Write-Output "=== SYSTEM UPTIME ==="
Write-Output ""
Write-Output "Last Boot Time: $bootTime"
Write-Output "Uptime: $($uptime.Days) days, $($uptime.Hours) hours, $($uptime.Minutes) minutes"
Write-Output ""
Write-Output "System has been running for $([math]::Round($uptime.TotalHours, 1)) hours"', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Top Processes by Memory (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Top Processes by Memory', 'List top 20 processes consuming the most memory', 'powershell',
'Write-Output "=== TOP 20 PROCESSES BY MEMORY USAGE ==="
Write-Output ""

Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 20 | ForEach-Object {
    $memMB = [math]::Round($_.WorkingSet64 / 1MB, 2)
    $cpu = [math]::Round($_.CPU, 2)
    Write-Output "$($_.ProcessName) (PID: $($_.Id))"
    Write-Output "  Memory: $memMB MB | CPU Time: $cpu seconds"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Firewall Status (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Firewall Status', 'Check Windows Firewall status for all profiles', 'powershell',
'Write-Output "=== WINDOWS FIREWALL STATUS ==="
Write-Output ""

Get-NetFirewallProfile | ForEach-Object {
    $status = if ($_.Enabled) { "ENABLED" } else { "DISABLED" }
    Write-Output "$($_.Name) Profile: $status"
    Write-Output "  Default Inbound: $($_.DefaultInboundAction)"
    Write-Output "  Default Outbound: $($_.DefaultOutboundAction)"
    Write-Output "  Log Allowed: $($_.LogAllowed) | Log Blocked: $($_.LogBlocked)"
    Write-Output ""
}

Write-Output "=== RECENT FIREWALL RULES (Last 10 Added) ==="
Get-NetFirewallRule | Where-Object Enabled -eq "True" |
    Sort-Object -Descending | Select-Object -First 10 | ForEach-Object {
    Write-Output "- $($_.DisplayName)"
    Write-Output "  Direction: $($_.Direction) | Action: $($_.Action)"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Windows Defender Status (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Windows Defender Status', 'Check Windows Defender antivirus status and definitions', 'powershell',
'Write-Output "=== WINDOWS DEFENDER STATUS ==="
Write-Output ""

$mpStatus = Get-MpComputerStatus

Write-Output "Real-Time Protection: $(if ($mpStatus.RealTimeProtectionEnabled) { ''ENABLED'' } else { ''DISABLED'' })"
Write-Output "Antivirus Enabled: $(if ($mpStatus.AntivirusEnabled) { ''YES'' } else { ''NO'' })"
Write-Output "Antispyware Enabled: $(if ($mpStatus.AntispywareEnabled) { ''YES'' } else { ''NO'' })"
Write-Output ""
Write-Output "=== DEFINITION STATUS ==="
Write-Output "Antivirus Signature Version: $($mpStatus.AntivirusSignatureVersion)"
Write-Output "Antivirus Signature Age: $($mpStatus.AntivirusSignatureAge) days"
Write-Output "Last Signature Update: $($mpStatus.AntivirusSignatureLastUpdated)"
Write-Output ""
Write-Output "=== SCAN STATUS ==="
Write-Output "Last Quick Scan: $($mpStatus.QuickScanEndTime)"
Write-Output "Last Full Scan: $($mpStatus.FullScanEndTime)"

if ($mpStatus.AntivirusSignatureAge -gt 7) {
    Write-Output ""
    Write-Output "WARNING: Antivirus definitions are more than 7 days old!"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Local User Accounts (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Local User Accounts', 'List all local user accounts and their status', 'powershell',
'Write-Output "=== LOCAL USER ACCOUNTS ==="
Write-Output ""

Get-LocalUser | ForEach-Object {
    $status = if ($_.Enabled) { "Enabled" } else { "Disabled" }
    $lastLogon = if ($_.LastLogon) { $_.LastLogon } else { "Never" }

    Write-Output "$($_.Name)"
    Write-Output "  Status: $status"
    Write-Output "  Full Name: $($_.FullName)"
    Write-Output "  Description: $($_.Description)"
    Write-Output "  Last Logon: $lastLogon"
    Write-Output "  Password Required: $($_.PasswordRequired)"
    Write-Output "  Password Expires: $(if ($_.PasswordExpires) { $_.PasswordExpires } else { ''Never'' })"
    Write-Output ""
}

Write-Output "=== LOCAL ADMINISTRATORS ==="
Get-LocalGroupMember -Group "Administrators" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Output "- $($_.Name) ($($_.ObjectClass))"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Disk Health Check (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Disk Health Check', 'Check disk health status using SMART data and volume info', 'powershell',
'Write-Output "=== DISK HEALTH CHECK ==="
Write-Output ""

# Physical Disks
Write-Output "--- PHYSICAL DISKS ---"
Get-PhysicalDisk | ForEach-Object {
    $health = $_.HealthStatus
    $status = if ($health -eq "Healthy") { "OK" } else { "WARNING: $health" }

    Write-Output "$($_.FriendlyName)"
    Write-Output "  Model: $($_.Model)"
    Write-Output "  Size: $([math]::Round($_.Size / 1GB, 2)) GB"
    Write-Output "  Media Type: $($_.MediaType)"
    Write-Output "  Health: $status"
    Write-Output "  Operational Status: $($_.OperationalStatus)"
    Write-Output ""
}

# Volumes
Write-Output "--- VOLUME STATUS ---"
Get-Volume | Where-Object DriveLetter | ForEach-Object {
    $free = [math]::Round($_.SizeRemaining / 1GB, 2)
    $total = [math]::Round($_.Size / 1GB, 2)
    $pctFree = if ($total -gt 0) { [math]::Round(($free / $total) * 100, 1) } else { 0 }
    $warning = if ($pctFree -lt 10) { " [LOW SPACE WARNING]" } else { "" }

    Write-Output "$($_.DriveLetter): $($_.FileSystemLabel)"
    Write-Output "  Free: $free GB / $total GB ($pctFree% free)$warning"
    Write-Output "  Health: $($_.HealthStatus)"
    Write-Output ""
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Startup Programs (Windows)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Startup Programs', 'List all programs configured to run at startup', 'powershell',
'Write-Output "=== STARTUP PROGRAMS ==="
Write-Output ""

Write-Output "--- REGISTRY (HKLM Run) ---"
Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run" -ErrorAction SilentlyContinue |
    Get-Member -MemberType NoteProperty | Where-Object { $_.Name -notmatch "^PS" } | ForEach-Object {
    $val = (Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run").$($_.Name)
    Write-Output "- $($_.Name)"
    Write-Output "  $val"
}

Write-Output ""
Write-Output "--- REGISTRY (HKCU Run) ---"
Get-ItemProperty "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run" -ErrorAction SilentlyContinue |
    Get-Member -MemberType NoteProperty | Where-Object { $_.Name -notmatch "^PS" } | ForEach-Object {
    $val = (Get-ItemProperty "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run").$($_.Name)
    Write-Output "- $($_.Name)"
    Write-Output "  $val"
}

Write-Output ""
Write-Output "--- STARTUP FOLDER ---"
$startupPath = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup"
Get-ChildItem $startupPath -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Output "- $($_.Name)"
}', '["windows"]'::jsonb)
ON CONFLICT DO NOTHING;

-- System Information (Linux)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'System Information (Linux)', 'Get detailed system information for Linux systems', 'bash',
'#!/bin/bash
echo "=== SYSTEM INFORMATION ==="
echo ""
echo "Hostname: $(hostname)"
echo "OS: $(cat /etc/os-release | grep PRETTY_NAME | cut -d= -f2 | tr -d ''"'')"
echo "Kernel: $(uname -r)"
echo "Architecture: $(uname -m)"
echo ""
echo "=== HARDWARE ==="
echo "CPU: $(grep "model name" /proc/cpuinfo | head -1 | cut -d: -f2 | xargs)"
echo "Cores: $(nproc)"
echo "RAM: $(free -h | grep Mem | awk ''{print $2}'')"
echo "Free RAM: $(free -h | grep Mem | awk ''{print $4}'')"
echo ""
echo "=== DISK USAGE ==="
df -h | grep -E "^/dev" | awk ''{print $1 " - Used: " $3 " / " $2 " (" $5 " used)"}"
echo ""
echo "=== UPTIME ==="
uptime -p', '["linux"]'::jsonb)
ON CONFLICT DO NOTHING;

-- Network Configuration (Linux)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'Network Configuration (Linux)', 'Display network configuration for Linux systems', 'bash',
'#!/bin/bash
echo "=== NETWORK CONFIGURATION ==="
echo ""
echo "--- INTERFACES ---"
ip -br addr show
echo ""
echo "--- ROUTING TABLE ---"
ip route
echo ""
echo "--- DNS SERVERS ---"
cat /etc/resolv.conf | grep nameserver
echo ""
echo "--- PUBLIC IP ---"
curl -s https://api.ipify.org && echo ""', '["linux"]'::jsonb)
ON CONFLICT DO NOTHING;

-- System Information (macOS)
INSERT INTO scripts (id, name, description, language, content, os_types) VALUES
(uuid_generate_v4(), 'System Information (macOS)', 'Get detailed system information for macOS', 'bash',
'#!/bin/bash
echo "=== SYSTEM INFORMATION ==="
echo ""
echo "Hostname: $(hostname)"
echo "OS: $(sw_vers -productName) $(sw_vers -productVersion)"
echo "Build: $(sw_vers -buildVersion)"
echo "Architecture: $(uname -m)"
echo ""
echo "=== HARDWARE ==="
echo "CPU: $(sysctl -n machdep.cpu.brand_string)"
echo "Cores: $(sysctl -n hw.ncpu)"
echo "RAM: $(( $(sysctl -n hw.memsize) / 1073741824 )) GB"
echo ""
echo "=== DISK USAGE ==="
df -h | grep -E "^/dev"
echo ""
echo "=== UPTIME ==="
uptime', '["macos"]'::jsonb)
ON CONFLICT DO NOTHING;
