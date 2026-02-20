#!/usr/bin/env python3
"""
Remote Agent Recovery - Two-step approach:
1. Upload a batch file to the agent
2. Execute the batch file to perform recovery

Usage: python3 send-recovery-command.py <device_id>
"""
import json
import sys
import urllib.request
import ssl
import base64
import time

import os
API_KEY = os.environ.get("SENTINEL_API_KEY", "")
BASE_URL = os.environ.get("SENTINEL_URL", "https://localhost")
if not API_KEY:
    print("Error: Set SENTINEL_API_KEY environment variable")
    sys.exit(1)

# Batch file content for recovery
BATCH_CONTENT = r"""@echo off
cd /d %TEMP%
curl -k -o sa.exe https://sentinelrmm.us:8443/api/agent/update/download?platform=windows^&arch=amd64
if not exist sa.exe exit /b 1
sc stop SentinelWatchdog
sc stop SentinelAgent
ping -n 5 127.0.0.1 > nul
taskkill /F /IM sentinel-watchdog.exe 2>nul
taskkill /F /IM sentinel-agent.exe 2>nul
ping -n 3 127.0.0.1 > nul
copy /Y sa.exe "C:\Program Files\Sentinel\sentinel-agent.exe"
del sa.exe
sc start SentinelWatchdog
sc start SentinelAgent
del "%~f0"
"""

def make_request(url, data, method="POST"):
    """Make an API request"""
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    req = urllib.request.Request(
        url,
        data=json.dumps(data).encode('utf-8') if data else None,
        headers={
            "X-API-Key": API_KEY,
            "Content-Type": "application/json"
        },
        method=method
    )

    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as response:
            return response.status, response.read().decode('utf-8')
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8')
    except Exception as e:
        return 0, str(e)

def send_websocket_message(device_id, msg_type, payload):
    """Send a WebSocket message via the command API"""
    # We need to use the file upload endpoint or similar
    # For now, use a direct hub message if available
    pass

def upload_file(device_id, remote_path, content):
    """Upload a file to the agent using the upload_file message"""
    # The agent's handleUploadFile expects:
    # - remotePath: destination path
    # - data: base64 encoded file content
    # - append: boolean

    # Need to send this via WebSocket, not REST API
    # Let's try a different approach - use the command to create the file
    pass

def execute_recovery(device_id):
    print(f"Step 1: Creating recovery batch file on agent...")

    # Use echo commands to create the batch file
    # Split into smaller chunks to avoid length issues
    lines = BATCH_CONTENT.strip().split('\n')

    # First create the file with the first line
    create_cmd = f'echo {lines[0]} > %TEMP%\\recovery.bat'

    status, response = make_request(
        f"{BASE_URL}/api/devices/{device_id}/commands",
        {"command": create_cmd, "commandType": "shell"}
    )
    print(f"  Create file: {status} - {response[:200] if len(response) > 200 else response}")

    if status != 200:
        return False

    time.sleep(1)

    # Append remaining lines
    for line in lines[1:]:
        if line.strip():
            # Escape special characters
            escaped_line = line.replace('^', '^^').replace('&', '^&').replace('|', '^|').replace('<', '^<').replace('>', '^>').replace('%', '%%')
            append_cmd = f'echo {escaped_line} >> %TEMP%\\recovery.bat'
            status, response = make_request(
                f"{BASE_URL}/api/devices/{device_id}/commands",
                {"command": append_cmd, "commandType": "shell"}
            )
            if status != 200:
                print(f"  Warning: Failed to append line: {response[:100]}")
            time.sleep(0.5)

    print(f"Step 2: Executing recovery batch file...")

    # Create a scheduled task to run the batch file
    exec_cmd = r'schtasks /create /tn SR /tr "%TEMP%\recovery.bat" /sc once /st 00:00 /ru SYSTEM /f /rl HIGHEST'
    status, response = make_request(
        f"{BASE_URL}/api/devices/{device_id}/commands",
        {"command": exec_cmd, "commandType": "shell"}
    )
    print(f"  Create task: {status} - {response[:200] if len(response) > 200 else response}")

    if status != 200:
        return False

    time.sleep(1)

    # Run the scheduled task
    run_cmd = 'schtasks /run /tn SR'
    status, response = make_request(
        f"{BASE_URL}/api/devices/{device_id}/commands",
        {"command": run_cmd, "commandType": "shell"}
    )
    print(f"  Run task: {status} - {response[:200] if len(response) > 200 else response}")

    return status == 200

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 send-recovery-command.py <device_id>")
        sys.exit(1)

    device_id = sys.argv[1]
    print(f"Starting recovery for device {device_id}...")
    success = execute_recovery(device_id)
    print(f"\nRecovery {'initiated' if success else 'failed'}")
    sys.exit(0 if success else 1)
