# Sentinel Installer Error Codes

This document provides a comprehensive reference for all error codes that may be encountered during Sentinel Agent installation, upgrade, or uninstallation.

## Error Code Format

All error codes follow the format: `EXYY` where:
- `X` = Error category (1-6)
- `YY` = Specific error number (00-99)

Each error generates a **Reference ID** in the format: `INS-XXXXXX-YYYYMMDD`
- `INS` = Installer prefix
- `XXXXXX` = Unique 6-character identifier
- `YYYYMMDD` = Date stamp

---

## E1xx: Installation Errors

General errors during the installation process.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E100 | `INSTALL_GENERAL_FAILURE` | General installation failure | Check logs for specific error details |
| E101 | `EXTRACT_FAILED` | Failed to extract installation files | Ensure temp directory is writable and has sufficient space |
| E102 | `DISK_SPACE_INSUFFICIENT` | Not enough disk space | Free at least 100MB on the installation drive |
| E103 | `PERMISSION_DENIED` | Insufficient permissions for installation | Run installer as Administrator (Windows) or root (Linux/macOS) |
| E104 | `PATH_NOT_FOUND` | Installation path does not exist | Verify the installation directory path is valid |
| E105 | `PATH_NOT_WRITABLE` | Installation path is not writable | Check directory permissions and ownership |
| E106 | `FILE_IN_USE` | Installation files are in use | Close applications using Sentinel files, or restart the computer |
| E107 | `BINARY_CORRUPT` | Downloaded binary is corrupted | Re-download the installer; verify network connection |
| E108 | `CHECKSUM_MISMATCH` | File checksum verification failed | Re-download the installer; possible network corruption |
| E109 | `PLATFORM_MISMATCH` | Installer architecture mismatch | Download the correct installer for your platform (x64, ARM64, etc.) |
| E110 | `PREREQUISITES_MISSING` | Required system components missing | Install required dependencies (Visual C++ Redistributable, etc.) |
| E111 | `TEMP_DIR_CREATION_FAILED` | Cannot create temporary directory | Check temp directory permissions: `%TEMP%` or `/tmp` |
| E112 | `DOWNLOAD_FAILED` | Failed to download installer components | Check internet connectivity and firewall settings |
| E113 | `TIMEOUT` | Installation timed out | Retry installation; check for system resource issues |
| E114 | `CLEANUP_FAILED` | Failed to clean up temporary files | Non-critical; can be ignored, temp files will be cleaned on reboot |
| E115 | `VERSION_CONFLICT` | Incompatible version detected | Uninstall existing version first, then retry |

---

## E2xx: Service Errors

Errors related to Windows Services, systemd units, or launchd daemons.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E200 | `SERVICE_GENERAL_FAILURE` | General service operation failure | Check Windows Event Viewer or system logs |
| E201 | `SERVICE_CREATE_FAILED` | Failed to create service | Ensure administrator privileges; check for service name conflicts |
| E202 | `SERVICE_START_FAILED` | Failed to start service | Check service dependencies and event logs |
| E203 | `SERVICE_STOP_FAILED` | Failed to stop service | Try stopping manually: `sc stop ServiceName` or `systemctl stop sentinel-agent` |
| E204 | `SERVICE_ALREADY_EXISTS` | Service with same name exists | Remove existing service or use `--force` flag |
| E205 | `SERVICE_NOT_FOUND` | Service does not exist | Service may have been manually removed; reinstall |
| E206 | `SERVICE_TIMEOUT` | Service operation timed out | Service may be hung; forcefully terminate and retry |
| E207 | `SERVICE_DEPENDENCY_FAILED` | Service dependency not met | Ensure network services are running |
| E208 | `SERVICE_DELETE_FAILED` | Failed to delete service | Stop service first; may require reboot |
| E209 | `SERVICE_ACCESS_DENIED` | Insufficient privileges for service operation | Run as Administrator/root |
| E210 | `SYSTEMD_RELOAD_FAILED` | Failed to reload systemd daemon | Run `systemctl daemon-reload` manually |
| E211 | `LAUNCHD_LOAD_FAILED` | Failed to load launchd plist | Check plist syntax and permissions |
| E212 | `LAUNCHD_UNLOAD_FAILED` | Failed to unload launchd plist | Service may not be running |
| E213 | `SERVICE_REGISTRY_ERROR` | Windows service registry error | Registry may be corrupted; run `sfc /scannow` |
| E214 | `SERVICE_HEALTH_CHECK_FAILED` | Service started but health check failed | Check agent logs for startup errors |

