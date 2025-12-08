# Sentinel Agent

Production-ready remote monitoring and management (RMM) agent for Windows, macOS, and Linux.

## Features

- **System Metrics Collection**: CPU, memory, disk, network usage monitoring
- **Remote Command Execution**: Execute shell commands and scripts
- **Terminal Sessions**: Interactive remote terminal access
- **File Transfer**: Upload and download files with progress tracking
- **Auto-Reconnection**: Resilient connection with exponential backoff
- **Service Installation**: Runs as a system service on all platforms
- **Secure Authentication**: Token-based enrollment and WebSocket auth

## Prerequisites

- Go 1.21 or later
- Administrator/root privileges for service installation

## Building

### Windows (PowerShell)

```powershell
cd agent
.\build.ps1 -Platform all
```

Or build specific platform:
```powershell
.\build.ps1 -Platform windows
.\build.ps1 -Platform linux
.\build.ps1 -Platform macos
```

### Linux/macOS (Make)

```bash
cd agent
make all           # Build all platforms
make windows       # Windows only
make linux         # Linux only
make macos         # macOS only
```

Build artifacts are placed in the `downloads/` directory.

## Installation

### Windows

Run as Administrator:
```powershell
.\sentinel-agent.exe --install --server=http://SERVER_IP:8080 --token=ENROLLMENT_TOKEN
```

### Linux

Run as root:
```bash
sudo ./sentinel-agent-linux --install --server=http://SERVER_IP:8080 --token=ENROLLMENT_TOKEN
```

### macOS

Run as root:
```bash
sudo ./sentinel-agent-macos --install --server=http://SERVER_IP:8080 --token=ENROLLMENT_TOKEN
```

## Command Line Options

| Option | Description |
|--------|-------------|
| `--server` | Sentinel server URL (e.g., http://192.168.1.100:8080) |
| `--token` | Enrollment token from the Sentinel dashboard |
| `--install` | Install as system service |
| `--uninstall` | Uninstall the system service |
| `--status` | Show service status |
| `--version` | Show version information |

## Service Management

### Windows
```powershell
# Start/stop service
net start SentinelAgent
net stop SentinelAgent

# Check status
sc query SentinelAgent
```

### Linux (systemd)
```bash
sudo systemctl start sentinel-agent
sudo systemctl stop sentinel-agent
sudo systemctl status sentinel-agent
sudo journalctl -u sentinel-agent -f  # View logs
```

### macOS (launchd)
```bash
sudo launchctl start com.sentinel.agent
sudo launchctl stop com.sentinel.agent
```

## Configuration

Configuration is stored at:
- **Windows**: `C:\ProgramData\Sentinel\config.json`
- **Linux**: `/etc/sentinel/config.json`
- **macOS**: `/Library/Application Support/Sentinel/config.json`

## Logs

Logs are stored at:
- **Windows**: `C:\ProgramData\Sentinel\logs\`
- **Linux**: `/var/log/sentinel/`
- **macOS**: `/Library/Logs/Sentinel/`

## Architecture

```
agent/
├── cmd/sentinel-agent/     # Main application entry point
└── internal/
    ├── client/            # WebSocket client for server communication
    ├── collector/         # System metrics collection
    ├── config/            # Configuration management
    ├── executor/          # Command and script execution
    ├── filetransfer/      # File operations
    ├── service/           # Service installation
    └── terminal/          # Terminal session management
```

## Protocol

The agent communicates with the server via WebSocket using JSON messages:

### Message Types

| Type | Direction | Description |
|------|-----------|-------------|
| `auth` | Agent → Server | Authentication request |
| `auth_response` | Server → Agent | Authentication result |
| `heartbeat` | Agent → Server | Keep-alive signal |
| `heartbeat_ack` | Server → Agent | Heartbeat acknowledgment |
| `metrics` | Agent → Server | System metrics data |
| `execute_command` | Server → Agent | Run shell command |
| `execute_script` | Server → Agent | Run script file |
| `start_terminal` | Server → Agent | Start terminal session |
| `terminal_input` | Server → Agent | Terminal input data |
| `terminal_output` | Agent → Server | Terminal output data |
| `list_files` | Server → Agent | List directory contents |
| `download_file` | Server → Agent | Download file from agent |
| `upload_file` | Server → Agent | Upload file to agent |

## Security Considerations

- Agent authenticates using enrollment tokens
- Commands are executed with the service account privileges
- File transfers use base64 encoding with chunking
- Sensitive configuration is stored with restricted permissions

## Troubleshooting

### Agent won't start
1. Check if the server URL is correct
2. Verify the enrollment token is valid
3. Check firewall rules allow outbound connections

### Agent keeps disconnecting
1. Verify network connectivity to server
2. Check server logs for authentication failures
3. Ensure no VPN or proxy is blocking WebSocket connections

### Commands not executing
1. Verify the agent has necessary permissions
2. Check the command type matches the OS
3. Review agent logs for execution errors
