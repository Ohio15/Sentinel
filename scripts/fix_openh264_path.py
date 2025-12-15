import re

with open('agent/internal/webrtc/webrtc.go', 'r', encoding='utf-8') as f:
    content = f.read()

old_load = '''// loadOpenH264 loads the OpenH264 DLL
func (m *Manager) loadOpenH264() error {
	var loadErr error
	m.h264LoadOnce.Do(func() {
		// Try multiple possible locations for the OpenH264 DLL
		possiblePaths := []string{
			"openh264-2.4.1-win64.dll",
			"./openh264-2.4.1-win64.dll",
			filepath.Join(filepath.Dir(os.Args[0]), "openh264-2.4.1-win64.dll"),
			"C:\\\\Program Files\\\\Sentinel Agent\\\\openh264-2.4.1-win64.dll",
		}

		for _, path := range possiblePaths {
			if err := openh264.Open(path); err == nil {
				log.Printf("Loaded OpenH264 from: %s", path)
				m.h264Loaded = true
				return
			}
		}

		loadErr = fmt.Errorf("failed to load OpenH264 DLL from any location")
	})
	return loadErr
}'''

new_load = '''// loadOpenH264 loads the OpenH264 DLL
func (m *Manager) loadOpenH264() error {
	var loadErr error
	m.h264LoadOnce.Do(func() {
		// Get executable path
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		log.Printf("[OpenH264] Executable path: %s", exePath)
		log.Printf("[OpenH264] Executable dir: %s", exeDir)

		// Try multiple possible locations for the OpenH264 DLL
		possiblePaths := []string{
			filepath.Join(exeDir, "openh264-2.4.1-win64.dll"),
			"C:\\\\Program Files\\\\Sentinel Agent\\\\openh264-2.4.1-win64.dll",
			"openh264-2.4.1-win64.dll",
			"./openh264-2.4.1-win64.dll",
			filepath.Join(filepath.Dir(os.Args[0]), "openh264-2.4.1-win64.dll"),
		}

		for _, path := range possiblePaths {
			log.Printf("[OpenH264] Trying path: %s", path)
			if err := openh264.Open(path); err == nil {
				log.Printf("[OpenH264] SUCCESS: Loaded from %s", path)
				m.h264Loaded = true
				return
			} else {
				log.Printf("[OpenH264] Failed: %v", err)
			}
		}

		loadErr = fmt.Errorf("failed to load OpenH264 DLL from any location")
	})
	return loadErr
}'''

if old_load in content:
    content = content.replace(old_load, new_load)
    with open('agent/internal/webrtc/webrtc.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Updated OpenH264 loading with better paths and logging')
else:
    print('Code not found - trying alternative')
    # Check what's actually there
    if 'loadOpenH264' in content:
        print('Found loadOpenH264 function')
    if 'possiblePaths' in content:
        print('Found possiblePaths')
