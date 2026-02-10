# Sentinel Agent Linux Packages

This directory contains build scripts and configuration for creating `.deb` (Debian/Ubuntu) and `.rpm` (RHEL/CentOS/Fedora) packages for the Sentinel RMM agent.

## Prerequisites

### Build Linux Binaries First

Before building packages, you must compile the Linux binaries. From the Sentinel project root:

```bash
cd D:/Projects/Sentinel/agent

# Build agent for Linux amd64
GOOS=linux GOARCH=amd64 go build -o ../installers/sentinel-agent-linux-amd64 ./cmd/sentinel-agent

# Build watchdog for Linux amd64
GOOS=linux GOARCH=amd64 go build -o ../installers/sentinel-watchdog-linux-amd64 ./cmd/sentinel-watchdog
```

### Package Build Requirements

**For .deb packages (Debian/Ubuntu):**
- `dpkg-deb` (pre-installed on Debian/Ubuntu)
- `fakeroot` (optional, for non-root builds)

**For .rpm packages (RHEL/CentOS/Fedora):**
- `rpmbuild` from the `rpm-build` package
  - Fedora/RHEL: `sudo dnf install rpm-build`
  - CentOS: `sudo yum install rpm-build`

## Building Packages

### Make Scripts Executable (Linux only)

```bash
chmod +x build-deb.sh build-rpm.sh
```

### Build .deb Package

```bash
./build-deb.sh 1.70.0
```

Output: `output/sentinel-agent_1.70.0_amd64.deb`

### Build .rpm Package

```bash
./build-rpm.sh 1.70.0
```

Output: `output/sentinel-agent-1.70.0-1.x86_64.rpm`

## Package Contents

Both packages install:

| Component | Location |
|-----------|----------|
| Agent binary | `/usr/local/bin/sentinel-agent` |
| Watchdog binary | `/usr/local/bin/sentinel-watchdog` |
| Configuration | `/etc/sentinel/config.json` |
| Log directory | `/var/log/sentinel/` |
| Agent service | `/etc/systemd/system/sentinel-agent.service` |
| Watchdog service | `/etc/systemd/system/sentinel-watchdog.service` |

## Post-Installation

After installing the package:

1. **Edit configuration** with your server details:
   ```bash
   sudo nano /etc/sentinel/config.json
   ```

2. **Start services:**
   ```bash
   sudo systemctl start sentinel-agent
   sudo systemctl start sentinel-watchdog
   ```

3. **View logs:**
   ```bash
   sudo journalctl -u sentinel-agent -f
   sudo journalctl -u sentinel-watchdog -f
   ```

## Uninstallation

### Debian/Ubuntu

```bash
# Remove but keep config and logs
sudo apt remove sentinel-agent

# Complete removal including config and logs
sudo apt purge sentinel-agent
```

### RHEL/CentOS/Fedora

```bash
# Remove package
sudo dnf remove sentinel-agent

# Manually remove config and logs if desired
sudo rm -rf /etc/sentinel /var/log/sentinel
sudo userdel sentinel
```

## Directory Structure

```
linux/
├── README.md                       # This file
├── build-deb.sh                    # Build script for .deb
├── build-rpm.sh                    # Build script for .rpm
├── debian/
│   ├── control                     # Package metadata
│   ├── conffiles                   # Config files to preserve on upgrade
│   ├── postinst                    # Post-install script
│   ├── prerm                       # Pre-remove script
│   └── postrm                      # Post-remove script
├── rpm/
│   └── sentinel-agent.spec         # RPM spec file
├── systemd/
│   ├── sentinel-agent.service      # Systemd unit for agent
│   └── sentinel-watchdog.service   # Systemd unit for watchdog
└── output/                         # Built packages (created by scripts)
```

## Security Notes

- The `sentinel` user is created as a system user with no login shell
- Services run with systemd security hardening (NoNewPrivileges, ProtectSystem, etc.)
- The watchdog runs as root to monitor and restart the agent if needed
- Configuration file has restricted permissions (640, owned by sentinel:sentinel)
