@echo off
REM Deploy web frontend to remote server
REM Usage: scripts\deploy-web.bat

setlocal

set REMOTE_HOST=REDACTED_SSH_TARGET
set REMOTE_PATH=~/Sentinel
set LOCAL_PATH=D:\Projects\Sentinel

echo === Building web frontend ===
cd /d %LOCAL_PATH%
call npm run build:web
if errorlevel 1 goto :error

echo === Copying portal files to build output ===
if not exist "%LOCAL_PATH%\dist\web\portal" mkdir "%LOCAL_PATH%\dist\web\portal"
copy /Y "%LOCAL_PATH%\src\portal\*" "%LOCAL_PATH%\dist\web\portal\"

echo === Cleaning old assets on remote ===
ssh %REMOTE_HOST% "rm -f %REMOTE_PATH%/dist/web/assets/*.js %REMOTE_PATH%/dist/web/assets/*.css 2>/dev/null; echo Old assets cleaned"

echo === Deploying to remote server ===
scp -r "%LOCAL_PATH%\dist\web\*" "%REMOTE_HOST%:%REMOTE_PATH%/dist/web/"
if errorlevel 1 goto :error

echo === Verifying deployment ===
ssh %REMOTE_HOST% "ls -la %REMOTE_PATH%/dist/web/assets/index-*.js %REMOTE_PATH%/dist/web/assets/index-*.css"

echo.
echo === Deployment complete ===
echo Clear browser cache (Ctrl+Shift+R) to see changes
goto :end

:error
echo Deployment failed!
exit /b 1

:end
endlocal
