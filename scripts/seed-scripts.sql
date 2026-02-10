-- Comprehensive RMM Script Library
-- Based on industry best practices for device management
-- Uses dollar-quoted strings to avoid escaping issues

-- ============================================
-- NETWORK DISCOVERY & SCANNING
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Device Discovery',
  'Scans local network subnet for active devices using ping sweep',
  'powershell',
  $script$
$subnet = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike "127.*" } | Select-Object -First 1).IPAddress -replace "\.\d+$", ""
$results = @()
1..254 | ForEach-Object -Parallel {
    $ip = "$using:subnet.$_"
    if (Test-Connection -ComputerName $ip -Count 1 -Quiet -TimeoutSeconds 1) {
        try {
            $hostname = [System.Net.Dns]::GetHostEntry($ip).HostName
        } catch { $hostname = "Unknown" }
        [PSCustomObject]@{ IP = $ip; Hostname = $hostname; Status = "Online" }
    }
} -ThrottleLimit 50 | Sort-Object { [version]$_.IP }
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Device Discovery (Linux)',
  'Scans local network subnet for active devices',
  'bash',
  $script$
#!/bin/bash
SUBNET=$(ip route | grep -v default | grep -oP "^\d+\.\d+\.\d+" | head -1)
echo "Scanning $SUBNET.0/24..."
echo "IP Address       Hostname"
echo "----------------------------------------"
for i in {1..254}; do
    (ping -c 1 -W 1 "$SUBNET.$i" >/dev/null 2>&1 && \
     host=$(getent hosts "$SUBNET.$i" 2>/dev/null | awk '{print $2}') && \
     echo "$SUBNET.$i    ${host:-Unknown}") &
done
wait
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Port Scanner - Common Ports',
  'Scans a target host for commonly used ports',
  'powershell',
  $script$
param([string]$Target = "localhost")
$ports = @(21,22,23,25,53,80,110,135,139,143,443,445,993,995,1433,1521,3306,3389,5432,5900,8080,8443)
$results = foreach ($port in $ports) {
    $tcp = New-Object System.Net.Sockets.TcpClient
    try {
        $connect = $tcp.BeginConnect($Target, $port, $null, $null)
        $wait = $connect.AsyncWaitHandle.WaitOne(500, $false)
        if ($wait -and $tcp.Connected) {
            [PSCustomObject]@{ Port = $port; Status = "Open" }
        }
    } catch {} finally { $tcp.Close() }
}
$results | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Open Ports on This Machine',
  'Lists all open listening ports on the local machine',
  'powershell',
  $script$
Get-NetTCPConnection -State Listen | Select-Object LocalAddress, LocalPort,
    @{Name="Process";Expression={(Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue).ProcessName}},
    @{Name="PID";Expression={$_.OwningProcess}} |
Sort-Object LocalPort | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Open Ports (Linux)',
  'Lists all open listening ports',
  'bash',
  $script$
#!/bin/bash
echo "=== Listening Ports ==="
ss -tuln | grep LISTEN
echo ""
echo "=== With Process Names ==="
ss -tulnp 2>/dev/null | grep LISTEN || netstat -tulnp 2>/dev/null | grep LISTEN
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Shares Enumeration',
  'Lists all network shares accessible from this machine',
  'powershell',
  $script$
Write-Host "=== Mapped Network Drives ===" -ForegroundColor Cyan
Get-PSDrive -PSProvider FileSystem | Where-Object { $_.DisplayRoot } |
    Select-Object Name, @{N="Path";E={$_.DisplayRoot}} | Format-Table -AutoSize

Write-Host "`n=== SMB Shares on Local Machine ===" -ForegroundColor Cyan
Get-SmbShare | Select-Object Name, Path, Description | Format-Table -AutoSize

Write-Host "`n=== Current SMB Connections ===" -ForegroundColor Cyan
Get-SmbConnection | Select-Object ServerName, ShareName, UserName | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'DNS Cache Viewer',
  'Displays the local DNS resolver cache',
  'powershell',
  $script$
Get-DnsClientCache | Select-Object Entry, RecordName, RecordType, TimeToLive, Data |
    Sort-Object Entry | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Interface Details',
  'Shows detailed network adapter configuration',
  'powershell',
  $script$
Get-NetAdapter | Where-Object Status -eq 'Up' | ForEach-Object {
    $adapter = $_
    $config = Get-NetIPConfiguration -InterfaceIndex $adapter.InterfaceIndex
    [PSCustomObject]@{
        Name = $adapter.Name
        Status = $adapter.Status
        Speed = "$([math]::Round($adapter.LinkSpeed / 1000000))Mbps"
        MAC = $adapter.MacAddress
        IPv4 = ($config.IPv4Address.IPAddress -join ', ')
        Gateway = $config.IPv4DefaultGateway.NextHop
        DNS = ($config.DNSServer.ServerAddresses -join ', ')
    }
} | Format-List
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- PRINTER MANAGEMENT
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'List All Printers',
  'Lists all installed printers with their status',
  'powershell',
  $script$
Get-Printer | Select-Object Name, DriverName, PortName, PrinterStatus, Shared |
    Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'List All Printers (Linux)',
  'Lists all configured printers via CUPS',
  'bash',
  $script$
