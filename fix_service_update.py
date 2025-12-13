filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Change the command to use schtasks which works better for services
# This creates a scheduled task that runs immediately and deletes itself after
old_cmd = 'cmd := exec.Command("cmd.exe", "/C", "start /min cmd.exe /C \\""+batchPath+"\\"")'

new_cmd = '''// Use schtasks to run the update script - this works reliably even from services
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

content = content.replace(old_cmd, new_cmd)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed service update command')
