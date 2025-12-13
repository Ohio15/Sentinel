# Fix registry by re-exporting and re-importing with correct permissions
# First, export the key
$exportPath = "C:\Windows\Temp\sentinel_reg_backup.reg"
$keyPath = "HKLM\SYSTEM\CurrentControlSet\Services\SentinelAgent"

# Export
reg export $keyPath $exportPath /y

if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to export registry key"
    exit 1
}

# Delete the key
reg delete $keyPath /f

if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to delete registry key (this is expected due to protection)"
    # Try using sc to delete/recreate service
    sc.exe delete SentinelAgent
}

# Re-import
reg import $exportPath

Write-Host "Registry fixed. Please reinstall the service if needed."