---

## E3xx: Configuration Errors

Errors related to configuration files and settings.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E300 | `CONFIG_GENERAL_FAILURE` | General configuration error | Check configuration file syntax |
| E301 | `CONFIG_INVALID_JSON` | Configuration file is not valid JSON | Validate JSON syntax; check for trailing commas |
| E302 | `CONFIG_MISSING_SERVER` | Server URL not specified | Provide `--server` parameter or set in config |
| E303 | `CONFIG_MISSING_TOKEN` | Enrollment token not specified | Generate token from Sentinel dashboard |
| E304 | `CONFIG_WRITE_FAILED` | Failed to write configuration file | Check file system permissions |
| E305 | `CONFIG_READ_FAILED` | Failed to read configuration file | Ensure config file exists and is readable |
| E306 | `CONFIG_PARSE_ERROR` | Failed to parse configuration | Check JSON syntax and encoding (UTF-8) |
| E307 | `CONFIG_INVALID_SERVER_URL` | Server URL format is invalid | Use format: `https://server.domain.com:port` |
| E308 | `CONFIG_INVALID_TOKEN` | Enrollment token format invalid | Tokens should be UUID format or alphanumeric |
| E309 | `CONFIG_FILE_LOCKED` | Configuration file is locked | Another process may be using the file |
| E310 | `CONFIG_BACKUP_FAILED` | Failed to backup existing configuration | Check backup directory permissions |
| E311 | `CONFIG_RESTORE_FAILED` | Failed to restore configuration from backup | Backup file may be corrupted |
| E312 | `CONFIG_MIGRATION_FAILED` | Failed to migrate configuration schema | Manual migration may be required |
| E313 | `CONFIG_ENCRYPTION_FAILED` | Failed to encrypt sensitive config data | Check system crypto provider |
| E314 | `CONFIG_DECRYPTION_FAILED` | Failed to decrypt configuration | Configuration may have been encrypted with different key |

---

## E4xx: Network Errors

Errors related to network connectivity and server communication.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E400 | `NETWORK_GENERAL_FAILURE` | General network error | Check internet connectivity |
| E401 | `SERVER_UNREACHABLE` | Cannot reach Sentinel server | Verify server URL and network connectivity |
| E402 | `TOKEN_INVALID` | Enrollment token is invalid | Generate a new token from the dashboard |
| E403 | `TOKEN_EXPIRED` | Enrollment token has expired | Generate a new token (tokens expire after configured time) |
| E404 | `TOKEN_MAX_USES` | Enrollment token has reached maximum uses | Generate a new token or increase max uses |
| E405 | `SSL_CERTIFICATE_ERROR` | SSL/TLS certificate verification failed | Check server certificate; use `--insecure` for testing only |
| E406 | `SSL_HANDSHAKE_FAILED` | TLS handshake failed | Check TLS version compatibility (requires TLS 1.2+) |
| E407 | `DNS_RESOLUTION_FAILED` | Cannot resolve server hostname | Check DNS settings and server hostname |
| E408 | `CONNECTION_TIMEOUT` | Connection to server timed out | Check firewall rules and network latency |
| E409 | `CONNECTION_REFUSED` | Server actively refused connection | Verify server is running and port is correct |
| E410 | `PROXY_ERROR` | Proxy configuration error | Check proxy settings and authentication |
| E411 | `HTTP_ERROR_401` | Authentication failed (Unauthorized) | Check enrollment token validity |
| E412 | `HTTP_ERROR_403` | Access forbidden | Token may be disabled or organization locked |
| E413 | `HTTP_ERROR_404` | Endpoint not found | Check server URL and API version |
| E414 | `HTTP_ERROR_500` | Server internal error | Contact server administrator |
| E415 | `DOWNLOAD_INTERRUPTED` | Download was interrupted | Retry download; check network stability |
| E416 | `ENROLLMENT_FAILED` | Device enrollment failed | Check server logs for details |

