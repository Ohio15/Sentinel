#!/usr/bin/env python3
"""Execute recovery commands step by step."""
import json
import urllib.request
import ssl
import time
import sys

API_KEY = "55ccf1fd8b1d937fd9377a5c306eaf675e00a5876e1cd33e5ac1c602f7559168"
BASE_URL = "https://localhost"

def run_cmd(device_id, cmd):
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    data = json.dumps({"command": cmd, "commandType": "cmd"}).encode("utf-8")
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
        print("Usage: python3 recovery-steps.py <device_id>")
        sys.exit(1)

    device_id = sys.argv[1]

    # Step 1: Download the new agent
    print("Step 1: Downloading agent...")
    status, resp = run_cmd(device_id, "curl -k -o %TEMP%\\sa.exe https://sentinelrmm.us:8443/api/agent/update/download?platform=windows")
    print(f"  Status: {status}")
    time.sleep(10)

    # Step 2: Stop services
    print("Step 2: Stopping services...")
    status, resp = run_cmd(device_id, "sc stop SentinelWatchdog")
    print(f"  Watchdog: {status}")
    time.sleep(2)
    status, resp = run_cmd(device_id, "sc stop SentinelAgent")
    print(f"  Agent: {status}")
    time.sleep(5)

    # Step 3: Kill processes
    print("Step 3: Killing processes...")
    status, resp = run_cmd(device_id, "taskkill /F /IM sentinel-watchdog.exe")
    print(f"  Watchdog: {status}")
    status, resp = run_cmd(device_id, "taskkill /F /IM sentinel-agent.exe")
    print(f"  Agent: {status}")
    time.sleep(3)

    # Step 4: Copy new binary
    print("Step 4: Copying new binary...")
    copy_cmd = 'copy /Y %TEMP%\\sa.exe "C:\\Program Files\\Sentinel\\sentinel-agent.exe"'
    status, resp = run_cmd(device_id, copy_cmd)
    print(f"  Copy: {status}")
    time.sleep(2)

    # Step 5: Start services
    print("Step 5: Starting services...")
    status, resp = run_cmd(device_id, "sc start SentinelWatchdog")
    print(f"  Watchdog: {status}")
    time.sleep(2)
    status, resp = run_cmd(device_id, "sc start SentinelAgent")
    print(f"  Agent: {status}")

    print("\nRecovery commands sent. Waiting 30 seconds for agent to reconnect...")
    time.sleep(30)

if __name__ == "__main__":
    main()
