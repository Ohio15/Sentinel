package remote

import (
	"runtime"
	"sync"

	"github.com/kbinani/screenshot"
)

// ScreenInfo contains display information
type ScreenInfo struct {
	Index       int     `json:"index"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	X           int     `json:"x"`           // Virtual desktop position
	Y           int     `json:"y"`
	RefreshRate int     `json:"refreshRate"` // 60, 144, etc.
	DPIScale    float64 `json:"dpiScale"`    // 1.0, 1.25, 1.5, 2.0
	IsPrimary   bool    `json:"isPrimary"`
	ColorDepth  int     `json:"colorDepth"`
	HDREnabled  bool    `json:"hdrEnabled"`
}

// EncoderInfo describes available encoder capabilities
type EncoderInfo struct {
	Type             string `json:"type"` // "nvenc", "quicksync", "amf", "openh264", "software"
	MaxWidth         int    `json:"maxWidth"`
	MaxHeight        int    `json:"maxHeight"`
	MaxFPS           int    `json:"maxFps"`
	SupportsHardware bool   `json:"supportsHardware"`
}

// InputCapabilities describes supported input methods
type InputCapabilities struct {
	AbsoluteMouse bool `json:"absoluteMouse"`
	RelativeMouse bool `json:"relativeMouse"` // For pointer lock
	MultiTouch    bool `json:"multiTouch"`
	Pen           bool `json:"pen"`
	Clipboard     bool `json:"clipboard"`
}

// HostCapabilities contains all host machine capabilities
type HostCapabilities struct {
	// Display configuration
	Screens []ScreenInfo `json:"screens"`

	// Encoding capabilities
	Encoders []EncoderInfo `json:"encoders"`

	// Input support
	InputCapabilities InputCapabilities `json:"inputCapabilities"`

	// System info
	Platform  string `json:"platform"`  // "windows", "linux", "darwin"
	OSVersion string `json:"osVersion"`
	CPUCores  int    `json:"cpuCores"`
	GPUName   string `json:"gpuName"`

	// Feature flags
	DXGICapture     bool `json:"dxgiCapture"`
	HardwareEncode  bool `json:"hardwareEncode"`
	CursorCapture   bool `json:"cursorCapture"`
}

// ClientPreferences contains client-side preferences for the session
type ClientPreferences struct {
	// Display
	ViewportWidth    int    `json:"viewportWidth"`
	ViewportHeight   int    `json:"viewportHeight"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`
	LocalRefreshRate int    `json:"localRefreshRate"`
	PreferredMonitor int    `json:"preferredMonitor"` // -1 for all

	// Quality
	PreferredQuality string `json:"preferredQuality"` // "low", "medium", "high", "auto"
	MaxBandwidth     int    `json:"maxBandwidth"`     // kbps, 0 = unlimited
	PreferLowLatency bool   `json:"preferLowLatency"` // true = minimize latency

	// Input
	PointerLockSupported bool `json:"pointerLockSupported"`
	TouchSupported       bool `json:"touchSupported"`
	ClipboardSupported   bool `json:"clipboardSupported"`

	// Network estimate
	EstimatedRTT   int    `json:"estimatedRtt"`   // ms
	ConnectionType string `json:"connectionType"` // "lan", "wifi", "cellular", "unknown"
}

// CoordinateMapping contains coordinate transformation parameters
type CoordinateMapping struct {
	HostVirtualLeft   int `json:"hostVirtualLeft"`   // May be negative (multi-monitor)
	HostVirtualTop    int `json:"hostVirtualTop"`
	HostVirtualWidth  int `json:"hostVirtualWidth"`
	HostVirtualHeight int `json:"hostVirtualHeight"`
	CaptureOffsetX    int `json:"captureOffsetX"` // Offset into virtual desktop
	CaptureOffsetY    int `json:"captureOffsetY"`
}

// NegotiatedSession contains the agreed-upon session parameters
type NegotiatedSession struct {
	// Resolution
	CaptureWidth  int `json:"captureWidth"`
	CaptureHeight int `json:"captureHeight"`
	EncodeWidth   int `json:"encodeWidth"`  // May differ for bandwidth
	EncodeHeight  int `json:"encodeHeight"`

	// Timing
	TargetFPS    int `json:"targetFps"`
	MaxLatencyMs int `json:"maxLatencyMs"`

	// Encoding
	Encoder         string `json:"encoder"`
	Bitrate         int    `json:"bitrate"`
	AdaptiveBitrate bool   `json:"adaptiveBitrate"`

	// Coordinate mapping
	CoordinateSpace CoordinateMapping `json:"coordinateSpace"`

	// Features enabled
	LocalCursor    bool `json:"localCursor"`
	ClipboardSync  bool `json:"clipboardSync"`
	PointerLock    bool `json:"pointerLock"`
}

// CursorUpdate contains cursor information sent to client
type CursorUpdate struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Visible   bool   `json:"visible"`
	Shape     string `json:"shape"`     // "default", "pointer", "text", "wait", "crosshair", "custom"
	ImageData string `json:"imageData"` // Base64 PNG for custom cursors
	HotspotX  int    `json:"hotspotX"`
	HotspotY  int    `json:"hotspotY"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// ClipboardData contains clipboard content
type ClipboardData struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

var (
	cachedCapabilities *HostCapabilities
	capabilitiesMu     sync.Mutex
)

// GetHostCapabilities returns the host machine capabilities
func GetHostCapabilities() *HostCapabilities {
	capabilitiesMu.Lock()
	defer capabilitiesMu.Unlock()

	if cachedCapabilities != nil {
		return cachedCapabilities
	}

	caps := &HostCapabilities{
		Platform: runtime.GOOS,
		CPUCores: runtime.NumCPU(),
		InputCapabilities: InputCapabilities{
			AbsoluteMouse: true,
			RelativeMouse: true,
			MultiTouch:    false,
			Pen:           false,
			Clipboard:     true,
		},
	}

	// Get screen info
	caps.Screens = getScreenInfo()

	// Get OS version and GPU info (platform-specific)
	caps.OSVersion, caps.GPUName = getSystemInfo()

	// Detect available encoders
	caps.Encoders = detectEncoders()

	// Feature detection
	caps.DXGICapture = detectDXGICapture()
	caps.HardwareEncode = detectHardwareEncode()
	caps.CursorCapture = true

	cachedCapabilities = caps
	return caps
}

func getScreenInfo() []ScreenInfo {
	numDisplays := screenshot.NumActiveDisplays()
	screens := make([]ScreenInfo, numDisplays)

	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		screens[i] = ScreenInfo{
			Index:       i,
			Width:       bounds.Dx(),
			Height:      bounds.Dy(),
			X:           bounds.Min.X,
			Y:           bounds.Min.Y,
			RefreshRate: 60, // Default, will be updated by platform-specific code
			DPIScale:    1.0,
			IsPrimary:   i == 0,
			ColorDepth:  32,
			HDREnabled:  false,
		}
	}

	// Update with platform-specific info
	updateScreenInfoPlatform(screens)

	return screens
}

