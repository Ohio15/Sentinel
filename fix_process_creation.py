filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Change from schtasks to a simpler approach - use cmd.exe /c start with proper flags
old_code = '''// Use schtasks to run the update script - this works reliably even from services
	taskName := "SentinelAgentUpdate"
	// Delete any existing task first
	exec.Command("schtasks.exe", "/Delete", "/TN", taskName, "/F").Run()
	// Create a task to run immediately
	cmd := exec.Command("schtasks.exe", "/Create", "/TN", taskName, "/TR", batchPath, "/SC", "ONCE", "/ST", "00:00", "/RU", "SYSTEM", "/F")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create update task: %w", err)
	}
	// Run the task immediately
	cmd = exec.Command("schtasks.exe", "/Run", "/TN", taskName)'''

# Use wmic process call create which creates a truly detached process
new_code = '''// Use wmic to create a truly detached process that survives service shutdown
	cmd := exec.Command("wmic.exe", "process", "call", "create", batchPath)'''

content = content.replace(old_code, new_code)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Changed to wmic process creation')