#!/bin/bash
echo "=== Installed Printers ==="
lpstat -p -d 2>/dev/null || echo "CUPS not installed or not running"
echo ""
echo "=== Printer Queue Status ==="
lpstat -o 2>/dev/null || echo "No jobs in queue"
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Printer Queue Management',
  'Shows and clears print queue for all printers',
  'powershell',
  $script$
Write-Host "=== Current Print Jobs ===" -ForegroundColor Cyan
Get-PrintJob -PrinterName * | Select-Object PrinterName, DocumentName, UserName,
    SubmittedTime, JobStatus, Size | Format-Table -AutoSize

$choice = Read-Host "Clear all print jobs? (y/n)"
if ($choice -eq 'y') {
    Get-Printer | ForEach-Object {
        Get-PrintJob -PrinterName $_.Name | Remove-PrintJob
    }
    Write-Host "All print jobs cleared" -ForegroundColor Green
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Printer Discovery',
  'Discovers network printers using common ports',
  'powershell',
  $script$
$subnet = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike "127.*" } | Select-Object -First 1).IPAddress -replace "\.\d+$", ""
Write-Host "Scanning $subnet.0/24 for printers..." -ForegroundColor Cyan

$printerPorts = @(9100, 515, 631)
$results = 1..254 | ForEach-Object -Parallel {
    $ip = "$using:subnet.$_"
    foreach ($port in $using:printerPorts) {
        $tcp = New-Object System.Net.Sockets.TcpClient
        try {
            $connect = $tcp.BeginConnect($ip, $port, $null, $null)
            if ($connect.AsyncWaitHandle.WaitOne(200, $false) -and $tcp.Connected) {
                [PSCustomObject]@{ IP = $ip; Port = $port; Protocol = switch($port){9100{"RAW"}515{"LPR"}631{"IPP"}} }
            }
        } catch {} finally { $tcp.Close() }
    }
} -ThrottleLimit 50

$results | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Printer Driver List',
  'Lists all installed printer drivers',
  'powershell',
  $script$
Get-PrinterDriver | Select-Object Name, Manufacturer,
    @{N="Version";E={$_.DriverVersion}},
    @{N="InfPath";E={$_.InfPath}} | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- SECURITY & COMPLIANCE
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Antivirus Status Check',
  'Checks Windows Defender and other AV status',
  'powershell',
  $script$
Write-Host "=== Windows Defender Status ===" -ForegroundColor Cyan
Get-MpComputerStatus | Select-Object AntivirusEnabled, AntispywareEnabled,
    RealTimeProtectionEnabled, IoavProtectionEnabled, AntivirusSignatureLastUpdated,
    @{N="LastScan";E={$_.FullScanEndTime}} | Format-List

Write-Host "`n=== Registered AV Products ===" -ForegroundColor Cyan
Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct |
    Select-Object displayName, productState, timestamp | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Windows Firewall Status',
  'Shows firewall status for all profiles',
  'powershell',
  $script$
Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction,
    DefaultOutboundAction, LogFileName | Format-Table -AutoSize

Write-Host "`n=== Recently Added Firewall Rules ===" -ForegroundColor Cyan
Get-NetFirewallRule | Where-Object { $_.Enabled -eq 'True' } |
    Sort-Object -Descending { $_.CreationClassName } | Select-Object -First 20 |
    Select-Object DisplayName, Direction, Action | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Failed Login Attempts',
  'Shows recent failed login attempts from Security log',
  'powershell',
  $script$
Get-WinEvent -FilterHashtable @{LogName='Security';Id=4625} -MaxEvents 50 -ErrorAction SilentlyContinue |
    ForEach-Object {
        $xml = [xml]$_.ToXml()
        [PSCustomObject]@{
            Time = $_.TimeCreated
            Account = $xml.Event.EventData.Data[5].'#text'
            Source = $xml.Event.EventData.Data[19].'#text'
            Reason = $xml.Event.EventData.Data[8].'#text'
        }
    } | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Failed Login Attempts (Linux)',
  'Shows recent failed login attempts',
  'bash',
  $script$
#!/bin/bash
echo "=== Failed SSH Logins ==="
grep "Failed password" /var/log/auth.log 2>/dev/null | tail -50 || \
grep "Failed password" /var/log/secure 2>/dev/null | tail -50 || \
journalctl -u sshd --no-pager | grep "Failed password" | tail -50

echo ""
echo "=== Failed Login Summary by IP ==="
grep "Failed password" /var/log/auth.log 2>/dev/null | \
    grep -oP '\d+\.\d+\.\d+\.\d+' | sort | uniq -c | sort -rn | head -20
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'USB Device History',
  'Shows history of USB devices connected to this machine',
  'powershell',
  $script$
Get-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Enum\USB\*\*" -ErrorAction SilentlyContinue |
    Where-Object { $_.FriendlyName } |
    Select-Object @{N="Device";E={$_.FriendlyName}},
                  @{N="VendorID";E={$_.PSChildName.Split('&')[0]}},
                  @{N="ProductID";E={$_.PSChildName.Split('&')[1]}},
                  @{N="Class";E={$_.Class}} |
    Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Certificate Expiration Check',
  'Checks for expiring SSL certificates in Windows cert store',
  'powershell',
  $script$
