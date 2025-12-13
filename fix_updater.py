import re

filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Replace the batch content format string
old_pattern = r'batchContent := fmt\.Sprintf\(`@echo off\nsetlocal enabledelayedexpansion\nset LOG_FILE=%s\necho \[%%%%date%%%% %%%%time%%%%\] Starting update to v%s > "%%%%LOG_FILE%%%%"\ntimeout /t 3 /nobreak > nul\nsc query SentinelAgent \| find "STOPPED" > nul\nif %%%%errorlevel%%%% neq 0 \(\n    net stop SentinelAgent /y\n    timeout /t 2 /nobreak > nul\n\)\nif exist "%s" del /f "%s" 2>nul\nmove /y "%s" "%s"\nif %%%%errorlevel%%%% neq 0 goto :restart_old\nmove /y "%s" "%s"\nif %%%%errorlevel%%%% neq 0 goto :rollback\nnet start SentinelAgent\ntimeout /t 3 /nobreak > nul\nsc query SentinelAgent \| find "RUNNING" > nul\nif %%%%errorlevel%%%% neq 0 goto :rollback\ndel /f "%s" 2>nul\ngoto :cleanup\n:rollback\nnet stop SentinelAgent /y 2>nul\ndel /f "%s" 2>nul\nmove /y "%s" "%s"\n:restart_old\nnet start SentinelAgent\n:cleanup\ndel /f "%s" 2>nul\n`, logPath, newVersion, backupPath, backupPath, currentExe, backupPath, downloadPath, currentExe, backupPath, currentExe, backupPath, currentExe, batchPath\)'

new_content = '''batchContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
set LOG_FILE=%s
echo [%%%%date%%%% %%%%time%%%%] Starting update to v%s > "%%%%LOG_FILE%%%%"
echo [%%%%date%%%% %%%%time%%%%] Current exe: %s >> "%%%%LOG_FILE%%%%"
echo [%%%%date%%%% %%%%time%%%%] Download path: %s >> "%%%%LOG_FILE%%%%"
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "STOPPED" > nul
if %%%%errorlevel%%%% neq 0 (
    echo [%%%%date%%%% %%%%time%%%%] Stopping service... >> "%%%%LOG_FILE%%%%"
    net stop SentinelAgent /y
    timeout /t 2 /nobreak > nul
)
echo [%%%%date%%%% %%%%time%%%%] Deleting old backup if exists >> "%%%%LOG_FILE%%%%"
if exist "%s" del /f "%s" 2>nul
echo [%%%%date%%%% %%%%time%%%%] Moving current to backup >> "%%%%LOG_FILE%%%%"
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 (
    echo [%%%%date%%%% %%%%time%%%%] Failed to backup current exe >> "%%%%LOG_FILE%%%%"
    goto :restart_old
)
echo [%%%%date%%%% %%%%time%%%%] Moving new exe into place >> "%%%%LOG_FILE%%%%"
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 (
    echo [%%%%date%%%% %%%%time%%%%] Failed to install new exe >> "%%%%LOG_FILE%%%%"
    goto :rollback
)
echo [%%%%date%%%% %%%%time%%%%] Starting service... >> "%%%%LOG_FILE%%%%"
net start SentinelAgent
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "RUNNING" > nul
if %%%%errorlevel%%%% neq 0 (
    echo [%%%%date%%%% %%%%time%%%%] Service failed to start, rolling back >> "%%%%LOG_FILE%%%%"
    goto :rollback
)
echo [%%%%date%%%% %%%%time%%%%] Update successful! >> "%%%%LOG_FILE%%%%"
del /f "%s" 2>nul
goto :cleanup
:rollback
echo [%%%%date%%%% %%%%time%%%%] Rolling back... >> "%%%%LOG_FILE%%%%"
net stop SentinelAgent /y 2>nul
del /f "%s" 2>nul
move /y "%s" "%s"
:restart_old
echo [%%%%date%%%% %%%%time%%%%] Restarting old version >> "%%%%LOG_FILE%%%%"
net start SentinelAgent
:cleanup
echo [%%%%date%%%% %%%%time%%%%] Cleanup complete >> "%%%%LOG_FILE%%%%"
`, logPath, newVersion, currentExe, downloadPath,
		backupPath, backupPath,
		currentExe, backupPath,
		downloadPath, currentExe,
		backupPath,
		currentExe, backupPath, currentExe)'''

# Simple string replace for the batch content section
content = content.replace(
'''	batchContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
set LOG_FILE=%s
echo [%%%%date%%%% %%%%time%%%%] Starting update to v%s > "%%%%LOG_FILE%%%%"
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "STOPPED" > nul
if %%%%errorlevel%%%% neq 0 (
    net stop SentinelAgent /y
    timeout /t 2 /nobreak > nul
)
if exist "%s" del /f "%s" 2>nul
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 goto :restart_old
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 goto :rollback
net start SentinelAgent
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "RUNNING" > nul
if %%%%errorlevel%%%% neq 0 goto :rollback
del /f "%s" 2>nul
goto :cleanup
:rollback
net stop SentinelAgent /y 2>nul
del /f "%s" 2>nul
move /y "%s" "%s"
:restart_old
net start SentinelAgent
:cleanup
del /f "%s" 2>nul
`, logPath, newVersion, backupPath, backupPath, currentExe, backupPath, downloadPath, currentExe, backupPath, currentExe, backupPath, currentExe, batchPath)''',
new_content)

# Also fix the command execution
content = content.replace(
'	cmd := exec.Command("cmd.exe", "/C", "net stop SentinelAgent && start /min cmd.exe /C "+batchPath)',
'''	log.Printf("Created update script at %s", batchPath)
	log.Printf("Update will replace %s with %s", currentExe, downloadPath)

	cmd := exec.Command("cmd.exe", "/C", "start /min cmd.exe /C \\""+batchPath+"\\"")''')

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('File updated successfully')
