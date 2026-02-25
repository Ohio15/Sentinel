#!/usr/bin/env python3
"""
Create a scheduled task on the Windows agent that will:
1. Download the new agent binary
2. Stop services
3. Replace the binary
4. Start services

This runs as a SYSTEM scheduled task, independent of the agent process.
"""
import json
import urllib.request
import ssl
import sys

API_KEY = "55ccf1fd8b1d937fd9377a5c306eaf675e00a5876e1cd33e5ac1c602f7559168"
BASE_URL = "https://localhost"

def run_cmd(device_id, cmd, cmd_type="cmd"):
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    data = json.dumps({"command": cmd, "commandType": cmd_type}).encode("utf-8")
    req = urllib.request.Request(
        f"{BASE_URL}/api/devices/{device_id}/commands",
        data=data,
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as resp:
            return resp.status, resp.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 recovery-task.py <device_id>")
        sys.exit(1)

    device_id = sys.argv[1]

    # Create the recovery batch file using schtasks to run cmd commands
    # The task will run the curl, sc stop, copy, sc start sequence

    print("Creating recovery scheduled task...")

    # The scheduled task command that runs all recovery steps
    # Using cmd /c to run multiple commands with &&
    task_cmd = (
        'cmd /c "'
        'cd /d %TEMP% && '
        'curl -k -o sa.exe https://sentinelrmm.us:8443/api/agent/update/download?platform=windows && '
        'sc stop SentinelWatchdog && '
        'sc stop SentinelAgent && '
        'ping -n 6 127.0.0.1 >nul && '
        'taskkill /F /IM sentinel-watchdog.exe 2>nul && '
        'taskkill /F /IM sentinel-agent.exe 2>nul && '
        'ping -n 3 127.0.0.1 >nul && '
        'copy /Y sa.exe \\"C:\\Program Files\\Sentinel\\sentinel-agent.exe\\" && '
        'del sa.exe && '
        'sc start SentinelWatchdog && '
        'sc start SentinelAgent && '
        'schtasks /delete /tn AgentRecovery /f'
        '"'
    )

    # Create the scheduled task
    create_task = f'schtasks /create /tn AgentRecovery /tr "{task_cmd}" /sc once /st 00:00 /ru SYSTEM /f /rl HIGHEST'

    status, resp = run_cmd(device_id, create_task)
    print(f"Create task: {status}")
    if status != 200:
        print(f"  Error: {resp}")
        return

    # Run the task immediately
    print("Running recovery task...")
    status, resp = run_cmd(device_id, "schtasks /run /tn AgentRecovery")
    print(f"Run task: {status}")

    print("\nRecovery task started. The agent should update within 60 seconds.")

if __name__ == "__main__":
    main()