$threshold = (Get-Date).AddDays(30)
Get-ChildItem Cert:\LocalMachine\My, Cert:\LocalMachine\WebHosting -ErrorAction SilentlyContinue |
    Where-Object { $_.NotAfter -lt $threshold } |
    Select-Object Subject, Issuer,
        @{N="Expires";E={$_.NotAfter.ToString("yyyy-MM-dd")}},
        @{N="DaysLeft";E={[math]::Round(($_.NotAfter - (Get-Date)).TotalDays)}} |
    Sort-Object DaysLeft | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'BitLocker Status',
  'Shows BitLocker encryption status for all drives',
  'powershell',
  $script$
Get-BitLockerVolume | Select-Object MountPoint, VolumeStatus, EncryptionMethod,
    EncryptionPercentage, ProtectionStatus, KeyProtector | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- HARDWARE & HEALTH
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Information',
  'Comprehensive system hardware information',
  'powershell',
  $script$
$cs = Get-CimInstance Win32_ComputerSystem
$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_Processor
$bios = Get-CimInstance Win32_BIOS

[PSCustomObject]@{
    ComputerName = $cs.Name
    Manufacturer = $cs.Manufacturer
    Model = $cs.Model
    OS = $os.Caption
    OSVersion = $os.Version
    CPU = $cpu.Name
    Cores = $cpu.NumberOfCores
    RAM = "$([math]::Round($cs.TotalPhysicalMemory/1GB))GB"
    BIOSVersion = $bios.SMBIOSBIOSVersion
    SerialNumber = $bios.SerialNumber
    LastBoot = $os.LastBootUpTime
} | Format-List
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Information (Linux)',
  'Comprehensive system hardware information',
  'bash',
  $script$
#!/bin/bash
echo "=== System Information ==="
hostnamectl 2>/dev/null || uname -a

echo ""
echo "=== CPU Information ==="
lscpu | grep -E "Model name|Socket|Core|Thread|MHz"

echo ""
echo "=== Memory Information ==="
free -h

echo ""
echo "=== Disk Information ==="
lsblk -o NAME,SIZE,TYPE,MOUNTPOINT

echo ""
echo "=== Network Interfaces ==="
ip addr show | grep -E "^[0-9]+:|inet "
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Disk Health (SMART)',
  'Checks disk SMART health status',
  'powershell',
  $script$
Get-PhysicalDisk | Select-Object FriendlyName, MediaType, HealthStatus,
    OperationalStatus, @{N="Size";E={[math]::Round($_.Size/1GB,1)}} | Format-Table -AutoSize

Write-Host "`n=== Detailed Disk Reliability ===" -ForegroundColor Cyan
Get-PhysicalDisk | Get-StorageReliabilityCounter |
    Select-Object DeviceId, Temperature, Wear, ReadErrorsTotal, WriteErrorsTotal | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Disk Health (Linux)',
  'Checks disk SMART health status',
  'bash',
  $script$
#!/bin/bash
if command -v smartctl &> /dev/null; then
    for disk in $(lsblk -d -o NAME | tail -n +2); do
        echo "=== /dev/$disk ==="
        sudo smartctl -H /dev/$disk 2>/dev/null || echo "SMART not supported"
    done
else
    echo "smartmontools not installed. Install with: apt install smartmontools"
fi
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Battery Health',
  'Shows battery health and charge information for laptops',
  'powershell',
  $script$
