//go:build !windows

package remote

func getSystemInfo() (osVersion string, gpuName string) {
	return "unknown", "unknown"
}

func updateScreenInfoPlatform(screens []ScreenInfo) {
	// No platform-specific updates on non-Windows
}

func detectEncoders() []EncoderInfo {
	// Only software encoder available on non-Windows for now
	return []EncoderInfo{
		{
			Type:             "openh264",
			MaxWidth:         4096,
			MaxHeight:        2160,
			MaxFPS:           60,
			SupportsHardware: false,
		},
	}
}

func detectDXGICapture() bool {
	return false
}

func detectHardwareEncode() bool {
	return false
}
