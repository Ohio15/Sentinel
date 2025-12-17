# Encrypted Config Storage - Quick Reference

## Overview
AES-256-GCM encryption for Sentinel agent config files with machine-specific key derivation.

## Key Features
- **Encryption:** AES-256-GCM authenticated encryption
- **Key Derivation:** SHA-256(MachineID + Hostname)
- **Auto-Migration:** Unencrypted configs automatically upgraded
- **Cross-Platform:** Windows, Linux, macOS support

## File Format
```
[SNTL][v1][nonce:12 bytes][ciphertext + auth tag]
```

## API Functions

### Core Functions
```go
// Encrypt config data
encrypted, err := crypto.EncryptConfig(jsonData)

// Decrypt config data
decrypted, err := crypto.DecryptConfig(encrypted)

// Check if data is encrypted
isEnc := crypto.IsEncrypted(data)

// Derive encryption key (for internal use)
key, err := crypto.DeriveKey()

// Get machine-specific ID
machineID, err := crypto.GetMachineID()
```

### Migration Utilities
```go
// Manually migrate config file
err := crypto.MigrateConfigFile("/path/to/config.json")

// Decrypt config for debugging
err := crypto.DecryptConfigFile("/path/to/config.json", "/path/to/output.json")

// Verify config can be decrypted
err := crypto.VerifyConfigFile("/path/to/config.json")
```

## Integration in config.go

### Load Function
```go
// Automatically detects and decrypts encrypted configs
// Migrates unencrypted configs on first load
cfg, err := config.Load()
```

### Save Function
```go
// Automatically encrypts config before writing
err := cfg.Save()
```

## Machine ID Sources

| Platform | Source |
|----------|--------|
| Windows | `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid` |
| Linux | `/etc/machine-id` or `/var/lib/dbus/machine-id` |
| macOS | IOPlatformUUID from `ioreg` command |

## File Permissions
All config files are written with `0600` (owner read/write only).

## Error Messages

| Error | Meaning | Solution |
|-------|---------|----------|
| "invalid magic bytes" | File not encrypted or corrupted | Delete and recreate config |
| "unsupported version: X" | Future version not supported | Upgrade agent |
| "failed to decrypt" | Wrong machine or corruption | Restore from backup |
| "failed to get machine ID" | System ID unavailable | Check platform-specific source |

## Testing

### Run Tests
```bash
cd agent
go test ./internal/crypto/... -v
```

### Build Verification
```bash
cd agent
go build ./cmd/sentinel-agent/
```

## Migration Process

1. **Automatic (Recommended)**
   - Start agent with updated code
   - Existing config detected as unencrypted
   - Automatically re-saved in encrypted format
   - Logged: "[CONFIG] Successfully migrated config to encrypted format"

2. **Manual**
   ```go
   import "github.com/sentinel/agent/internal/crypto"

   err := crypto.MigrateConfigFile("/path/to/config.json")
   // Creates backup at config.json.backup
   ```

## Security Properties

✅ **Provides:**
- Confidentiality (AES-256 encryption)
- Integrity (GCM authentication tag)
- Machine binding (cannot decrypt on different hardware)
- File access control (0600 permissions)

❌ **Does Not Protect Against:**
- Local admin/root access
- Memory dumps
- Hardware cloning
- Machine ID changes

## Performance
- Key derivation: ~10-20ms
- Encryption: ~1-2ms per KB
- Decryption: ~1-2ms per KB

## Dependencies
- Go 1.21+
- `golang.org/x/sys/windows` (Windows only)
- Standard library `crypto/*` packages

## Files Structure
```
agent/internal/crypto/
├── config_crypto.go          # Core encryption logic
├── config_crypto_windows.go  # Windows machine ID
├── config_crypto_unix.go     # Linux/macOS machine ID
├── config_crypto_test.go     # Test suite
├── example_test.go           # Usage examples
├── migrate.go                # Migration utilities
├── README.md                 # Full documentation
├── IMPLEMENTATION.md         # Technical details
└── QUICK_REFERENCE.md        # This file
```

## Common Operations

### Create Encrypted Config
```go
cfg := config.DefaultConfig()
cfg.ServerURL = "https://example.com"
err := cfg.Save() // Automatically encrypted
```

### Load Encrypted Config
```go
cfg, err := config.Load() // Automatically decrypted
```

### Check Config Status
```go
data, _ := os.ReadFile("/path/to/config.json")
if crypto.IsEncrypted(data) {
    fmt.Println("Config is encrypted")
}
```

### Decrypt for Inspection (Debug Only)
```go
err := crypto.DecryptConfigFile(
    "/etc/sentinel/config.json",
    "/tmp/config-plain.json",
)
// View /tmp/config-plain.json
```

## Troubleshooting

### Config Won't Decrypt After Hardware Change
**Cause:** Machine ID changed (new hardware, virtualization)
**Solution:** Delete config file and re-enroll agent

### Permission Denied
**Cause:** Insufficient permissions to read system ID
**Solution:**
- Windows: Run as Administrator
- Linux: Run with appropriate privileges
- macOS: Check file permissions on IOPlatformUUID

### Migration Not Happening
**Cause:** Write permissions or file locked
**Solution:**
- Check config directory permissions
- Ensure no other processes have file open
- Check disk space

## Version History

| Version | Changes |
|---------|---------|
| 1 | Initial AES-256-GCM implementation |

## Future Considerations

Potential enhancements (not currently implemented):
- TPM/TEE integration for hardware security
- Key rotation based on time
- Remote key management (Vault, AWS Secrets Manager)
- Per-field encryption for selective protection
- Compression before encryption

## Support

- Documentation: See README.md and IMPLEMENTATION.md
- Tests: See config_crypto_test.go
- Examples: See example_test.go
