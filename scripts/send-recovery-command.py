#!/usr/bin/env python3
"""
Remote Agent Recovery - Sends a PowerShell command to update a Windows agent
Usage: python3 send-recovery-command.py <device_id>
"""
import json
import sys
import urllib.request
import ssl

API_KEY = "55ccf1fd8b1d937fd9377a5c306eaf675e00a5876e1cd33e5ac1c602f7559168"
BASE_URL = "https://localhost"

# PowerShell script that creates a scheduled task for recovery
PS_SCRIPT = r'''
$script = @'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$url = "https://sentinelrmm.us:8443/api/agent/update/download?platform=windows&arch=amd64"
$tmp = "$env:TEMP\sentinel-update.exe"
$agent = "C:\Program Files\Sentinel\sentinel-agent.exe"
try {
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing -TimeoutSec 180
    Stop-Service SentinelWatchdog -Force -ErrorAction SilentlyContinue
    Stop-Service SentinelAgent -Force -ErrorAction SilentlyContinue
    Start-Sleep 3
    Copy-Item $tmp $agent -Force
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Start-Service SentinelWatchdog
    Start-Service SentinelAgent
} catch {
    Start-Service SentinelAgent -ErrorAction SilentlyContinue
}
schtasks /delete /tn SentinelRecovery /f 2>$null
'@
$enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($script))
$time = (Get-Date).AddSeconds(30).ToString("HH:mm")
schtasks /delete /tn SentinelRecovery /f 2>$null
schtasks /create /tn SentinelRecovery /tr "powershell -ep bypass -enc $enc" /sc once /st $time /ru SYSTEM /f /rl HIGHEST
Write-Output "Recovery scheduled for $time"
'''

def send_recovery_command(device_id):
    url = f"{BASE_URL}/api/devices/{device_id}/commands"

    data = json.dumps({
        "command": PS_SCRIPT,
        "commandType": "powershell"
    }).encode('utf-8')

    # Create SSL context that doesn't verify (for localhost)
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    req = urllib.request.Request(
        url,
        data=data,
        headers={
            "X-API-Key": API_KEY,
            "Content-Type": "application/json"
        },
        method="POST"
    )

    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as response:
            result = response.read().decode('utf-8')
            print(f"Status: {response.status}")
            print(f"Response: {result}")
            return True
    except urllib.error.HTTPError as e:
        print(f"HTTP Error: {e.code}")
        print(f"Response: {e.read().decode('utf-8')}")
        return False
    except Exception as e:
        print(f"Error: {e}")
        return False

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 send-recovery-command.py <device_id>")
        print("Example: python3 send-recovery-command.py 3d762e83-0bea-4b39-b2c7-2e50427c7fdf")
        sys.exit(1)

    device_id = sys.argv[1]
    print(f"Sending recovery command to device {device_id}...")
    success = send_recovery_command(device_id)
    sys.exit(0 if success else 1)
