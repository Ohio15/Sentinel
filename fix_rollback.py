filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Fix the rollback function that was accidentally modified
old = '''	if runtime.GOOS == "windows" {
		batchPath := filepath.Join(os.TempDir(), "sentinel-rollback.bat")
		batchContent := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak > nul
net stop SentinelAgent /y
timeout /t 2 /nobreak > nul
del /f "%s"
move /y "%s" "%s"
net start SentinelAgent
del /f "%s"
`, currentExe, backupPath, currentExe, batchPath)
		os.WriteFile(batchPath, []byte(batchContent), 0755)
		log.Printf("Created update script at %s", batchPath)
	log.Printf("Update will replace %s with %s", currentExe, downloadPath)

	cmd := exec.Command("cmd.exe", "/C", "start /min cmd.exe /C \\""+batchPath+"\\"")
		return cmd.Start()
	}'''

new = '''	if runtime.GOOS == "windows" {
		batchPath := filepath.Join(os.TempDir(), "sentinel-rollback.bat")
		batchContent := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak > nul
net stop SentinelAgent /y
timeout /t 2 /nobreak > nul
del /f "%s"
move /y "%s" "%s"
net start SentinelAgent
del /f "%s"
`, currentExe, backupPath, currentExe, batchPath)
		os.WriteFile(batchPath, []byte(batchContent), 0755)
		cmd := exec.Command("cmd.exe", "/C", "net stop SentinelAgent && start /min cmd.exe /C "+batchPath)
		return cmd.Start()
	}'''

content = content.replace(old, new)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed rollback function')
