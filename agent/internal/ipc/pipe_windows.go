// +build windows

package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// PipeServer listens for connections on the named pipe
type PipeServer struct {
	listener net.Listener
	handler  func(msg PipeMessage) *PipeMessage
}

// SecurityDescriptor for the named pipe - allows SYSTEM and Administrators only
// D: = DACL
// (A;;GA;;;SY) = Allow Generic All to SYSTEM
// (A;;GA;;;BA) = Allow Generic All to Builtin Administrators
const pipeSecurityDescriptor = "D:(A;;GA;;;SY)(A;;GA;;;BA)"

// NewPipeServer creates a new named pipe server
func NewPipeServer(handler func(msg PipeMessage) *PipeMessage) (*PipeServer, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: pipeSecurityDescriptor,
		MessageMode:        true,
		InputBufferSize:    4096,
		OutputBufferSize:   4096,
	}

	listener, err := winio.ListenPipe(PipeName, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe listener: %w", err)
	}

	return &PipeServer{
		listener: listener,
		handler:  handler,
	}, nil
}

// Accept waits for and handles a single connection
func (ps *PipeServer) Accept() error {
	conn, err := ps.listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept connection: %w", err)
	}
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// Read message
	var msg PipeMessage
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to decode message: %w", err)
	}

	// Handle message and optionally send response
	if ps.handler != nil {
		resp := ps.handler(msg)
		if resp != nil {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			encoder := json.NewEncoder(conn)
			if err := encoder.Encode(resp); err != nil {
				return fmt.Errorf("failed to encode response: %w", err)
			}
		}
	}

	return nil
}

// Close closes the pipe listener
func (ps *PipeServer) Close() error {
	if ps.listener != nil {
		return ps.listener.Close()
	}
	return nil
}

// PipeClient connects to the named pipe
type PipeClient struct {
	conn net.Conn
}

// ConnectPipe attempts to connect to the watchdog's named pipe
func ConnectPipe() (*PipeClient, error) {
	conn, err := winio.DialPipe(PipeName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pipe: %w", err)
	}

	return &PipeClient{conn: conn}, nil
}

// ConnectPipeWithTimeout attempts to connect with a timeout
func ConnectPipeWithTimeout(timeout time.Duration) (*PipeClient, error) {
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pipe: %w", err)
	}

	return &PipeClient{conn: conn}, nil
}

// Send sends a message and optionally waits for a response
func (pc *PipeClient) Send(msg PipeMessage, expectResponse bool) (*PipeMessage, error) {
	// Set write deadline
	pc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	encoder := json.NewEncoder(pc.conn)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	if !expectResponse {
		return nil, nil
	}

	// Set read deadline for response
	pc.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	var resp PipeMessage
	decoder := json.NewDecoder(pc.conn)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Close closes the pipe connection
func (pc *PipeClient) Close() error {
	if pc.conn != nil {
		return pc.conn.Close()
	}
	return nil
}

// SignalUpdateReady sends an update ready signal to the watchdog via named pipe.
// Returns nil if successful, or an error if the pipe is not available (watchdog may be old version).
func SignalUpdateReady(request *UpdateRequest) error {
	client, err := ConnectPipeWithTimeout(5 * time.Second)
	if err != nil {
		return fmt.Errorf("watchdog pipe not available: %w", err)
	}
	defer client.Close()

	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	msg := PipeMessage{
		Type:    MsgUpdateReady,
		Payload: string(payload),
	}

	_, err = client.Send(msg, false)
	return err
}

// QueryWatchdogVersion queries the watchdog for its version via named pipe.
// Returns empty string if pipe is not available.
func QueryWatchdogVersion() (string, error) {
	client, err := ConnectPipeWithTimeout(5 * time.Second)
	if err != nil {
		return "", fmt.Errorf("watchdog pipe not available: %w", err)
	}
	defer client.Close()

	msg := PipeMessage{
		Type: MsgVersionQuery,
	}

	resp, err := client.Send(msg, true)
	if err != nil {
		return "", err
	}

	if resp.Type != MsgVersionResp {
		return "", fmt.Errorf("unexpected response type: %s", resp.Type)
	}

	return resp.Payload, nil
}

// IsPipeAvailable checks if the watchdog's named pipe is available
func IsPipeAvailable() bool {
	client, err := ConnectPipeWithTimeout(1 * time.Second)
	if err != nil {
		return false
	}
	client.Close()
	return true
}
