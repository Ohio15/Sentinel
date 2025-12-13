# Fix SentinelAgent service by resetting registry ACL from SYSTEM context
# This script should be run via scheduled task as SYSTEM

$keyPath = "HKLM:\SYSTEM\CurrentControlSet\Services\SentinelAgent"

# Check current ACL
Write-Host "Current ACL:"
$acl = Get-Acl $keyPath
$acl.Access | Format-Table IdentityReference, AccessControlType, RegistryRights

# Try to modify
Write-Host "`nAttempting to modify registry..."
try {
    # Get ACL
    $acl = Get-Acl $keyPath

    # Disable protection (allow inherited permissions)
    $acl.SetAccessRuleProtection($false, $false)

    # Remove deny rules
    $denyRules = @($acl.Access | Where-Object { $_.AccessControlType -eq 'Deny' })
    Write-Host "Found $($denyRules.Count) deny rules"
    foreach ($rule in $denyRules) {
        $result = $acl.RemoveAccessRule($rule)
        Write-Host "Removed: $($rule.IdentityReference) - Result: $result"
    }

    # Apply changes
    Set-Acl $keyPath $acl
    Write-Host "ACL modified successfully"

    # Now enable the service
    sc.exe config SentinelAgent start=auto
    Write-Host "Service enabled"

    # Start it
    sc.exe start SentinelAgent
    Write-Host "Service started"

} catch {
    Write-Host "Error: $_"
}

# Show final state
Write-Host "`nFinal ACL:"
$finalAcl = Get-Acl $keyPath
$finalAcl.Access | Format-Table IdentityReference, AccessControlType, RegistryRights

sc.exe qc SentinelAgent
sc.exe query SentinelAgent
