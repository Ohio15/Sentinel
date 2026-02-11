//go:build linux

package helper

// NOTE: This file provides stub types for Linux that match the Windows API.
// On Linux, the desktop manager handles WebRTC directly (see manager_linux.go)
// rather than using a separate helper process like on Windows.
// These stubs exist only to satisfy the compiler when code references helper types.

// HelperState represents the state of the desktop helper (stub for Linux)
type HelperState string

const (
	StateReady        HelperState = "ready"
	StateConnecting   HelperState = "connecting"
	StateConnected    HelperState = "connected"
	StateDisconnected HelperState = "disconnected"
	StateError        HelperState = "error"
)
