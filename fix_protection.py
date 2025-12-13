filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add protection import
old_imports = '''"time"
)'''

new_imports = '''"time"

	"github.com/sentinel/agent/internal/protection"
)'''

content = content.replace(old_imports, new_imports)

# Add DisableProtections call before creating batch script
old_apply = '''func (u *Updater) applyUpdateWindows(currentExe, downloadPath, newVersion string) error {
	u.updateStatus(StateRestarting, "Installing update...", 50)

	batchPath := filepath.Join(os.TempDir(), "sentinel-update.bat")'''

new_apply = '''func (u *Updater) applyUpdateWindows(currentExe, downloadPath, newVersion string) error {
	u.updateStatus(StateRestarting, "Installing update...", 50)

	// Disable file protections before update
	installPath := filepath.Dir(currentExe)
	protMgr := protection.NewManager(installPath, "SentinelAgent")
	if err := protMgr.DisableProtections(); err != nil {
		log.Printf("Warning: failed to disable protections: %v", err)
	} else {
		log.Println("File protections disabled for update")
	}

	batchPath := filepath.Join(os.TempDir(), "sentinel-update.bat")'''

content = content.replace(old_apply, new_apply)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Added protection disable call to updater')
