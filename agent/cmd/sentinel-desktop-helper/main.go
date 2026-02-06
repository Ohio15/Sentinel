//go:build windows
// +build windows

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/sentinel/agent/internal/desktop"
	"github.com/sentinel/agent/internal/desktop/helper"
)

func main() {
	pipeName := flag.String("pipe", "", "Named pipe path for IPC")
	sessionIDStr := flag.String("session", "", "Windows session ID")
	flag.Parse()

	// Set up logging to file
	logFile, err := os.OpenFile("C:\\ProgramData\\Sentinel\\sentinel-desktop.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if *pipeName == "" || *sessionIDStr == "" {
		log.Fatal("Usage: sentinel-desktop-helper -pipe <pipepath> -session <sessionid>")
	}

	sessionID, err := strconv.ParseUint(*sessionIDStr, 10, 32)
	if err != nil {
		log.Fatalf("Invalid session ID: %v", err)
	}

	log.Printf("Desktop helper starting for session %d", sessionID)
	log.Printf("Connecting to pipe: %s", *pipeName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutdown signal received")
		cancel()
	}()

	// Create IPC client - use empty token since new protocol doesn't require it
	client := desktop.NewIPCClient(uint32(sessionID), "")

	// Create WebRTC handler
	webrtcHandler := helper.NewWebRTCHandler(client)
	defer webrtcHandler.Close()

	// Set up message handlers
	client.SetHandlers(
		// OnStartSession - create WebRTC session with the offer SDP
		func(payload *desktop.StartSessionPayload) error {
			log.Printf("[Helper] Received start_session: connectionID=%s", payload.ConnectionID)
			return webrtcHandler.HandleStartSession(ctx, payload)
		},
		// OnStopSession - stop the WebRTC session
		func(payload *desktop.StopSessionPayload) error {
			log.Printf("[Helper] Received stop_session: connectionID=%s", payload.ConnectionID)
			return webrtcHandler.HandleStopSession(payload)
		},
		// OnICECandidate - add ICE candidate to the session
		func(payload *desktop.ICECandidatePayload) error {
			log.Printf("[Helper] Received ICE candidate")
			return webrtcHandler.HandleICECandidate(payload)
		},
		// OnShutdown - graceful shutdown
		func(payload *desktop.ShutdownPayload) {
			log.Printf("[Helper] Shutdown requested: %s", payload.Reason)
			cancel()
		},
	)

	// Connect to the service
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect to service: %v", err)
	}
	defer client.Close()

	log.Println("Connected to service, waiting for commands...")

	// Start the message loop (blocks until shutdown)
	if err := client.Start(ctx); err != nil && ctx.Err() == nil {
		log.Printf("Client error: %v", err)
	}

	log.Println("Desktop helper shutting down")
}
