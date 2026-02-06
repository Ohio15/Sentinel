// Package guacamole implements the Apache Guacamole protocol for RDP connections
package guacamole

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultGuacdAddr is the default address for guacd daemon
	DefaultGuacdAddr = "127.0.0.1:4822"

	// ConnectionTimeout is the timeout for connecting to guacd
	ConnectionTimeout = 10 * time.Second

	// ReadBufferSize for guacd connection
	ReadBufferSize = 8192

	// WriteBufferSize for guacd connection
	WriteBufferSize = 8192
)

// RDPConnectionConfig holds configuration for an RDP connection through guacd
type RDPConnectionConfig struct {
	// Connection target
	Hostname string
	Port     int
	Username string
	Password string
	Domain   string

	// Shadow mode options
	ShadowSessionID uint32
	ShadowControl   bool // true for full control, false for view-only

	// Security settings
	IgnoreCert bool
	Security   string // "", "nla", "tls", "rdp", "any"

	// Display settings
	Width       int
	Height      int
	ColorDepth  int // 8, 16, 24, 32
	DPI         int
	ResizeMethod string // "display-update" or "reconnect"

	// Audio/Video
	DisableAudio bool
	EnableDrive  bool
	DrivePath    string

	// Performance settings
	DisableWallpaper      bool
	DisableTheming        bool
	DisableFontSmoothing  bool
	DisableFullWindowDrag bool
	DisableMenuAnimations bool
}

// DefaultRDPConfig returns a default RDP configuration
func DefaultRDPConfig() RDPConnectionConfig {
	return RDPConnectionConfig{
		Port:         3389,
		IgnoreCert:   true,
		Security:     "any",
		Width:        1920,
		Height:       1080,
		ColorDepth:   24,
		DPI:          96,
		ResizeMethod: "display-update",
		DisableAudio: true, // Disable for RMM to save bandwidth
	}
}

// GuacdConnection manages a connection to the guacd daemon
type GuacdConnection struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// NewGuacdConnection creates a new connection to guacd
func NewGuacdConnection(guacdAddr string) (*GuacdConnection, error) {
	if guacdAddr == "" {
		guacdAddr = DefaultGuacdAddr
	}

	conn, err := net.DialTimeout("tcp", guacdAddr, ConnectionTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to guacd at %s: %w", guacdAddr, err)
	}

	return &GuacdConnection{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, ReadBufferSize),
	}, nil
}

// InitiateRDPConnection starts an RDP connection through guacd
func (g *GuacdConnection) InitiateRDPConnection(config RDPConnectionConfig) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Step 1: Send "select" instruction to choose RDP protocol
	if err := g.sendInstruction("select", "rdp"); err != nil {
		return fmt.Errorf("select failed: %w", err)
	}

	// Step 2: Read "args" response which lists required parameters
	args, err := g.readInstruction()
	if err != nil {
		return fmt.Errorf("failed to read args: %w", err)
	}

	if len(args) == 0 || args[0] != "args" {
		return fmt.Errorf("unexpected response: expected 'args', got %v", args)
	}

	// Step 3: Send "size" instruction with display dimensions
	if err := g.sendInstruction("size",
		strconv.Itoa(config.Width),
		strconv.Itoa(config.Height),
		strconv.Itoa(config.DPI),
	); err != nil {
		return fmt.Errorf("size failed: %w", err)
	}

	// Step 4: Send "audio" instruction (mime types for audio)
	if !config.DisableAudio {
		if err := g.sendInstruction("audio", "audio/L8", "audio/L16"); err != nil {
			return fmt.Errorf("audio failed: %w", err)
		}
	}

	// Step 5: Send "video" instruction (empty for RDP)
	if err := g.sendInstruction("video"); err != nil {
		return fmt.Errorf("video failed: %w", err)
	}

	// Step 6: Send "image" instruction (supported image types)
	if err := g.sendInstruction("image", "image/jpeg", "image/png", "image/webp"); err != nil {
		return fmt.Errorf("image failed: %w", err)
	}

	// Step 7: Build connect arguments based on args response
	// The args list tells us what parameters guacd expects in what order
	connectArgs := g.buildConnectArgs(config, args[1:])

	// Step 8: Send "connect" instruction with RDP parameters
	if err := g.sendInstruction("connect", connectArgs...); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	// Step 9: Read response - should be "ready" with connection ID
	ready, err := g.readInstruction()
	if err != nil {
		return fmt.Errorf("failed to read ready: %w", err)
	}

	if len(ready) == 0 || ready[0] != "ready" {
		// Check if it's an error
		if len(ready) > 0 && ready[0] == "error" {
			errMsg := "unknown error"
			if len(ready) > 1 {
				errMsg = ready[1]
			}
			return fmt.Errorf("guacd error: %s", errMsg)
		}
		return fmt.Errorf("unexpected response: expected 'ready', got %v", ready)
	}

	return nil
}

