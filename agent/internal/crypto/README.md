# Sentinel Agent Config Encryption

This package provides AES-256-GCM encryption for the Sentinel agent configuration file, using machine-specific key derivation to protect sensitive data at rest.

## Features

- **AES-256-GCM Encryption**: Industry-standard authenticated encryption
- **Machine-Specific Keys**: Derived from hardware identifiers unique to each machine
- **Automatic Migration**: Transparently upgrades unencrypted configs to encrypted format
- **Cross-Platform**: Works on Windows, Linux, and macOS
- **Version Support**: Includes version byte for future algorithm changes

## Architecture

### Encryption Format

Encrypted config files use the following binary format:

```
[4 bytes magic "SNTL"][1 byte version][12 bytes nonce][encrypted data + 16 byte auth tag]
```

- **Magic Bytes**: "SNTL" - identifies encrypted Sentinel config
- **Version**: Currently 1 - allows future algorithm changes
- **Nonce**: 12-byte random nonce for GCM (generated per encryption)
- **Ciphertext**: AES-256-GCM encrypted JSON config data
- **Auth Tag**: 16-byte authentication tag (appended by GCM)

### Key Derivation

The encryption key is derived using SHA-256 from machine-specific data:

**Windows:**
- Machine GUID from `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`
- Computer hostname

**Linux:**
- Machine ID from `/etc/machine-id` or `/var/lib/dbus/machine-id`
- Computer hostname

**macOS:**
- IOPlatformUUID from `ioreg` command
- Computer hostname

The combined string is hashed with SHA-256 to produce a 32-byte key for AES-256.

## Usage

The encryption is transparent to the rest of the application. The config module automatically:

1. Encrypts data when saving configs
2. Decrypts data when loading configs
3. Migrates unencrypted configs on first load

### Example

```go
import "github.com/sentinel/agent/internal/config"

// Load config (automatically decrypts if encrypted)
cfg, err := config.Load()
if err != nil {
    log.Fatal(err)
}

// Modify config
cfg.HeartbeatInterval = 30

// Save config (automatically encrypts)
err = cfg.Save()
if err != nil {
    log.Fatal(err)
}
```

## Migration Path

When an existing unencrypted config is detected:

1. Config is loaded as plain JSON
2. Migration event is logged
3. Config is immediately re-saved in encrypted format
4. Future loads will use encrypted format

No user intervention is required.

## Security Considerations

### Strengths

- **Authenticated Encryption**: GCM mode provides both confidentiality and integrity
- **Machine-Bound**: Config can only be decrypted on the original machine
- **No Key Storage**: Key is derived on-demand, never stored on disk
- **Forward Compatibility**: Version byte allows algorithm upgrades

### Limitations

- **Local Access**: Does not protect against attackers with local admin/root access
- **Machine Transfer**: Config cannot be moved to different hardware
- **VM Cloning**: Cloned VMs may share the same encryption key
- **Key Derivation**: Based on system identifiers that may change during hardware changes

### File Permissions

Config files are stored with 0600 permissions (owner read/write only) for additional protection.

## Testing

Run the test suite:

```bash
cd agent
go test ./internal/crypto/... -v
```

Tests cover:
- Encryption/decryption round-trip
- Magic byte detection
- Key derivation consistency
- Invalid data handling
- Machine ID retrieval

## API Reference

### `EncryptConfig(data []byte) ([]byte, error)`

Encrypts configuration data using AES-256-GCM.

- **Input**: Plain JSON config data
- **Output**: Encrypted binary data with magic bytes, version, and nonce
- **Returns**: Error if key derivation or encryption fails

### `DecryptConfig(data []byte) ([]byte, error)`

Decrypts configuration data encrypted with `EncryptConfig`.

- **Input**: Encrypted binary data
- **Output**: Plain JSON config data
- **Returns**: Error if data is invalid, corrupted, or wrong version

### `IsEncrypted(data []byte) bool`

Checks if data appears to be encrypted config by checking magic bytes.

- **Input**: Raw config file data
- **Output**: true if encrypted, false otherwise

### `GetMachineID() (string, error)`

Retrieves the machine-specific identifier for the current platform.

- **Output**: Machine GUID/UUID/ID as a string
- **Returns**: Error if unable to read system identifier

### `DeriveKey() ([]byte, error)`

Derives a 32-byte AES-256 key from machine ID and hostname.

- **Output**: 32-byte encryption key
- **Returns**: Error if machine ID retrieval or hashing fails

## Troubleshooting

### "Failed to decrypt config"

**Cause**: Config file corrupted or machine hardware changed

**Solution**:
1. Backup the config file
2. Delete the config file to create a fresh one
3. Re-enroll the agent

### "Failed to read MachineGuid" (Windows)

**Cause**: Registry permissions or corrupted registry

**Solution**: Run agent with administrator privileges

### "Failed to read machine-id" (Linux)

**Cause**: Missing system files or permissions

**Solution**:
1. Ensure `/etc/machine-id` exists
2. Run agent with appropriate permissions
3. On systemd systems, run `systemd-machine-id-setup`

### Config doesn't migrate

**Cause**: File permissions prevent re-writing config

**Solution**: Ensure agent has write permissions to config directory
