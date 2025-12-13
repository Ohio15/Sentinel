# Fix SentinelAgent registry key permissions
$keyPath = 'HKLM:\SYSTEM\CurrentControlSet\Services\SentinelAgent'

# Take ownership first
$key = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey(
    'SYSTEM\CurrentControlSet\Services\SentinelAgent',
    [Microsoft.Win32.RegistryKeyPermissionCheck]::ReadWriteSubTree,
    [System.Security.AccessControl.RegistryRights]::TakeOwnership
)

if ($key -eq $null) {
    Write-Host "Failed to open key for ownership"
    exit 1
}

# Get current ACL
$acl = $key.GetAccessControl()
$currentOwner = $acl.GetOwner([System.Security.Principal.NTAccount])
Write-Host "Current owner: $currentOwner"

# Set SYSTEM as owner
$system = [System.Security.Principal.NTAccount]"NT AUTHORITY\SYSTEM"
$acl.SetOwner($system)
$key.SetAccessControl($acl)
$key.Close()
Write-Host "Ownership set to SYSTEM"

# Now open with full control to modify DACL
$key = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey(
    'SYSTEM\CurrentControlSet\Services\SentinelAgent',
    [Microsoft.Win32.RegistryKeyPermissionCheck]::ReadWriteSubTree,
    [System.Security.AccessControl.RegistryRights]::ChangePermissions
)

if ($key -eq $null) {
    Write-Host "Failed to open key for permission change"
    exit 1
}

$acl = $key.GetAccessControl()

# Disable protected DACL
$acl.SetAccessRuleProtection($false, $false)

# Remove all deny rules
$denyRules = @($acl.Access | Where-Object { $_.AccessControlType -eq 'Deny' })
Write-Host "Found $($denyRules.Count) deny rules"
foreach ($rule in $denyRules) {
    $result = $acl.RemoveAccessRule($rule)
    Write-Host "Removed: $($rule.IdentityReference) - $result"
}

# Add proper allow rules
$systemFull = New-Object System.Security.AccessControl.RegistryAccessRule(
    "NT AUTHORITY\SYSTEM",
    "FullControl",
    "ContainerInherit,ObjectInherit",
    "None",
    "Allow"
)
$adminFull = New-Object System.Security.AccessControl.RegistryAccessRule(
    "BUILTIN\Administrators",
    "FullControl",
    "ContainerInherit,ObjectInherit",
    "None",
    "Allow"
)
$acl.AddAccessRule($systemFull)
$acl.AddAccessRule($adminFull)

$key.SetAccessControl($acl)
$key.Close()
Write-Host "Permissions updated"

# Verify
$newAcl = Get-Acl $keyPath
$newAcl.Access | Format-Table IdentityReference, AccessControlType, RegistryRights