---

## E5xx: Upgrade Errors

Errors specific to upgrading an existing installation.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E500 | `UPGRADE_GENERAL_FAILURE` | General upgrade failure | Check logs for specific error |
| E501 | `UPGRADE_STOP_FAILED` | Failed to stop existing services | Manually stop services and retry |
| E502 | `UPGRADE_BACKUP_FAILED` | Failed to backup existing installation | Ensure backup directory is writable |
| E503 | `UPGRADE_ROLLBACK_FAILED` | Failed to rollback after upgrade failure | Manual intervention may be required |
| E504 | `UPGRADE_VERSION_DOWNGRADE` | Cannot downgrade to older version | Use `--force` to allow downgrade (not recommended) |
| E505 | `UPGRADE_CONFIG_MIGRATION` | Configuration migration required | Backup config and re-run installer |
| E506 | `UPGRADE_FILES_IN_USE` | Upgrade files are in use | Reboot and retry installation |
| E507 | `UPGRADE_INCOMPLETE` | Previous upgrade was incomplete | Run repair installation |
| E508 | `UPGRADE_CLEANUP_FAILED` | Failed to clean up old version | Old files may remain; non-critical |
| E509 | `UPGRADE_VERIFICATION_FAILED` | Post-upgrade verification failed | Check service status and logs |
| E510 | `UPGRADE_PERMISSION_CHANGED` | File permissions changed during upgrade | Reset permissions manually |
| E511 | `UPGRADE_DATABASE_MIGRATION` | Local database migration failed | Check agent logs for details |

---

## E6xx: Uninstall Errors

Errors specific to uninstalling the agent.

| Code | Name | Description | Resolution |
|------|------|-------------|------------|
| E600 | `UNINSTALL_GENERAL_FAILURE` | General uninstall failure | Check logs for specific error |
| E601 | `UNINSTALL_SERVICES_RUNNING` | Cannot uninstall while services are running | Stop services first |
| E602 | `UNINSTALL_FILES_IN_USE` | Installation files are in use | Close applications using files; may need reboot |
| E603 | `UNINSTALL_PERMISSION_DENIED` | Insufficient permissions | Run uninstaller as Administrator/root |
| E604 | `UNINSTALL_REGISTRY_CLEANUP` | Failed to clean registry entries | May need manual registry cleanup |
| E605 | `UNINSTALL_SERVICE_DELETE` | Failed to delete service | Try `sc delete ServiceName` manually |
| E606 | `UNINSTALL_FILES_REMAINING` | Some files could not be deleted | Files will be deleted on next reboot |
| E607 | `UNINSTALL_CONFIG_PRESERVED` | Configuration files were preserved | By design; remove manually if needed |
| E608 | `UNINSTALL_LOG_PRESERVED` | Log files were preserved | By design; remove manually if needed |
| E609 | `UNINSTALL_INCOMPLETE` | Uninstallation was incomplete | Manual cleanup may be required |

---

## Getting Help

### 1. Locate the Reference ID

When an error occurs, note the **Reference ID** (format: `INS-XXXXXX-YYYYMMDD`).

### 2. Find the Log File

**Windows:**
```
%TEMP%\Sentinel\install-INS-XXXXXX-YYYYMMDD.log
C:\ProgramData\Sentinel\logs\install.log
```

**Linux:**
```
/var/log/sentinel/install.log
/tmp/sentinel-install-*.log
```

**macOS:**
```
/var/log/sentinel-install.log
/tmp/sentinel-install-*.log
```

### 3. Contact Support

Include the following when contacting support:
- Reference ID
- Error code (E###)
- Log file contents
- Operating system and version
- Installer version

---

## Exit Codes

The installer returns these exit codes:

| Exit Code | Meaning |
|-----------|---------|
| 0 | Success |
| 1 | General failure |
| 2 | Invalid arguments |
| 3 | Permission denied |
| 4 | Network error |
| 5 | Configuration error |
| 6 | Service error |
| 7 | Upgrade error |
| 8 | User cancelled |
| 100+ | Windows-specific error codes |