$battery = Get-CimInstance Win32_Battery
if ($battery) {
    [PSCustomObject]@{
        Status = $battery.Status
        EstimatedChargeRemaining = "$($battery.EstimatedChargeRemaining)%"
        EstimatedRunTime = if($battery.EstimatedRunTime -eq 71582788){"Charging"}else{"$($battery.EstimatedRunTime) min"}
        BatteryStatus = switch($battery.BatteryStatus){1{"Discharging"}2{"AC Power"}3{"Fully Charged"}4{"Low"}5{"Critical"}default{$battery.BatteryStatus}}
    } | Format-List

    Write-Host "`n=== Detailed Battery Report ===" -ForegroundColor Cyan
    powercfg /batteryreport /output "$env:TEMP\battery-report.html" | Out-Null
    Write-Host "Full report saved to: $env:TEMP\battery-report.html"
} else {
    Write-Host "No battery detected - this appears to be a desktop"
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'GPU Information',
  'Shows graphics card information and driver version',
  'powershell',
  $script$
Get-CimInstance Win32_VideoController | Select-Object Name, DriverVersion,
    @{N="VRAM";E={[math]::Round($_.AdapterRAM/1GB,1)}},
    VideoModeDescription, CurrentRefreshRate | Format-List
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Memory Details',
  'Shows detailed RAM information by slot',
  'powershell',
  $script$
Get-CimInstance Win32_PhysicalMemory | Select-Object DeviceLocator, Manufacturer,
    @{N="Capacity";E={$_.Capacity/1GB}}, Speed, MemoryType | Format-Table -AutoSize

Write-Host "`nTotal Slots Used: $((Get-CimInstance Win32_PhysicalMemory).Count)"
Write-Host "Total Memory: $([math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB))GB"
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'BIOS Information',
  'Shows BIOS/UEFI version and settings',
  'powershell',
  $script$
$bios = Get-CimInstance Win32_BIOS
$cs = Get-CimInstance Win32_ComputerSystem
[PSCustomObject]@{
    Manufacturer = $bios.Manufacturer
    Version = $bios.SMBIOSBIOSVersion
    ReleaseDate = $bios.ReleaseDate
    SerialNumber = $bios.SerialNumber
    SecureBoot = Confirm-SecureBootUEFI -ErrorAction SilentlyContinue
    BootMode = if($env:firmware_type -eq 'UEFI'){"UEFI"}else{"Legacy"}
} | Format-List
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- SOFTWARE & APPLICATIONS
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Installed Software List',
  'Lists all installed applications with version info',
  'powershell',
  $script$
$apps = @()
$apps += Get-ItemProperty "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*" -ErrorAction SilentlyContinue
$apps += Get-ItemProperty "HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*" -ErrorAction SilentlyContinue
$apps += Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*" -ErrorAction SilentlyContinue

$apps | Where-Object { $_.DisplayName } |
    Select-Object DisplayName, DisplayVersion, Publisher, InstallDate |
    Sort-Object DisplayName | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Installed Packages (Linux)',
  'Lists installed packages with versions',
  'bash',
  $script$
#!/bin/bash
if command -v dpkg &> /dev/null; then
    dpkg -l | grep "^ii" | awk '{print $2, $3}' | head -100
elif command -v rpm &> /dev/null; then
    rpm -qa --qf "%{NAME} %{VERSION}\n" | sort | head -100
elif command -v pacman &> /dev/null; then
    pacman -Q | head -100
fi
echo ""
echo "(Showing first 100 packages)"
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Startup Programs',
  'Lists all programs that run at startup',
  'powershell',
  $script$
Write-Host "=== Registry Run Keys ===" -ForegroundColor Cyan
Get-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run" -ErrorAction SilentlyContinue |
    Select-Object * -ExcludeProperty PS* | Format-List

Write-Host "`n=== Startup Folder ===" -ForegroundColor Cyan
Get-ChildItem "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup" -ErrorAction SilentlyContinue |
    Select-Object Name, LastWriteTime | Format-Table -AutoSize

Write-Host "`n=== Scheduled Tasks (User) ===" -ForegroundColor Cyan
Get-ScheduledTask | Where-Object { $_.State -eq 'Ready' -and $_.Principal.UserId -notmatch 'SYSTEM' } |
    Select-Object TaskName, State -First 20 | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Windows Update Status',
  'Shows pending Windows updates and update history',
  'powershell',
  $script$
Write-Host "=== Pending Updates ===" -ForegroundColor Cyan
$Session = New-Object -ComObject Microsoft.Update.Session
$Searcher = $Session.CreateUpdateSearcher()
$Updates = $Searcher.Search("IsInstalled=0").Updates
if ($Updates.Count -eq 0) {
    Write-Host "No pending updates" -ForegroundColor Green
} else {
    $Updates | ForEach-Object { [PSCustomObject]@{Title=$_.Title; KB=$_.KBArticleIDs} } | Format-Table -AutoSize
}

Write-Host "`n=== Recent Update History ===" -ForegroundColor Cyan
Get-HotFix | Sort-Object InstalledOn -Descending | Select-Object -First 10 |
    Select-Object HotFixID, Description, InstalledOn | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Running Services',
  'Lists all running Windows services',
  'powershell',
  $script$
Get-Service | Where-Object Status -eq 'Running' |
    Select-Object Name, DisplayName, StartType | Sort-Object DisplayName | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Running Services (Linux)',
  'Lists all running systemd services',
  'bash',
  $script$
#!/bin/bash
systemctl list-units --type=service --state=running --no-pager | head -50
$script$,
  '["Linux"]',
  NOW(), NOW()
);

-- ============================================
-- USER & ACCOUNT MANAGEMENT
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Local Users and Groups',
  'Lists all local users and their group memberships',
  'powershell',
  $script$
Write-Host "=== Local Users ===" -ForegroundColor Cyan
Get-LocalUser | Select-Object Name, Enabled, LastLogon, PasswordLastSet | Format-Table -AutoSize

Write-Host "`n=== Administrators Group ===" -ForegroundColor Cyan
Get-LocalGroupMember -Group "Administrators" -ErrorAction SilentlyContinue | Format-Table -AutoSize

Write-Host "`n=== Remote Desktop Users ===" -ForegroundColor Cyan
Get-LocalGroupMember -Group "Remote Desktop Users" -ErrorAction SilentlyContinue | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Local Users (Linux)',
  'Lists local users and their details',
  'bash',
  $script$
#!/bin/bash
echo "=== Regular Users (UID >= 1000) ==="
awk -F: '$3 >= 1000 && $3 < 65534 {print $1, $3, $6, $7}' /etc/passwd

echo ""
echo "=== Users with Sudo Access ==="
grep -E "^sudo:|^wheel:" /etc/group 2>/dev/null

echo ""
echo "=== Recent Logins ==="
last -10
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Currently Logged In Users',
  'Shows all currently logged in users and their sessions',
  'powershell',
  $script$
