// +build windows

// sentinel-desktop is the user-mode helper process for WebRTC remote desktop.
// It runs in the user's interactive session and handles screen capture,
// H.264 encoding, and WebRTC peer connections.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sentinel/agent/internal/desktop"
	"github.com/sentinel/agent/internal/desktop/helper"

	"golang.org/x/sys/windows"
)

var (
	version   = "1.0.0"
	buildTime = "development"
)

func main() {
	// Parse command line arguments
	sessionID := flag.Uint("session-id", 0, "Windows session ID")
	token := flag.String("token", "", "Authorization token from service")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sentinel-desktop %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Setup logging to file
	logFile, err := setupLogging()
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	log.Printf("sentinel-desktop starting, version=%s, sessionID=%d", version, *sessionID)

	// Validate required arguments
	if *sessionID == 0 {
		log.Fatal("--session-id is required")
	}
	if *token == "" {
		log.Fatal("--token is required")
	}

	// Try to acquire session-scoped mutex
	mutexName := fmt.Sprintf("%s%d", desktop.MutexNamePrefix, *sessionID)
	mutex, err := acquireMutex(mutexName)
	if err != nil {
		log.Printf("Failed to acquire mutex %s: %v", mutexName, err)
		os.Exit(desktop.ExitCodeMutexConflict)
	}
	defer releaseMutex(mutex)

	log.Printf("Acquired mutex: %s", mutexName)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, shutting down", sig)
		cancel()
	}()

	// Create IPC client
	client := desktop.NewIPCClient(uint32(*sessionID), *token)

	// Create helper instance
	h := NewHelper(ctx, client, uint32(*sessionID))

	// Set IPC handlers
	client.SetHandlers(
		h.OnStartSession,
		h.OnStopSession,
		h.OnICECandidate,
		h.OnShutdown,
	)

	// Connect to service
	if err := client.Connect(ctx); err != nil {
		log.Printf("Failed to connect to service: %v", err)
		os.Exit(desktop.ExitCodeIPCDisconnected)
	}
	defer client.Close()

	// Authenticate
	if err := client.Authenticate(); err != nil {
		log.Printf("Authentication failed: %v", err)
		os.Exit(desktop.ExitCodeAuthFailed)
	}

	// Send initial status
	client.SendStatus(desktop.StateReady, "Helper ready", "")

	log.Printf("Helper initialized and ready")

	// Run the main loop
	exitCode := h.Run(ctx, client)

	log.Printf("Helper exiting with code %d", exitCode)
	os.Exit(exitCode)
}

// Helper manages the WebRTC session and screen capture
type Helper struct {
	client        *desktop.IPCClient
	sessionID     uint32
	webrtcHandler *helper.WebRTCHandler
	ctx           context.Context

	shutdownChan chan struct{}
}

// NewHelper creates a new helper instance
func NewHelper(ctx context.Context, client *desktop.IPCClient, sessionID uint32) *Helper {
	return &Helper{
		client:        client,
		sessionID:     sessionID,
		webrtcHandler: helper.NewWebRTCHandler(client),
		ctx:           ctx,
		shutdownChan:  make(chan struct{}),
	}
}

// Run starts the main helper loop
func (h *Helper) Run(ctx context.Context, client *desktop.IPCClient) int {
	// Ensure cleanup on exit
	defer func() {
		if h.webrtcHandler != nil {
			h.webrtcHandler.Close()
		}
	}()

	// Start the IPC message loop
	err := client.Start(ctx)

	select {
	case <-h.shutdownChan:
		return desktop.ExitCodeShutdownRequested
	default:
	}

	if err != nil {
		if ctx.Err() != nil {
			return desktop.ExitCodeSuccess
		}
		log.Printf("IPC loop error: %v", err)
		return desktop.ExitCodeIPCDisconnected
	}

	return desktop.ExitCodeSuccess
}

// OnStartSession handles the start_session message from service
func (h *Helper) OnStartSession(payload *desktop.StartSessionPayload) error {
	log.Printf("[Helper] StartSession received, connectionID=%s, sdpType=%s, sdp length=%d",
		payload.ConnectionID, payload.SDPType, len(payload.SDP))

	return h.webrtcHandler.HandleStartSession(h.ctx, payload)
}

// OnStopSession handles the stop_session message from service
func (h *Helper) OnStopSession(payload *desktop.StopSessionPayload) error {
	log.Printf("[Helper] StopSession received, connectionID=%s, reason=%s",
		payload.ConnectionID, payload.Reason)

	return h.webrtcHandler.HandleStopSession(payload)
}

// OnICECandidate handles ICE candidates from the service
func (h *Helper) OnICECandidate(payload *desktop.ICECandidatePayload) error {
	log.Printf("[Helper] ICE candidate received, connectionID=%s", payload.ConnectionID)

	return h.webrtcHandler.HandleICECandidate(payload)
}

// OnShutdown handles shutdown request from service
func (h *Helper) OnShutdown(payload *desktop.ShutdownPayload) {
	log.Printf("[Helper] Shutdown requested, reason=%s, timeout=%v", payload.Reason, payload.Timeout)

	// Close WebRTC handler
	if h.webrtcHandler != nil {
		h.webrtcHandler.Close()
	}

	close(h.shutdownChan)
}

// setupLogging configures logging to file
func setupLogging() (*os.File, error) {
	logDir := `C:\ProgramData\Sentinel`
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	logPath := logDir + `\sentinel-desktop.log`

	// Open log file in append mode
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	return f, nil
}

// acquireMutex tries to acquire a named mutex
func acquireMutex(name string) (windows.Handle, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}

	// Try to create/open the mutex
	handle, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil {
		return 0, fmt.Errorf("CreateMutex failed: %w", err)
	}

	// Try to acquire ownership (wait 0ms = non-blocking)
	event, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		windows.CloseHandle(handle)
		return 0, fmt.Errorf("WaitForSingleObject failed: %w", err)
	}

	switch event {
	case windows.WAIT_OBJECT_0:
		// Successfully acquired
		return handle, nil
	case windows.WAIT_ABANDONED:
		// Previous owner died, we now own it
		return handle, nil
	case uint32(windows.WAIT_TIMEOUT):
		// Another instance owns the mutex
		windows.CloseHandle(handle)
		return 0, fmt.Errorf("mutex already owned by another instance")
	default:
		windows.CloseHandle(handle)
		return 0, fmt.Errorf("unexpected wait result: %d", event)
	}
}

// releaseMutex releases the mutex
func releaseMutex(handle windows.Handle) {
	if handle != 0 {
		windows.ReleaseMutex(handle)
		windows.CloseHandle(handle)
	}
}