// NegotiateSession creates session parameters from host capabilities and client preferences
func NegotiateSession(caps *HostCapabilities, prefs *ClientPreferences) *NegotiatedSession {
	session := &NegotiatedSession{
		AdaptiveBitrate: true,
		LocalCursor:     true,
		ClipboardSync:   prefs.ClipboardSupported && caps.InputCapabilities.Clipboard,
		PointerLock:     prefs.PointerLockSupported && caps.InputCapabilities.RelativeMouse,
	}

	// Determine which monitor(s) to capture
	var captureScreen *ScreenInfo
	if prefs.PreferredMonitor >= 0 && prefs.PreferredMonitor < len(caps.Screens) {
		captureScreen = &caps.Screens[prefs.PreferredMonitor]
	} else if len(caps.Screens) > 0 {
		// Find primary or first screen
		for i := range caps.Screens {
			if caps.Screens[i].IsPrimary {
				captureScreen = &caps.Screens[i]
				break
			}
		}
		if captureScreen == nil {
			captureScreen = &caps.Screens[0]
		}
	}

	if captureScreen != nil {
		session.CaptureWidth = captureScreen.Width
		session.CaptureHeight = captureScreen.Height
		session.CoordinateSpace.CaptureOffsetX = captureScreen.X
		session.CoordinateSpace.CaptureOffsetY = captureScreen.Y
	}

	// Calculate virtual screen bounds
	if len(caps.Screens) > 0 {
		minX, minY := caps.Screens[0].X, caps.Screens[0].Y
		maxX, maxY := caps.Screens[0].X+caps.Screens[0].Width, caps.Screens[0].Y+caps.Screens[0].Height

		for _, s := range caps.Screens[1:] {
			if s.X < minX {
				minX = s.X
			}
			if s.Y < minY {
				minY = s.Y
			}
			if s.X+s.Width > maxX {
				maxX = s.X + s.Width
			}
			if s.Y+s.Height > maxY {
				maxY = s.Y + s.Height
			}
		}

		session.CoordinateSpace.HostVirtualLeft = minX
		session.CoordinateSpace.HostVirtualTop = minY
		session.CoordinateSpace.HostVirtualWidth = maxX - minX
		session.CoordinateSpace.HostVirtualHeight = maxY - minY
	}

	// Determine encoding resolution based on quality and client viewport
	session.EncodeWidth = session.CaptureWidth
	session.EncodeHeight = session.CaptureHeight

	// Scale down if client viewport is smaller or quality is reduced
	switch prefs.PreferredQuality {
	case "low":
		session.TargetFPS = 15
		session.Bitrate = 1000000 // 1 Mbps
		if session.EncodeWidth > 1280 {
			scale := 1280.0 / float64(session.EncodeWidth)
			session.EncodeWidth = 1280
			session.EncodeHeight = int(float64(session.EncodeHeight) * scale)
		}
	case "medium":
		session.TargetFPS = 30
		session.Bitrate = 3000000 // 3 Mbps
		if session.EncodeWidth > 1920 {
			scale := 1920.0 / float64(session.EncodeWidth)
			session.EncodeWidth = 1920
			session.EncodeHeight = int(float64(session.EncodeHeight) * scale)
		}
	case "high":
		session.TargetFPS = 60
		session.Bitrate = 8000000 // 8 Mbps
	default: // auto
		// Use client estimated bandwidth
		if prefs.MaxBandwidth > 0 {
			session.Bitrate = prefs.MaxBandwidth * 1000
		} else {
			session.Bitrate = 4000000 // 4 Mbps default
		}

		// Adjust FPS based on latency preference
		if prefs.PreferLowLatency {
			session.TargetFPS = 30
			session.MaxLatencyMs = 50
		} else {
			session.TargetFPS = 60
			session.MaxLatencyMs = 100
		}
	}

	// Ensure dimensions are even (required for H.264)
	session.EncodeWidth = (session.EncodeWidth / 2) * 2
	session.EncodeHeight = (session.EncodeHeight / 2) * 2

	// Select encoder
	session.Encoder = selectEncoder(caps.Encoders, session.EncodeWidth, session.EncodeHeight, session.TargetFPS)

	return session
}

func selectEncoder(encoders []EncoderInfo, width, height, fps int) string {
	// Prefer hardware encoders
	for _, enc := range encoders {
		if enc.SupportsHardware && enc.MaxWidth >= width && enc.MaxHeight >= height && enc.MaxFPS >= fps {
			return enc.Type
		}
	}

	// Fall back to software
	for _, enc := range encoders {
		if enc.MaxWidth >= width && enc.MaxHeight >= height {
			return enc.Type
		}
	}

	return "openh264" // Default fallback
}