query user 2>$null
if ($LASTEXITCODE -ne 0) {
    Get-CimInstance Win32_LoggedOnUser |
        Select-Object @{N="User";E={$_.Antecedent.Name}}, @{N="Session";E={$_.Dependent.LogonId}} |
        Format-Table -AutoSize
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Password Policy',
  'Shows local password policy settings',
  'powershell',
  $script$
net accounts
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- STORAGE & BACKUP
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Disk Space Report',
  'Shows disk space usage for all drives',
  'powershell',
  $script$
Get-Volume | Where-Object DriveLetter | Select-Object DriveLetter, FileSystemLabel,
    @{N="Size(GB)";E={[math]::Round($_.Size/1GB,2)}},
    @{N="Free(GB)";E={[math]::Round($_.SizeRemaining/1GB,2)}},
    @{N="Used%";E={[math]::Round((1-$_.SizeRemaining/$_.Size)*100,1)}} |
    Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Disk Space Report (Linux)',
  'Shows disk space usage for all mounted filesystems',
  'bash',
  $script$
#!/bin/bash
echo "=== Disk Usage ==="
df -h | grep -v tmpfs | grep -v loop

echo ""
echo "=== Largest Directories (top 10) ==="
du -h --max-depth=1 / 2>/dev/null | sort -hr | head -10
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Large Files Finder',
  'Finds the largest files on the system',
  'powershell',
  $script$
param([int]$TopCount = 20, [string]$Path = "C:\")

Get-ChildItem -Path $Path -Recurse -File -ErrorAction SilentlyContinue |
    Sort-Object Length -Descending | Select-Object -First $TopCount |
    Select-Object FullName, @{N="Size(MB)";E={[math]::Round($_.Length/1MB,2)}}, LastWriteTime |
    Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Shadow Copy Status',
  'Shows Volume Shadow Copy (VSS) status and available snapshots',
  'powershell',
  $script$
Write-Host "=== Shadow Copy Storage ===" -ForegroundColor Cyan
vssadmin list shadowstorage

Write-Host "`n=== Available Shadow Copies ===" -ForegroundColor Cyan
vssadmin list shadows
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Windows Backup Status',
  'Checks Windows Backup configuration and history',
  'powershell',
  $script$
$backupStatus = Get-WBSummary -ErrorAction SilentlyContinue
if ($backupStatus) {
    $backupStatus | Format-List
} else {
    Write-Host "Windows Server Backup not configured or not available"
    Write-Host "`nChecking File History..."
    Get-ItemProperty "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\FileHistory" -ErrorAction SilentlyContinue
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- PERFORMANCE & OPTIMIZATION
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Performance Snapshot',
  'Shows current CPU, memory, and disk performance',
  'powershell',
  $script$
Write-Host "=== CPU Usage ===" -ForegroundColor Cyan
$cpu = Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples 3
Write-Host "CPU: $([math]::Round(($cpu.CounterSamples.CookedValue | Measure-Object -Average).Average, 1))%"

Write-Host "`n=== Memory Usage ===" -ForegroundColor Cyan
$os = Get-CimInstance Win32_OperatingSystem
$usedMem = [math]::Round(($os.TotalVisibleMemorySize - $os.FreePhysicalMemory)/1MB, 1)
$totalMem = [math]::Round($os.TotalVisibleMemorySize/1MB, 1)
Write-Host "Memory: $usedMem GB / $totalMem GB ($([math]::Round($usedMem/$totalMem*100,1))%)"

Write-Host "`n=== Top Processes by Memory ===" -ForegroundColor Cyan
Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 10 |
    Select-Object Name, @{N="Memory(MB)";E={[math]::Round($_.WorkingSet64/1MB)}}, CPU | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Performance (Linux)',
  'Shows current system resource usage',
  'bash',
  $script$
#!/bin/bash
echo "=== CPU & Memory ==="
top -bn1 | head -5

echo ""
echo "=== Top Processes by Memory ==="
ps aux --sort=-%mem | head -11

echo ""
echo "=== Top Processes by CPU ==="
ps aux --sort=-%cpu | head -11

echo ""
echo "=== Disk I/O ==="
iostat -x 1 1 2>/dev/null || echo "iostat not available (install sysstat)"
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Temp Files Cleanup Analysis',
  'Analyzes temporary files that can be safely cleaned',
  'powershell',
  $script$
$locations = @(
    @{Path="$env:TEMP"; Name="User Temp"},
    @{Path="C:\Windows\Temp"; Name="System Temp"},
    @{Path="C:\Windows\SoftwareDistribution\Download"; Name="Windows Update Cache"}
)

foreach ($loc in $locations) {
    if (Test-Path $loc.Path) {
        $size = (Get-ChildItem $loc.Path -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum
        Write-Host "$($loc.Name): $([math]::Round($size/1MB, 2)) MB" -ForegroundColor Cyan
    }
}

Write-Host "`nRun Disk Cleanup for safe removal: cleanmgr /d C"
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Uptime',
  'Shows system uptime and last boot time',
  'powershell',
  $script$
$os = Get-CimInstance Win32_OperatingSystem
$uptime = (Get-Date) - $os.LastBootUpTime
[PSCustomObject]@{
    LastBoot = $os.LastBootUpTime
    Uptime = "$($uptime.Days) days, $($uptime.Hours) hours, $($uptime.Minutes) minutes"
} | Format-List
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Uptime (Linux)',
  'Shows system uptime and load average',
  'bash',
  $script$
#!/bin/bash
echo "=== Uptime ==="
uptime

echo ""
echo "=== Last Boot ==="
who -b

echo ""
echo "=== Load Average History ==="
cat /proc/loadavg
$script$,
  '["Linux"]',
  NOW(), NOW()
);

-- ============================================
-- TROUBLESHOOTING & DIAGNOSTICS
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Event Log Errors (Last 24h)',
  'Shows recent error events from Windows Event Log',
  'powershell',
  $script$
$yesterday = (Get-Date).AddDays(-1)
$logs = @('System', 'Application')

foreach ($log in $logs) {
    Write-Host "=== $log Log Errors ===" -ForegroundColor Cyan
    Get-WinEvent -FilterHashtable @{LogName=$log; Level=2; StartTime=$yesterday} -MaxEvents 20 -ErrorAction SilentlyContinue |
        Select-Object TimeCreated, Id, Message | Format-Table -Wrap
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Logs (Linux)',
  'Shows recent system errors and warnings',
  'bash',
  $script$
#!/bin/bash
echo "=== Recent Errors (journalctl) ==="
journalctl -p err --since "24 hours ago" --no-pager | tail -50

echo ""
echo "=== Kernel Messages (dmesg) ==="
dmesg --level=err,warn | tail -30
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Connectivity Test',
  'Tests network connectivity to common endpoints',
  'powershell',
  $script$
$targets = @(
    @{Name="Google DNS"; Target="8.8.8.8"},
    @{Name="Cloudflare DNS"; Target="1.1.1.1"},
    @{Name="Google.com"; Target="google.com"},
    @{Name="Microsoft.com"; Target="microsoft.com"}
)

foreach ($t in $targets) {
    $result = Test-Connection -ComputerName $t.Target -Count 2 -Quiet
    $status = if($result){"OK"}else{"FAILED"}
    $color = if($result){"Green"}else{"Red"}
    Write-Host "$($t.Name) ($($t.Target)): $status" -ForegroundColor $color
}

Write-Host "`n=== DNS Resolution Test ===" -ForegroundColor Cyan
Resolve-DnsName google.com -Type A | Select-Object Name, IPAddress
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Connectivity Test (Linux)',
  'Tests network connectivity to common endpoints',
  'bash',
  $script$
#!/bin/bash
targets=("8.8.8.8:Google DNS" "1.1.1.1:Cloudflare DNS" "google.com:Google" "microsoft.com:Microsoft")

for target in "${targets[@]}"; do
    ip="${target%%:*}"
    name="${target##*:}"
    if ping -c 2 -W 2 "$ip" > /dev/null 2>&1; then
        echo "$name ($ip): OK"
    else
        echo "$name ($ip): FAILED"
    fi
done

echo ""
echo "=== DNS Resolution ==="
dig google.com +short
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Driver Issues Check',
  'Identifies devices with driver problems',
  'powershell',
  $script$
Write-Host "=== Devices with Problems ===" -ForegroundColor Cyan
Get-PnpDevice | Where-Object { $_.Status -ne 'OK' } |
    Select-Object Class, FriendlyName, Status, Problem | Format-Table -AutoSize

Write-Host "`n=== Recently Installed Drivers ===" -ForegroundColor Cyan
Get-WmiObject Win32_PnPSignedDriver | Sort-Object DriverDate -Descending |
    Select-Object -First 10 DeviceName, DriverVersion, DriverDate | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Blue Screen (BSOD) History',
  'Shows recent blue screen crash information',
  'powershell',
  $script$
Get-WinEvent -FilterHashtable @{LogName='System'; Id=1001; ProviderName='Microsoft-Windows-WER-SystemErrorReporting'} -MaxEvents 10 -ErrorAction SilentlyContinue |
    ForEach-Object {
        $xml = [xml]$_.ToXml()
        [PSCustomObject]@{
            Date = $_.TimeCreated
            BugCheck = $xml.Event.EventData.Data[0].'#text'
            Parameter1 = $xml.Event.EventData.Data[1].'#text'
        }
    } | Format-Table -AutoSize

Write-Host "`nCheck C:\Windows\Minidump for detailed crash dumps"
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- REMOTE ACCESS & MANAGEMENT
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'RDP Status Check',
  'Checks Remote Desktop configuration and status',
  'powershell',
  $script$
$rdp = Get-ItemProperty "HKLM:\System\CurrentControlSet\Control\Terminal Server"
$nla = Get-ItemProperty "HKLM:\System\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp"

[PSCustomObject]@{
    RDPEnabled = $rdp.fDenyTSConnections -eq 0
    NLARequired = $nla.UserAuthentication -eq 1
    Port = (Get-ItemProperty "HKLM:\System\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp").PortNumber
    FirewallRule = (Get-NetFirewallRule -DisplayName "*Remote Desktop*" -ErrorAction SilentlyContinue | Where-Object Enabled -eq 'True').Count -gt 0
} | Format-List

Write-Host "`n=== Active RDP Sessions ===" -ForegroundColor Cyan
query session 2>$null
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'SSH Status Check (Linux)',
  'Checks SSH server configuration and status',
  'bash',
  $script$
#!/bin/bash
echo "=== SSH Service Status ==="
systemctl status sshd --no-pager 2>/dev/null || systemctl status ssh --no-pager

echo ""
echo "=== SSH Configuration ==="
grep -E "^(Port|PermitRootLogin|PasswordAuthentication|PubkeyAuthentication)" /etc/ssh/sshd_config 2>/dev/null

echo ""
echo "=== Active SSH Sessions ==="
who | grep -E "pts/"

echo ""
echo "=== SSH Login History (last 10) ==="
last -10 | grep -E "pts/"
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'WinRM Status',
  'Checks Windows Remote Management configuration',
  'powershell',
  $script$
Write-Host "=== WinRM Service ===" -ForegroundColor Cyan
Get-Service WinRM | Select-Object Name, Status, StartType

Write-Host "`n=== WinRM Configuration ===" -ForegroundColor Cyan
winrm get winrm/config/service 2>$null

Write-Host "`n=== Trusted Hosts ===" -ForegroundColor Cyan
Get-Item WSMan:\localhost\Client\TrustedHosts | Select-Object Value
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- ACTIVE DIRECTORY & DOMAIN
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Domain Join Status',
  'Checks if machine is domain-joined and shows domain info',
  'powershell',
  $script$
$cs = Get-CimInstance Win32_ComputerSystem
[PSCustomObject]@{
    ComputerName = $cs.Name
    Domain = $cs.Domain
    DomainJoined = $cs.PartOfDomain
    DomainRole = switch($cs.DomainRole){
        0{"Standalone Workstation"}
        1{"Member Workstation"}
        2{"Standalone Server"}
        3{"Member Server"}
        4{"Backup Domain Controller"}
        5{"Primary Domain Controller"}
    }
} | Format-List

if ($cs.PartOfDomain) {
    Write-Host "=== Domain Controllers ===" -ForegroundColor Cyan
    nltest /dclist:$($cs.Domain) 2>$null
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Group Policy Status',
  'Shows applied Group Policies and last refresh time',
  'powershell',
  $script$
Write-Host "=== Last GP Refresh ===" -ForegroundColor Cyan
gpresult /r /scope:computer 2>$null | Select-String "Last time Group Policy"

Write-Host "`n=== Applied GPOs ===" -ForegroundColor Cyan
gpresult /r /scope:computer 2>$null | Select-String -Pattern "^\s+\S" -Context 0,0 |
    Where-Object { $_.Line -notmatch "N/A|The following" }

Write-Host "`nRun 'gpupdate /force' to refresh Group Policy"
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- COMPLIANCE & REPORTING
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Security Compliance Check',
  'Basic security compliance audit',
  'powershell',
  $script$
$results = @()

# Check Windows Defender
$defender = Get-MpComputerStatus -ErrorAction SilentlyContinue
$results += [PSCustomObject]@{
    Check = "Windows Defender Enabled"
    Status = if($defender.AntivirusEnabled){"PASS"}else{"FAIL"}
}
$results += [PSCustomObject]@{
    Check = "Real-time Protection"
    Status = if($defender.RealTimeProtectionEnabled){"PASS"}else{"FAIL"}
}

# Check Firewall
$fw = Get-NetFirewallProfile | Where-Object Enabled -eq $true
$results += [PSCustomObject]@{
    Check = "Firewall Enabled"
    Status = if($fw){"PASS"}else{"FAIL"}
}

# Check BitLocker
$bl = Get-BitLockerVolume -MountPoint C: -ErrorAction SilentlyContinue
$results += [PSCustomObject]@{
    Check = "BitLocker on C:"
    Status = if($bl.ProtectionStatus -eq 'On'){"PASS"}else{"WARN"}
}

# Check Password Policy
$results += [PSCustomObject]@{
    Check = "Password Never Expires (Bad)"
    Status = if((Get-LocalUser | Where-Object PasswordNeverExpires).Count -eq 0){"PASS"}else{"WARN"}
}

$results | Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Security Compliance Check (Linux)',
  'Basic security compliance audit for Linux',
  'bash',
  $script$
#!/bin/bash
echo "=== Security Compliance Check ==="
echo ""

# SSH Root Login
echo -n "SSH Root Login Disabled: "
if grep -qE "^PermitRootLogin no" /etc/ssh/sshd_config 2>/dev/null; then
    echo "PASS"
else
    echo "WARN - Root login may be permitted"
fi

# Firewall
echo -n "Firewall Active: "
if systemctl is-active --quiet ufw 2>/dev/null || systemctl is-active --quiet firewalld 2>/dev/null; then
    echo "PASS"
else
    echo "WARN - No active firewall detected"
fi

# Updates
echo -n "Security Updates: "
if command -v apt &> /dev/null; then
    updates=$(apt list --upgradable 2>/dev/null | grep -i security | wc -l)
    if [ "$updates" -eq 0 ]; then echo "PASS"; else echo "WARN - $updates security updates pending"; fi
else
    echo "SKIP - apt not available"
fi

# Password aging
echo -n "Password Aging Configured: "
if grep -qE "^PASS_MAX_DAYS\s+[0-9]+" /etc/login.defs 2>/dev/null; then
    echo "PASS"
else
    echo "WARN"
fi
$script$,
  '["Linux"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Inventory Export',
  'Exports comprehensive system inventory to JSON',
  'powershell',
  $script$
$inventory = [PSCustomObject]@{
    GeneratedAt = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    ComputerName = $env:COMPUTERNAME
    OS = (Get-CimInstance Win32_OperatingSystem).Caption
    OSVersion = (Get-CimInstance Win32_OperatingSystem).Version
    CPU = (Get-CimInstance Win32_Processor).Name
    RAM = "$([math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB))GB"
    Disks = Get-Volume | Where-Object DriveLetter | Select-Object DriveLetter,
        @{N="SizeGB";E={[math]::Round($_.Size/1GB)}},
        @{N="FreeGB";E={[math]::Round($_.SizeRemaining/1GB)}}
    IPAddresses = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object IPAddress -notlike "127.*").IPAddress
    InstalledSoftwareCount = (Get-ItemProperty "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*" | Where-Object DisplayName).Count
    LastBoot = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime
}

$inventory | ConvertTo-Json -Depth 3
$script$,
  '["Windows"]',
  NOW(), NOW()
);

-- ============================================
-- macOS SCRIPTS
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'System Information (macOS)',
  'Comprehensive macOS system information',
  'bash',
  $script$
#!/bin/bash
echo "=== System Information ==="
system_profiler SPHardwareDataType | grep -E "Model|Processor|Memory|Serial"

echo ""
echo "=== macOS Version ==="
sw_vers

echo ""
echo "=== Disk Usage ==="
df -h | grep -E "^/dev/"

echo ""
echo "=== Network ==="
ifconfig | grep -E "^en|inet " | grep -v inet6
$script$,
  '["macOS"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Installed Applications (macOS)',
  'Lists installed applications',
  'bash',
  $script$
#!/bin/bash
echo "=== Applications in /Applications ==="
ls -1 /Applications | grep "\.app$" | sed 's/\.app$//'

echo ""
echo "=== Homebrew Packages ==="
if command -v brew &> /dev/null; then
    brew list
else
    echo "Homebrew not installed"
fi
$script$,
  '["macOS"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Security Status (macOS)',
  'Checks macOS security settings',
  'bash',
  $script$
#!/bin/bash
echo "=== FileVault Status ==="
fdesetup status

echo ""
echo "=== Firewall Status ==="
/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate

echo ""
echo "=== Gatekeeper Status ==="
spctl --status

echo ""
echo "=== SIP Status ==="
csrutil status
$script$,
  '["macOS"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Network Printers (macOS)',
  'Lists configured printers',
  'bash',
  $script$
#!/bin/bash
echo "=== Configured Printers ==="
lpstat -p

echo ""
echo "=== Default Printer ==="
lpstat -d

echo ""
echo "=== Printer Queue ==="
lpq -a
$script$,
  '["macOS"]',
  NOW(), NOW()
);

-- ============================================
-- CROSS-PLATFORM UTILITIES
-- ============================================

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Process List by Memory',
  'Lists top processes by memory usage',
  'powershell',
  $script$
Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 20 |
    Select-Object Name, Id,
        @{N="Memory(MB)";E={[math]::Round($_.WorkingSet64/1MB,1)}},
        @{N="CPU(s)";E={[math]::Round($_.CPU,1)}} |
    Format-Table -AutoSize
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Process List by Memory (Linux/macOS)',
  'Lists top processes by memory usage',
  'bash',
  $script$
#!/bin/bash
echo "=== Top 20 Processes by Memory ==="
ps aux --sort=-%mem | head -21
$script$,
  '["Linux", "macOS"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Kill Process by Name',
  'Terminates processes matching a name pattern',
  'powershell',
  $script$
param([string]$ProcessName)

if (-not $ProcessName) {
    Write-Host "Usage: Provide process name as parameter" -ForegroundColor Yellow
    Write-Host "Current running processes:"
    Get-Process | Select-Object Name -Unique | Sort-Object Name
    return
}

$procs = Get-Process -Name "*$ProcessName*" -ErrorAction SilentlyContinue
if ($procs) {
    Write-Host "Found processes:"
    $procs | Select-Object Name, Id, CPU | Format-Table -AutoSize
    $procs | Stop-Process -Force
    Write-Host "Processes terminated" -ForegroundColor Green
} else {
    Write-Host "No processes found matching '$ProcessName'" -ForegroundColor Yellow
}
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Environment Variables',
  'Lists all environment variables',
  'powershell',
  $script$
Get-ChildItem Env: | Sort-Object Name | Format-Table -AutoSize -Wrap
$script$,
  '["Windows"]',
  NOW(), NOW()
);

INSERT INTO scripts (id, name, description, language, content, os_types, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Environment Variables (Linux/macOS)',
  'Lists all environment variables',
  'bash',
  $script$
#!/bin/bash
env | sort
$script$,
  '["Linux", "macOS"]',
  NOW(), NOW()
);

-- Done! Summary of scripts added
SELECT
    language,
    COUNT(*) as count,
    array_agg(DISTINCT unnest(os_types::text[])) as os_types
FROM scripts
GROUP BY language
ORDER BY count DESC;
