# Remove deny rules from SentinelAgent registry key
$keyPath = 'HKLM:\SYSTEM\CurrentControlSet\Services\SentinelAgent'
$acl = Get-Acl $keyPath

# Remove all deny rules
$denyRules = $acl.Access | Where-Object { $_.AccessControlType -eq 'Deny' }
foreach ($rule in $denyRules) {
    $result = $acl.RemoveAccessRule($rule)
    Write-Host "Removed deny rule: $($rule.IdentityReference) - Result: $result"
}

# Disable protection
$acl.SetAccessRuleProtection($false, $true)

# Apply the modified ACL
Set-Acl $keyPath $acl
Write-Host "Registry permissions updated"

# Verify
$newAcl = Get-Acl $keyPath
$newAcl.Access | Format-Table IdentityReference, AccessControlType, RegistryRights
