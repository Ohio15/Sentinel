@echo off
echo Stopping services...
net stop SentinelWatchdog
net stop SentinelAgent

echo Deleting services...
sc delete SentinelAgent
sc delete SentinelWatchdog

echo Services deleted. Check registry manually.
reg query HKLM\SYSTEM\CurrentControlSet\Services\SentinelAgent 2>nul
if %ERRORLEVEL% EQU 0 (
    echo SentinelAgent registry key still exists - manual deletion required
) else (
    echo SentinelAgent registry key deleted successfully
)

reg query HKLM\SYSTEM\CurrentControlSet\Services\SentinelWatchdog 2>nul
if %ERRORLEVEL% EQU 0 (
    echo SentinelWatchdog registry key still exists - manual deletion required
) else (
    echo SentinelWatchdog registry key deleted successfully
)

pause