// buildConnectArgs builds the connection arguments based on what guacd expects
func (g *GuacdConnection) buildConnectArgs(config RDPConnectionConfig, expectedArgs []string) []string {
	// Create a map of known parameters
	params := map[string]string{
		"hostname":              config.Hostname,
		"port":                  strconv.Itoa(config.Port),
		"username":              config.Username,
		"password":              config.Password,
		"domain":                config.Domain,
		"security":              config.Security,
		"ignore-cert":           boolToString(config.IgnoreCert),
		"disable-audio":         boolToString(config.DisableAudio),
		"enable-drive":          boolToString(config.EnableDrive),
		"drive-path":            config.DrivePath,
		"width":                 strconv.Itoa(config.Width),
		"height":                strconv.Itoa(config.Height),
		"dpi":                   strconv.Itoa(config.DPI),
		"color-depth":           strconv.Itoa(config.ColorDepth),
		"resize-method":         config.ResizeMethod,
		"disable-wallpaper":     boolToString(config.DisableWallpaper),
		"disable-theming":       boolToString(config.DisableTheming),
		"disable-font-smoothing": boolToString(config.DisableFontSmoothing),
		"disable-full-window-drag": boolToString(config.DisableFullWindowDrag),
		"disable-menu-animations": boolToString(config.DisableMenuAnimations),
	}

	// If shadow mode is requested, add shadow parameters
	if config.ShadowSessionID > 0 {
		// FreeRDP shadow connection parameters
		params["preconnection-id"] = strconv.Itoa(int(config.ShadowSessionID))
		if config.ShadowControl {
			params["remote-app"] = fmt.Sprintf("/shadow:%d /control", config.ShadowSessionID)
		} else {
			params["remote-app"] = fmt.Sprintf("/shadow:%d", config.ShadowSessionID)
		}
	}

	// Build args in the order guacd expects
	args := make([]string, len(expectedArgs))
	for i, argName := range expectedArgs {
		if val, ok := params[argName]; ok {
			args[i] = val
		} else {
			args[i] = "" // Empty string for unknown/unused parameters
		}
	}

	return args
}

// Bridge forwards data between a WebSocket client and guacd
func (g *GuacdConnection) Bridge(ws *websocket.Conn) error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return fmt.Errorf("connection closed")
	}
	g.mu.Unlock()

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// WebSocket -> guacd
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				errChan <- fmt.Errorf("websocket read: %w", err)
				return
			}
			g.mu.Lock()
			_, err = g.conn.Write(msg)
			g.mu.Unlock()
			if err != nil {
				errChan <- fmt.Errorf("guacd write: %w", err)
				return
			}
		}
	}()

	// guacd -> WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, ReadBufferSize)
		for {
			g.mu.Lock()
			n, err := g.conn.Read(buf)
			g.mu.Unlock()
			if err != nil {
				errChan <- fmt.Errorf("guacd read: %w", err)
				return
			}
			if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
				errChan <- fmt.Errorf("websocket write: %w", err)
				return
			}
		}
	}()

	// Wait for either direction to fail
	err := <-errChan
	g.Close()
	ws.Close()
	wg.Wait()

	return err
}

// SendInstruction sends an instruction to guacd (thread-safe for sending during bridge)
func (g *GuacdConnection) SendInstruction(opcode string, args ...string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sendInstruction(opcode, args...)
}

// sendInstruction sends a Guacamole protocol instruction (not thread-safe)
func (g *GuacdConnection) sendInstruction(opcode string, args ...string) error {
	instruction := EncodeInstruction(opcode, args...)
	_, err := g.conn.Write([]byte(instruction))
	return err
}

// readInstruction reads a Guacamole protocol instruction
func (g *GuacdConnection) readInstruction() ([]string, error) {
	line, err := g.reader.ReadString(';')
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("connection closed by guacd")
		}
		return nil, err
	}
	return DecodeInstruction(line), nil
}

// Close closes the connection to guacd
func (g *GuacdConnection) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return nil
	}

	g.closed = true
	return g.conn.Close()
}

// EncodeInstruction encodes an instruction in Guacamole protocol format
// Format: "length.value,length.value,...;"
func EncodeInstruction(opcode string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, fmt.Sprintf("%d.%s", len(opcode), opcode))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%d.%s", len(arg), arg))
	}
	return strings.Join(parts, ",") + ";"
}

// DecodeInstruction decodes a Guacamole protocol instruction
func DecodeInstruction(data string) []string {
	data = strings.TrimSuffix(data, ";")
	var result []string

	for len(data) > 0 {
		dotIdx := strings.Index(data, ".")
		if dotIdx == -1 {
			break
		}

		length, err := strconv.Atoi(data[:dotIdx])
		if err != nil {
			break
		}

		start := dotIdx + 1
		end := start + length
		if end > len(data) {
			break
		}

		result = append(result, data[start:end])
		data = data[end:]

		if len(data) > 0 && data[0] == ',' {
			data = data[1:]
		}
	}

	return result
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
