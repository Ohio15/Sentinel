//go:build linux

package helper

// GetVirtualScreenBounds returns the bounds of the virtual screen (all monitors)
// On Linux, this returns a default 1920x1080 screen - actual implementation would use Xrandr
func GetVirtualScreenBounds() (left, top, width, height int) {
	// Default to common resolution - actual implementation would query X11/Xrandr
	return 0, 0, 1920, 1080
}

// GetPrimaryScreenDimensions returns the primary screen dimensions
// On Linux, this returns a default 1920x1080 - actual implementation would use Xrandr
func GetPrimaryScreenDimensions() (width, height int) {
	// Default to common resolution - actual implementation would query X11/Xrandr
	return 1920, 1080
}
