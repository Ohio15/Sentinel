@echo off
REM Read configuration from environment or prompt
if "%SENTINEL_SERVER%"=="" set SENTINEL_SERVER=http://localhost:8081
if "%SENTINEL_TOKEN%"=="" (
    echo No SENTINEL_TOKEN environment variable set.
    set /p SENTINEL_TOKEN="Enter enrollment token: "
)

echo Installing Sentinel Agent as service...
"D:\Projects\Sentinel\downloads\sentinel-agent.exe" --install --server=%SENTINEL_SERVER% --token=%SENTINEL_TOKEN%
echo.
echo Checking service status...
sc query SentinelAgent
pause
