package main

import "os"

// removeZoneIdentifier removes the Zone.Identifier alternate data stream from a file.
// This prevents the "Downloaded from the Internet" security warning on Windows.
func removeZoneIdentifier(filePath string) error {
	adsPath := filePath + ":Zone.Identifier"
	err := os.Remove(adsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeSelfZoneIdentifier removes the Zone.Identifier from the running executable
func removeSelfZoneIdentifier() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	removeZoneIdentifier(exe)
}
