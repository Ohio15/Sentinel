@echo off
REM Deploy web frontend to remote server
REM Usage: scripts\deploy-web.bat

setlocal

set REMOTE_HOST=REDACTED_SSH_TARGET
set REMOTE_PATH=D:/Projects/Sentinel
set LOCAL_PATH=D:\Projects\Sentinel

echo === Building web frontend ===
cd /d %LOCAL_PATH%
call npm run build:web
if errorlevel 1 goto :error

echo === Copying portal files to build output ===
if not exist "%LOCAL_PATH%\dist\web\portal" mkdir "%LOCAL_PATH%\dist\web\portal"
copy /Y "%LOCAL_PATH%\src\portal\*" "%LOCAL_PATH%\dist\web\portal\"

echo === Cleaning old assets on remote ===
ssh %REMOTE_HOST% "cd /d %REMOTE_PATH%/dist/web/assets && del /Q *.js *.css 2>nul || echo No old assets"

echo === Deploying to remote server ===
scp -r "%LOCAL_PATH%\dist\web\*" "%REMOTE_HOST%:%REMOTE_PATH%/dist/web/"
if errorlevel 1 goto :error

echo === Verifying deployment ===
ssh %REMOTE_HOST% "docker exec sentinel-frontend cat /usr/share/nginx/html/index.html"

echo.
echo === Deployment complete ===
echo Clear browser cache (Ctrl+Shift+R) to see changes
goto :end

:error
echo Deployment failed!
exit /b 1

:end
endlocal
