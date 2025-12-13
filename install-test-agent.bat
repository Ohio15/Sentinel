@echo off
echo Installing Sentinel Agent v1.17.0 as service...
"D:\Projects\Sentinel\downloads\sentinel-agent-v1.17.0.exe" --install --server=http://localhost:8081 --token=40addfff-a1c0-4825-8e70-ca422dffd90e
echo.
echo Checking service status...
sc query SentinelAgent
pause
