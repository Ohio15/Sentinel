// +build !windows

package ipc

import (
	"errors"
	"time"
)

// ErrNotSupported is returned on non-Windows platforms
var ErrNotSupported = errors.New("named pipes are only supported on Windows")

// PipeServer is a stub for non-Windows platforms
type PipeServer struct{}

// NewPipeServer returns an error on non-Windows platforms
func NewPipeServer(handler func(msg PipeMessage) *PipeMessage) (*PipeServer, error) {
	return nil, ErrNotSupported
}

// Accept is a stub
func (ps *PipeServer) Accept() error {
	return ErrNotSupported
}

// Close is a stub
func (ps *PipeServer) Close() error {
	return nil
}

// PipeClient is a stub for non-Windows platforms
type PipeClient struct{}

// ConnectPipe returns an error on non-Windows platforms
func ConnectPipe() (*PipeClient, error) {
	return nil, ErrNotSupported
}

// ConnectPipeWithTimeout returns an error on non-Windows platforms
func ConnectPipeWithTimeout(timeout time.Duration) (*PipeClient, error) {
	return nil, ErrNotSupported
}

// Send is a stub
func (pc *PipeClient) Send(msg PipeMessage, expectResponse bool) (*PipeMessage, error) {
	return nil, ErrNotSupported
}

// Close is a stub
func (pc *PipeClient) Close() error {
	return nil
}

// SignalUpdateReady is a stub on non-Windows platforms
func SignalUpdateReady(request *UpdateRequest) error {
	return ErrNotSupported
}

// QueryWatchdogVersion is a stub on non-Windows platforms
func QueryWatchdogVersion() (string, error) {
	return "", ErrNotSupported
}

// IsPipeAvailable always returns false on non-Windows platforms
func IsPipeAvailable() bool {
	return false
}
