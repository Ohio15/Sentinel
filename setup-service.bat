@echo off
echo === Sentinel Agent Service Setup ===
echo.

REM Read configuration from environment or prompt
if "%SENTINEL_SERVER%"=="" set SENTINEL_SERVER=http://localhost:8081
if "%SENTINEL_TOKEN%"=="" (
    echo No SENTINEL_TOKEN environment variable set.
    set /p SENTINEL_TOKEN="Enter enrollment token: "
)

echo [1/5] Taking ownership of install directory...
takeown /f "C:\Program Files\Sentinel Agent" /r /d y >nul 2>&1
icacls "C:\Program Files\Sentinel Agent" /grant Administrators:F /t >nul 2>&1

echo [2/5] Stopping any running agent...
taskkill /f /im sentinel-agent.exe >nul 2>&1

echo [3/5] Copying agent binary...
copy /Y "D:\Projects\Sentinel\downloads\sentinel-agent.exe" "C:\Program Files\Sentinel Agent\sentinel-agent.exe"
if errorlevel 1 (
    echo ERROR: Failed to copy even after taking ownership.
    echo Trying alternate method - recreating directory...
    rmdir /s /q "C:\Program Files\Sentinel Agent" 2>nul
    mkdir "C:\Program Files\Sentinel Agent"
    copy /Y "D:\Projects\Sentinel\downloads\sentinel-agent.exe" "C:\Program Files\Sentinel Agent\sentinel-agent.exe"
    if errorlevel 1 (
        echo FATAL: Cannot copy file. Please check permissions.
        pause
        exit /b 1
    )
)

echo [4/5] Creating/updating service...
sc query SentinelAgent >nul 2>&1
if errorlevel 1 (
    echo Service does not exist, creating...
    sc create SentinelAgent binPath= ""C:\Program Files\Sentinel Agent\sentinel-agent.exe" --server=%SENTINEL_SERVER% --token=%SENTINEL_TOKEN% --service" start= auto DisplayName= "Sentinel Agent"
) else (
    echo Service exists, updating configuration...
    sc config SentinelAgent binPath= ""C:\Program Files\Sentinel Agent\sentinel-agent.exe" --server=%SENTINEL_SERVER% --token=%SENTINEL_TOKEN% --service"
)

echo [5/5] Starting service...
sc start SentinelAgent

echo.
echo === Service Status ===
sc query SentinelAgent

echo.
echo === Checking agent version ===
"C:\Program Files\Sentinel Agent\sentinel-agent.exe" --version 2>nul || echo Could not get version

echo.
echo Done\! The agent should now connect to %SENTINEL_SERVER%
pause
