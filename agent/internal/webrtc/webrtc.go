//go:build windows

package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sentinel/agent/internal/capture"

	"github.com/kbinani/screenshot"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/y9o/go-openh264"
)

// Session represents an active WebRTC session
type Session struct {
	ID             string
	PeerConnection *webrtc.PeerConnection
	VideoTrack     *webrtc.TrackLocalStaticSample
	DataChannel    *webrtc.DataChannel
	Quality        string
	Active         bool
	Connected      bool
	OnSignal       func(signal SignalMessage)
	OnInput        func(input InputEvent)
	ctx            context.Context
	cancel         context.CancelFunc
	encoder        *h264Encoder       // Legacy OpenH264 encoder (fallback)
	videoEncoder   VideoEncoder       // New encoder interface (MF or OpenH264)
	dxgiCapture    *capture.DXGICapture // DXGI capture for high-performance screen capture
	useDXGI        bool                 // Whether to use DXGI capture (vs screenshot fallback)
	captureStrategy *CaptureStrategy   // Smart frame decision strategy
	encoderBitrate  int                // Current bitrate for dynamic adjustment
	mu             sync.Mutex
}

// h264Encoder wraps the OpenH264 encoder
type h264Encoder struct {
	encoder    *openh264.ISVCEncoder
	width      int32
	height     int32
	frameIndex int64
	pinner     *runtime.Pinner
	mu         sync.Mutex
}

// Windows API for cursor
var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")
)

type point struct {
	X, Y int32
}

// getCursorPos returns the current cursor position
func getCursorPos() (int, int) {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

// drawCursor draws a cursor shape on the image at the given position
func drawCursor(img *image.RGBA, x, y int, bounds image.Rectangle) {
	// Adjust for screen bounds offset
	x = x - bounds.Min.X
	y = y - bounds.Min.Y

	// Check if cursor is within the captured area
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}

	// Scale factor for cursor size (2 = double size)
	scale := 2

	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}

	// Draw a larger arrow cursor
	// The cursor hotspot is at (0,0) - top-left corner of the arrow
	cursorHeight := 21 * scale
	_ = 12 * scale // cursorWidth - not used directly but documents intent

	// Draw the cursor shape (arrow pointing down-right from hotspot)
	for row := 0; row < cursorHeight; row++ {
		// Calculate how wide this row should be
		// Arrow tapers: starts at 1px, grows to ~7px at row 14, then narrows for the tail
		var rowWidth int
		if row < 14*scale {
			rowWidth = (row / scale) / 2 + 1
		} else {
			// Tail part - constant width
			rowWidth = 2
		}

		for col := 0; col < rowWidth*scale; col++ {
			px, py := x+col, y+row
			if px >= 0 && py >= 0 && px < img.Bounds().Dx() && py < img.Bounds().Dy() {
				// Black outline on edges, white fill inside
				isEdge := col == 0 || col >= (rowWidth*scale)-scale || row == 0 || row >= cursorHeight-scale
				if row < 14*scale {
					isEdge = isEdge || col >= (rowWidth-1)*scale
				}
				if isEdge {
					img.Set(px, py, black)
				} else {
					img.Set(px, py, white)
				}
			}
		}
	}

	// Add black outline on the right edge of the arrow
	for row := 0; row < 14*scale; row++ {
		rowWidth := (row/scale)/2 + 1
		for s := 0; s < scale; s++ {
			px, py := x+rowWidth*scale+s, y+row
			if px >= 0 && py >= 0 && px < img.Bounds().Dx() && py < img.Bounds().Dy() {
				img.Set(px, py, black)
			}
		}
	}
}

// Manager manages WebRTC sessions
type Manager struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	h264Loaded   bool
	h264LoadErr  error
	h264LoadOnce sync.Once
}

// NewManager creates a new WebRTC manager
// Pre-loads OpenH264 at startup and tests encoder creation to fail gracefully
func NewManager() *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
	}
	// Pre-load OpenH264 at startup to catch loading issues early
	log.Printf("[WebRTC] NewManager: Pre-loading OpenH264...")
	if err := m.loadOpenH264(); err != nil {
		log.Printf("[WebRTC] NewManager: OpenH264 pre-load failed: %v", err)
		m.h264LoadErr = err
		return m
	}
	log.Printf("[WebRTC] NewManager: OpenH264 pre-loaded successfully")
	
	// Test encoder creation twice to verify re-creation works
	log.Printf("[WebRTC] NewManager: Testing encoder creation (1st)...")
	testEncoder, err := newH264EncoderWithTimeout(1920, 1080, 1500000, 15*time.Second)
	if err != nil {
		log.Printf("[WebRTC] NewManager: Test encoder 1st creation failed: %v", err)
		m.h264LoadErr = fmt.Errorf("encoder test failed: %w", err)
		return m
	}
	log.Printf("[WebRTC] NewManager: Test encoder 1st created, closing...")
	testEncoder.close()
	
	// Test second encoder creation after closing first
	log.Printf("[WebRTC] NewManager: Testing encoder creation (2nd)...")
	testEncoder2, err := newH264EncoderWithTimeout(1920, 1080, 1500000, 15*time.Second)
	if err != nil {
		log.Printf("[WebRTC] NewManager: Test encoder 2nd creation failed: %v", err)
		m.h264LoadErr = fmt.Errorf("encoder re-creation test failed: %w", err)
		return m
	}
	testEncoder2.close()
	log.Printf("[WebRTC] NewManager: Both test encoders created and closed successfully")

	// Test peer connection creation to catch issues early
	log.Printf("[WebRTC] NewManager: Testing peer connection creation...")
	if err := m.testPeerConnection(); err != nil {
		log.Printf("[WebRTC] NewManager: Peer connection test failed: %v", err)
		m.h264LoadErr = fmt.Errorf("peer connection test failed: %w", err)
		return m
	}
	log.Printf("[WebRTC] NewManager: Peer connection test passed")
	return m
}

// testPeerConnection tests if we can create a basic peer connection and call SetRemoteDescription
func (m *Manager) testPeerConnection() error {
	// Create minimal media engine
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return fmt.Errorf("failed to register H264 codec: %w", err)
	}

	// Create interceptor registry
	interceptorRegistry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		return fmt.Errorf("failed to register interceptors: %w", err)
	}

	// Create SettingEngine to avoid hangs in Windows services
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settingEngine.SetICETimeouts(5*time.Second, 25*time.Second, 2*time.Second)

	// Create API with ICE settings
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
		webrtc.WithSettingEngine(settingEngine),
	)

	// Create peer connection with no ICE servers (local only)
	log.Printf("[WebRTC] testPeerConnection: Creating peer connection...")
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Test SetRemoteDescription with a minimal SDP offer
	log.Printf("[WebRTC] testPeerConnection: Testing SetRemoteDescription with minimal SDP...")
	minimalSDP := "v=0\r\n" +
		"o=- 0 0 IN IP4 127.0.0.1\r\n" +
		"s=-\r\n" +
		"t=0 0\r\n" +
		"a=group:BUNDLE 0\r\n" +
		"a=msid-semantic:WMS\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=rtcp:9 IN IP4 0.0.0.0\r\n" +
		"a=ice-ufrag:test\r\n" +
		"a=ice-pwd:testpasswordtestpassword\r\n" +
		"a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00\r\n" +
		"a=setup:actpass\r\n" +
		"a=mid:0\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:96 H264/90000\r\n" +
		"a=fmtp:96 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f\r\n"

	done := make(chan error, 1)
	go func() {
		done <- pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  minimalSDP,
		})
	}()

	select {
	case err = <-done:
		if err != nil {
			log.Printf("[WebRTC] testPeerConnection: SetRemoteDescription returned error (expected for minimal SDP): %v", err)
			// Error is OK - we just want to make sure it doesn't hang/crash
		} else {
			log.Printf("[WebRTC] testPeerConnection: SetRemoteDescription succeeded")
		}
	case <-time.After(10 * time.Second):
		pc.Close()
		return fmt.Errorf("SetRemoteDescription test timed out after 10 seconds")
	}

	log.Printf("[WebRTC] testPeerConnection: Peer connection created, closing...")
	pc.Close()
	log.Printf("[WebRTC] testPeerConnection: Peer connection closed")
	return nil
}

// loadOpenH264 loads the OpenH264 DLL
func (m *Manager) loadOpenH264() error {
	var loadErr error
	m.h264LoadOnce.Do(func() {
		// Get executable path
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		log.Printf("[OpenH264] Executable path: %s", exePath)
		log.Printf("[OpenH264] Executable dir: %s", exeDir)

		// Try multiple possible locations for the OpenH264 DLL
		possiblePaths := []string{
			filepath.Join(exeDir, "openh264-2.4.1-win64.dll"),
			"C:\\ProgramData\\Sentinel\\openh264-2.4.1-win64.dll",
			"C:\\Program Files\\Sentinel Agent\\openh264-2.4.1-win64.dll",
			"openh264-2.4.1-win64.dll",
			"./openh264-2.4.1-win64.dll",
			filepath.Join(filepath.Dir(os.Args[0]), "openh264-2.4.1-win64.dll"),
		}

		for _, path := range possiblePaths {
			log.Printf("[OpenH264] Trying path: %s", path)
			if err := openh264.Open(path); err == nil {
				log.Printf("[OpenH264] SUCCESS: Loaded from %s", path)
				m.h264Loaded = true
				return
			} else {
				log.Printf("[OpenH264] Failed: %v", err)
			}
		}

		loadErr = fmt.Errorf("failed to load OpenH264 DLL from any location")
	})
	return loadErr
}

// getVideoConstraints returns video constraints based on quality setting
// Returns: width, height, fps, bitrate
// Note: width/height are now max limits - actual resolution uses screen dimensions for accurate cursor mapping
func getVideoConstraints(quality string) (int, int, int, int) {
	switch quality {
	case "low":
		return 1920, 1080, 15, 1_500_000 // 15fps, 1.5Mbps (was 10fps, 800kbps)
	case "high":
		return 3840, 2160, 60, 15_000_000 // 60fps, 15Mbps (was 30fps, 4Mbps)
	case "ultra":
		return 3840, 2160, 60, 30_000_000 // 60fps, 30Mbps (new tier for LAN)
	default: // medium
		return 2560, 1440, 30, 6_000_000 // 30fps, 6Mbps (was 20fps, 2Mbps)
	}
}

// getActualScreenDimensions returns the primary display dimensions safely
func getActualScreenDimensions() (int, int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[WebRTC] Recovered from panic in getActualScreenDimensions: %v", r)
		}
	}()
	bounds := screenshot.GetDisplayBounds(0)
	if bounds.Dx() > 0 && bounds.Dy() > 0 {
		return bounds.Dx(), bounds.Dy()
	}
	return 1920, 1080 // Safe fallback
}

// newH264EncoderWithTimeout creates a new H.264 encoder with a timeout to prevent hanging
func newH264EncoderWithTimeout(width, height, bitrate int, timeout time.Duration) (*h264Encoder, error) {
	log.Printf("[H264Encoder] newH264EncoderWithTimeout called: %dx%d @ %d bps, timeout=%v", width, height, bitrate, timeout)

	type result struct {
		encoder *h264Encoder
		err     error
	}

	done := make(chan result, 1)
	go func() {
		log.Printf("[H264Encoder] Goroutine: starting newH264EncoderInternal...")
		enc, err := newH264EncoderInternal(width, height, bitrate)
		log.Printf("[H264Encoder] Goroutine: newH264EncoderInternal returned, err=%v, sending to channel...", err)
		done <- result{enc, err}
		log.Printf("[H264Encoder] Goroutine: sent to channel")
	}()

	log.Printf("[H264Encoder] Waiting on select...")
	select {
	case res := <-done:
		log.Printf("[H264Encoder] Received from channel, returning encoder (err=%v)...", res.err)
		return res.encoder, res.err
	case <-time.After(timeout):
		log.Printf("[H264Encoder] TIMEOUT waiting for encoder")
		return nil, fmt.Errorf("encoder creation timed out after %v", timeout)
	}
}

// alignTo16 rounds up to nearest multiple of 16 (macroblock size for H.264)
func alignTo16(val int) int {
	if val%16 == 0 {
		return val
	}
	return ((val / 16) + 1) * 16
}

// newH264EncoderInternal creates a new H.264 encoder (internal implementation)
func newH264EncoderInternal(width, height, bitrate int) (*h264Encoder, error) {
	// Align dimensions to 16-pixel boundaries (H.264 macroblock requirement)
	alignedWidth := alignTo16(width)
	alignedHeight := alignTo16(height)

	log.Printf("[H264Encoder] Creating encoder: %dx%d (aligned: %dx%d) @ %d bps",
		width, height, alignedWidth, alignedHeight, bitrate)

	var ppEnc *openh264.ISVCEncoder
	log.Printf("[H264Encoder] Calling WelsCreateSVCEncoder...")
	if ret := openh264.WelsCreateSVCEncoder(&ppEnc); ret != 0 || ppEnc == nil {
		return nil, fmt.Errorf("failed to create H264 encoder: %d", ret)
	}
	log.Printf("[H264Encoder] WelsCreateSVCEncoder returned successfully")

	// Use CAMERA_VIDEO_REAL_TIME for better compatibility
	// SCREEN_CONTENT_REAL_TIME can cause crashes on some systems
	encParam := openh264.SEncParamBase{
		IUsageType:     openh264.CAMERA_VIDEO_REAL_TIME,
		IPicWidth:      int32(alignedWidth),
		IPicHeight:     int32(alignedHeight),
		ITargetBitrate: int32(bitrate),
		FMaxFrameRate:  30.0,
	}
	log.Printf("[H264Encoder] Encoder params: UsageType=%d, Width=%d, Height=%d, Bitrate=%d, FPS=%.1f",
		encParam.IUsageType, encParam.IPicWidth, encParam.IPicHeight, encParam.ITargetBitrate, encParam.FMaxFrameRate)
	log.Printf("[H264Encoder] Calling Initialize...")

	if ret := ppEnc.Initialize(&encParam); ret != 0 {
		log.Printf("[H264Encoder] Initialize failed with code: %d", ret)
		openh264.WelsDestroySVCEncoder(ppEnc)
		return nil, fmt.Errorf("failed to initialize H264 encoder: %d", ret)
	}
	log.Printf("[H264Encoder] Initialize returned successfully")

	return &h264Encoder{
		encoder:    ppEnc,
		width:      int32(alignedWidth),
		height:     int32(alignedHeight),
		frameIndex: 0,
		pinner:     &runtime.Pinner{},
	}, nil
}

// newH264Encoder creates a new H.264 encoder with default 10 second timeout
func newH264Encoder(width, height, bitrate int) (*h264Encoder, error) {
	return newH264EncoderWithTimeout(width, height, bitrate, 10*time.Second)
}

// rgbaToYCbCr converts RGBA image to YCbCr 4:2:0 format
func rgbaToYCbCr(rgba *image.RGBA) *image.YCbCr {
	bounds := rgba.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	ycbcr := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := (y-bounds.Min.Y)*rgba.Stride + (x-bounds.Min.X)*4
			r := float64(rgba.Pix[offset])
			g := float64(rgba.Pix[offset+1])
			b := float64(rgba.Pix[offset+2])

			// ITU-R BT.601 conversion
			yVal := 16 + (65.481*r+128.553*g+24.966*b)/255.0
			cbVal := 128 + (-37.797*r-74.203*g+112.0*b)/255.0
			crVal := 128 + (112.0*r-93.786*g-18.214*b)/255.0

			// Clamp values
			if yVal < 0 {
				yVal = 0
			} else if yVal > 255 {
				yVal = 255
			}
			if cbVal < 0 {
				cbVal = 0
			} else if cbVal > 255 {
				cbVal = 255
			}
			if crVal < 0 {
				crVal = 0
			} else if crVal > 255 {
				crVal = 255
			}

			yIndex := (y-bounds.Min.Y)*ycbcr.YStride + (x - bounds.Min.X)
			ycbcr.Y[yIndex] = uint8(yVal)

			// Subsample Cb and Cr (4:2:0)
			if x%2 == 0 && y%2 == 0 {
				cIndex := ((y-bounds.Min.Y)/2)*ycbcr.CStride + (x-bounds.Min.X)/2
				ycbcr.Cb[cIndex] = uint8(cbVal)
				ycbcr.Cr[cIndex] = uint8(crVal)
			}
		}
	}

	return ycbcr
}

// rgbaToYCbCrPadded converts RGBA image to YCbCr 4:2:0 format with padding to target dimensions
// targetWidth and targetHeight must be >= the source image dimensions
func rgbaToYCbCrPadded(rgba *image.RGBA, targetWidth, targetHeight int) *image.YCbCr {
	bounds := rgba.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	// Create YCbCr with target (padded) dimensions
	targetBounds := image.Rect(0, 0, targetWidth, targetHeight)
	ycbcr := image.NewYCbCr(targetBounds, image.YCbCrSubsampleRatio420)

	// Initialize all Y values to black (16 in YCbCr) and Cb/Cr to neutral (128)
	for i := range ycbcr.Y {
		ycbcr.Y[i] = 16 // Black in Y
	}
	for i := range ycbcr.Cb {
		ycbcr.Cb[i] = 128 // Neutral Cb
		ycbcr.Cr[i] = 128 // Neutral Cr
	}

	// Convert source pixels
	for y := 0; y < srcHeight; y++ {
		for x := 0; x < srcWidth; x++ {
			offset := (y-bounds.Min.Y)*rgba.Stride + (x-bounds.Min.X)*4
			r := float64(rgba.Pix[offset])
			g := float64(rgba.Pix[offset+1])
			b := float64(rgba.Pix[offset+2])

			// ITU-R BT.601 conversion
			yVal := 16 + (65.481*r+128.553*g+24.966*b)/255.0
			cbVal := 128 + (-37.797*r-74.203*g+112.0*b)/255.0
			crVal := 128 + (112.0*r-93.786*g-18.214*b)/255.0

			// Clamp values
			if yVal < 0 {
				yVal = 0
			} else if yVal > 255 {
				yVal = 255
			}
			if cbVal < 0 {
				cbVal = 0
			} else if cbVal > 255 {
				cbVal = 255
			}
			if crVal < 0 {
				crVal = 0
			} else if crVal > 255 {
				crVal = 255
			}

			yIndex := y*ycbcr.YStride + x
			ycbcr.Y[yIndex] = uint8(yVal)

			// Subsample Cb and Cr (4:2:0)
			if x%2 == 0 && y%2 == 0 {
				cIndex := (y/2)*ycbcr.CStride + x/2
				ycbcr.Cb[cIndex] = uint8(cbVal)
				ycbcr.Cr[cIndex] = uint8(crVal)
			}
		}
	}

	return ycbcr
}

// encode encodes a YCbCr frame to H.264
func (e *h264Encoder) encode(ycbcr *image.YCbCr) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pinner.Pin(&ycbcr.Y[0])
	e.pinner.Pin(&ycbcr.Cb[0])
	e.pinner.Pin(&ycbcr.Cr[0])
	defer e.pinner.Unpin()

	encSrcPic := openh264.SSourcePicture{
		IColorFormat: openh264.VideoFormatI420,
		IStride:      [4]int32{int32(ycbcr.YStride), int32(ycbcr.CStride), int32(ycbcr.CStride), 0},
		IPicWidth:    e.width,
		IPicHeight:   e.height,
		UiTimeStamp:  e.frameIndex * 33, // ~30fps timestamp in ms
	}

	encSrcPic.PData[0] = (*uint8)(unsafe.Pointer(&ycbcr.Y[0]))
	encSrcPic.PData[1] = (*uint8)(unsafe.Pointer(&ycbcr.Cb[0]))
	encSrcPic.PData[2] = (*uint8)(unsafe.Pointer(&ycbcr.Cr[0]))

	encInfo := openh264.SFrameBSInfo{}
	if ret := e.encoder.EncodeFrame(&encSrcPic, &encInfo); ret != openh264.CmResultSuccess {
		return nil, fmt.Errorf("encode failed: %d", ret)
	}

	e.frameIndex++

	if encInfo.EFrameType == openh264.VideoFrameTypeSkip {
		return nil, nil
	}

	// Collect all NAL units
	var result []byte
	for iLayer := 0; iLayer < int(encInfo.ILayerNum); iLayer++ {
		pLayerBsInfo := &encInfo.SLayerInfo[iLayer]
		var iLayerSize int32
		nallens := unsafe.Slice(pLayerBsInfo.PNalLengthInByte, pLayerBsInfo.INalCount)
		for _, l := range nallens {
			iLayerSize += l
		}
		nals := unsafe.Slice(pLayerBsInfo.PBsBuf, iLayerSize)
		result = append(result, nals...)
	}

	return result, nil
}

// close closes the encoder
func (e *h264Encoder) close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.encoder != nil {
		e.encoder.Uninitialize()
		openh264.WelsDestroySVCEncoder(e.encoder)
		e.encoder = nil
	}
}

// CreateSession creates a new WebRTC session with H.264 video encoding
func (m *Manager) CreateSession(config SessionConfig, onSignal func(signal SignalMessage), onInput func(input InputEvent)) (*Session, error) {
	log.Printf("[WebRTC] CreateSession starting for session %s", config.SessionID)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if OpenH264 pre-loading failed
	if m.h264LoadErr != nil {
		log.Printf("[WebRTC] OpenH264 not available (pre-load failed): %v", m.h264LoadErr)
		return nil, fmt.Errorf("WebRTC remote desktop not available: H.264 encoder failed to initialize: %w", m.h264LoadErr)
	}

	// Load OpenH264 if not already loaded (should be pre-loaded but just in case)
	if !m.h264Loaded {
		log.Printf("[WebRTC] Loading OpenH264...")
		if err := m.loadOpenH264(); err != nil {
			return nil, fmt.Errorf("failed to load OpenH264: %w", err)
		}
	}
	log.Printf("[WebRTC] OpenH264 loaded successfully")

	// Stop existing session if any
	if existing, ok := m.sessions[config.SessionID]; ok {
		existing.Stop()
	}

	// Configure ICE servers
	iceServers := []webrtc.ICEServer{}
	for _, server := range config.ICEServers {
		iceServer := webrtc.ICEServer{
			URLs: server.URLs,
		}
		if server.Username != "" {
			iceServer.Username = server.Username
			iceServer.Credential = server.Credential
		}
		iceServers = append(iceServers, iceServer)
	}

	// Default STUN servers if none provided
	// NOTE: TURN servers temporarily disabled - metered.ca rate limited
	// For LAN connections, STUN alone should work via host candidates
	if len(iceServers) == 0 {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
			{URLs: []string{"stun:stun2.l.google.com:19302"}},
			{URLs: []string{"stun:stun3.l.google.com:19302"}},
		}
	}

	// Log ICE servers being used
	log.Printf("[WebRTC] Configuring %d ICE servers:", len(iceServers))
	for i, server := range iceServers {
		log.Printf("[WebRTC]   [%d] URLs: %v, Username: %s", i, server.URLs, server.Username)
	}

	// Get quality settings for fps and bitrate (resolution limits are fallbacks only)
	maxWidth, maxHeight, fps, bitrate := getVideoConstraints(config.Quality)

	// Use actual screen dimensions for accurate cursor mapping
	// This ensures video coordinates match screen coordinates 1:1
	screenWidth, screenHeight := getActualScreenDimensions()

	// Cap to quality limits to prevent excessive bandwidth on very large screens
	if screenWidth > maxWidth {
		screenWidth = maxWidth
	}
	if screenHeight > maxHeight {
		screenHeight = maxHeight
	}

	log.Printf("[WebRTC] Using actual screen dimensions: %dx%d (max: %dx%d)", screenWidth, screenHeight, maxWidth, maxHeight)
	log.Printf("[WebRTC] Quality settings: fps=%d, bitrate=%d", fps, bitrate)

	// Create H.264 encoder - using OpenH264 (software) for stability
	// NOTE: Media Foundation encoder disabled due to crashes on some systems
	log.Printf("[WebRTC] Creating H.264 encoder with dims %dx%d, bitrate %d...", screenWidth, screenHeight, bitrate)

	var videoEncoder VideoEncoder
	var legacyEncoder *h264Encoder

	// Use OpenH264 encoder (software, but stable)
	log.Printf("[WebRTC] Creating OpenH264 encoder (software)...")
	encoder, err := newH264Encoder(screenWidth, screenHeight, bitrate)
	if err != nil {
		log.Printf("[WebRTC] OpenH264 encoder failed: %v", err)
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}
	legacyEncoder = encoder
	videoEncoder = &openH264Wrapper{enc: encoder}
	log.Printf("[WebRTC] OpenH264 encoder created successfully")
	log.Printf("[WebRTC] H.264 encoder ready (internal dims: %dx%d, hardware=%v)",
		videoEncoder.GetWidth(), videoEncoder.GetHeight(), videoEncoder.IsHardware())

	// Create media engine with H.264 codec
	// Profile-level-id: 42e032 = Baseline profile, level 5.0 (supports 4K30, higher bitrates)
	// Level 5.0 supports up to 4096x2304@30fps or 2560x1440@60fps at 135 Mbps
	log.Printf("[WebRTC] Creating media engine...")
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e032",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		videoEncoder.Close()
		return nil, fmt.Errorf("failed to register H264 codec: %w", err)
	}
	log.Printf("[WebRTC] Media engine created")

	// Create interceptor registry for PLI support
	interceptorRegistry := &interceptor.Registry{}
	intervalPLIFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		videoEncoder.Close()
		return nil, fmt.Errorf("failed to create PLI interceptor: %w", err)
	}
	interceptorRegistry.Add(intervalPLIFactory)

	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		videoEncoder.Close()
		return nil, fmt.Errorf("failed to register interceptors: %w", err)
	}

	// Create SettingEngine to avoid hangs in Windows services
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	// Note: ICE Lite removed - incompatible with sending media tracks
	settingEngine.SetICETimeouts(5*time.Second, 25*time.Second, 2*time.Second) // Disconnected, Failed, Keepalive
	log.Printf("[WebRTC] ICE settings: mDNS=disabled, timeouts=5s/25s/2s")

	// Create API with media engine and setting engine
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
		webrtc.WithSettingEngine(settingEngine),
	)

	// Create peer connection
	log.Printf("[WebRTC] Creating peer connection...")
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		videoEncoder.Close()
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}
	log.Printf("[WebRTC] Peer connection created")

	// Create video track
	log.Printf("[WebRTC] Creating video track...")
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e032",
		},
		"video",
		"screen",
	)
	if err != nil {
		log.Printf("[WebRTC] ERROR: Failed to create video track: %v", err)
		videoEncoder.Close()
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}
	log.Printf("[WebRTC] Video track created successfully")

	// Add video track to peer connection
	log.Printf("[WebRTC] Adding video track to peer connection...")
	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		log.Printf("[WebRTC] ERROR: Failed to add video track: %v", err)
		videoEncoder.Close()
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}
	log.Printf("[WebRTC] Video track added to peer connection")
	log.Printf("[WebRTC] DEBUG: Starting RTCP goroutine...")

	// Read incoming RTCP packets for PLI handling
	go func() {
		log.Printf("[WebRTC] DEBUG: RTCP goroutine started")
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	log.Printf("[WebRTC] DEBUG: Creating context...")
	ctx, cancel := context.WithCancel(context.Background())
	log.Printf("[WebRTC] DEBUG: Context created")

	// Try DXGI capture first (high performance), fall back to screenshot if it fails
	// Use a timeout to prevent blocking the SDP exchange
	var dxgiCap *capture.DXGICapture
	useDXGI := false

	log.Printf("[WebRTC] Attempting DXGI capture initialization (3s timeout)...")
	dxgiInitDone := make(chan struct{})
	var dxgiErr error
	go func() {
		defer close(dxgiInitDone)
		dxgiCap, dxgiErr = capture.NewDXGICapture(0) // Monitor 0 (primary)
	}()

	select {
	case <-dxgiInitDone:
		if dxgiErr != nil {
			log.Printf("[WebRTC] DXGI capture initialization failed, using screenshot fallback: %v", dxgiErr)
			dxgiCap = nil
			useDXGI = false
		} else {
			useDXGI = true
			w, h := dxgiCap.GetDimensions()
			log.Printf("[WebRTC] DXGI capture enabled: %dx%d", w, h)
		}
	case <-time.After(3 * time.Second):
		log.Printf("[WebRTC] DXGI capture initialization timed out after 3s, using screenshot fallback")
		dxgiCap = nil
		useDXGI = false
	}
	log.Printf("[WebRTC] Capture initialization complete, useDXGI=%v", useDXGI)

	// Create capture strategy for smart frame decisions
	captureStrategy := NewCaptureStrategy(screenWidth, screenHeight)

	session := &Session{
		ID:              config.SessionID,
		PeerConnection:  peerConnection,
		VideoTrack:      videoTrack,
		Quality:         config.Quality,
		Active:          true,
		Connected:       false,
		OnSignal:        onSignal,
		OnInput:         onInput,
		ctx:             ctx,
		cancel:          cancel,
		encoder:         legacyEncoder,
		videoEncoder:    videoEncoder,
		dxgiCapture:     dxgiCap,
		useDXGI:         useDXGI,
		captureStrategy: captureStrategy,
		encoderBitrate:  bitrate,
	}

	// Handle incoming data channels from the browser (offerer creates, we receive)
	log.Printf("[WebRTC] Setting up OnDataChannel handler for incoming data channels...")
	peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("[WebRTC] Received data channel: %s (id=%d)", dc.Label(), dc.ID())

		session.mu.Lock()
		session.DataChannel = dc
		session.mu.Unlock()

		dc.OnOpen(func() {
			log.Printf("[WebRTC] Data channel '%s' opened for session %s", dc.Label(), session.ID)
			session.mu.Lock()
			session.Connected = true
			session.mu.Unlock()
		})

		dc.OnClose(func() {
			log.Printf("[WebRTC] Data channel '%s' closed for session %s", dc.Label(), session.ID)
			session.mu.Lock()
			session.Connected = false
			session.mu.Unlock()
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			// Try to detect message type first
			var typeCheck struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg.Data, &typeCheck); err != nil {
				log.Printf("[WebRTC] Failed to detect message type: %v", err)
				return
			}

			// Handle ping messages for RTT measurement
			if typeCheck.Type == "ping" {
				var ping PingMessage
				if err := json.Unmarshal(msg.Data, &ping); err == nil {
					session.sendPong(ping.ClientT)
				}
				return
			}

			// Handle input events
			var input InputEvent
			if err := json.Unmarshal(msg.Data, &input); err != nil {
				log.Printf("[WebRTC] Failed to unmarshal input event: %v (raw=%s)", err, string(msg.Data))
				return
			}
			log.Printf("[WebRTC] Received input: type=%s, x=%.1f, y=%.1f, button=%d, key=%s",
				input.Type, input.X, input.Y, input.Button, input.Key)
			if session.OnInput != nil {
				session.OnInput(input)
			}
		})
	})
	log.Printf("[WebRTC] OnDataChannel handler set up")

	// Handle ICE candidates
	log.Printf("[WebRTC] Setting up ICE candidate handler...")
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			log.Printf("[WebRTC] ICE gathering complete for session %s", session.ID)
			return
		}
		// Log detailed candidate info for debugging NAT traversal issues
		log.Printf("[WebRTC] ICE candidate generated: type=%s, protocol=%s, address=%s:%d, relatedAddr=%s:%d",
			candidate.Typ.String(),
			candidate.Protocol.String(),
			candidate.Address,
			candidate.Port,
			candidate.RelatedAddress,
			candidate.RelatedPort,
		)
		candidateJSON, err := json.Marshal(candidate.ToJSON())
		if err != nil {
			log.Printf("[WebRTC] Failed to marshal ICE candidate: %v", err)
			return
		}
		if session.OnSignal != nil {
			session.OnSignal(SignalMessage{
				Type:      "candidate",
				SessionID: session.ID,
				Candidate: string(candidateJSON),
			})
		}
	})
	log.Printf("[WebRTC] ICE candidate handler set up")

	// Handle ICE gathering state for debugging
	peerConnection.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		log.Printf("[WebRTC] ICE gathering state for session %s: %s", session.ID, state.String())
	})

	// Handle connection state changes
	log.Printf("[WebRTC] Setting up connection state handler...")
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTC connection state for session %s: %s", session.ID, state.String())
		switch state {
		case webrtc.PeerConnectionStateConnected:
			log.Printf("WebRTC connected for session %s - starting screen capture", session.ID)
			go session.startScreenCapture(fps)
		case webrtc.PeerConnectionStateDisconnected:
			log.Printf("WebRTC disconnected for session %s", session.ID)
		case webrtc.PeerConnectionStateFailed:
			log.Printf("WebRTC connection failed for session %s", session.ID)
			session.Stop()
		case webrtc.PeerConnectionStateClosed:
			log.Printf("WebRTC connection closed for session %s", session.ID)
		}
	})
	log.Printf("[WebRTC] Connection state handler set up")

	log.Printf("[WebRTC] Setting up ICE connection state handler...")
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("WebRTC ICE state for session %s: %s", session.ID, state.String())

		// Provide debugging info on failure
		if state == webrtc.ICEConnectionStateFailed {
			log.Printf("[WebRTC] ICE FAILED for session %s - this usually means:", session.ID)
			log.Printf("[WebRTC]   1. Both peers behind symmetric NAT and TURN relay failed")
			log.Printf("[WebRTC]   2. Firewall blocking UDP on ports 3478 or relay ports")
			log.Printf("[WebRTC]   3. TURN server credentials invalid or rate limited")
			log.Printf("[WebRTC] Check that TURN servers are accessible and not rate limited")
		}
	})
	log.Printf("[WebRTC] ICE connection state handler set up")

	log.Printf("[WebRTC] Storing session in manager...")
	m.sessions[config.SessionID] = session

	log.Printf("WebRTC session %s created (quality: %s, %dx%d@%dfps, %dkbps)",
		config.SessionID, config.Quality, screenWidth, screenHeight, fps, bitrate/1000)
	return session, nil
}

// FrameTiming contains latency instrumentation for each frame
type FrameTiming struct {
	Type       string  `json:"type"` // "frameTiming"
	FrameID    uint64  `json:"frameId"`
	CaptureMs  float64 `json:"captureMs"`  // Time to capture screen
	ConvertMs  float64 `json:"convertMs"`  // Time to convert RGBA to YCbCr
	EncodeMs   float64 `json:"encodeMs"`   // Time to encode to H.264
	TotalMs    float64 `json:"totalMs"`    // Total pipeline time
	Timestamp  int64   `json:"timestamp"`  // Unix microseconds when capture started
}

// PingMessage for round-trip latency measurement
type PingMessage struct {
	Type     string `json:"type"` // "ping" or "pong"
	ClientT  int64  `json:"clientT,omitempty"`
	ServerT  int64  `json:"serverT,omitempty"`
}

// CursorMessage represents cursor info sent to dashboard
type CursorMessage struct {
	Type    string `json:"type"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Visible bool   `json:"visible"`
}

// CursorShapeMessage represents cursor shape change sent to dashboard
type CursorShapeMessage struct {
	Type  string      `json:"type"` // "cursorShape"
	Shape CursorShape `json:"shape"`
}

// CursorShape describes the cursor appearance
type CursorShape struct {
	ShapeType string       `json:"type"` // "default", "pointer", "text", "wait", "crosshair", "move", "not-allowed", "custom"
	Hotspot   CursorHotspot `json:"hotspot"`
	Image     string       `json:"image,omitempty"` // Base64 PNG for custom cursors
}

// CursorHotspot is the click point within the cursor image
type CursorHotspot struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// RemoteInfoMessage tells dashboard about screen dimensions
type RemoteInfoMessage struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// startScreenCapture captures the screen and sends frames over WebRTC
func (s *Session) startScreenCapture(fps int) {
	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	// Cursor update ticker (120Hz for instant cursor feedback)
	// Higher rate reduces perceived latency since client renders cursor locally
	cursorTicker := time.NewTicker(8 * time.Millisecond)
	defer cursorTicker.Stop()

	// Get primary display bounds safely
	var bounds image.Rectangle
	screenWidth := s.videoEncoder.GetWidth()
	screenHeight := s.videoEncoder.GetHeight()

	// Use DXGI dimensions if available
	if s.useDXGI && s.dxgiCapture != nil {
		w, h := s.dxgiCapture.GetDimensions()
		screenWidth = w
		screenHeight = h
		bounds = image.Rect(0, 0, screenWidth, screenHeight)
		log.Printf("[WebRTC] Using DXGI capture: %dx%d", screenWidth, screenHeight)
	} else {
		// Fall back to screenshot library
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WebRTC] PANIC in GetDisplayBounds: %v, using encoder dimensions", r)
				}
			}()
			bounds = screenshot.GetDisplayBounds(0)
			screenWidth = bounds.Dx()
			screenHeight = bounds.Dy()
		}()

		if bounds.Empty() {
			bounds = image.Rect(0, 0, screenWidth, screenHeight)
		}
		log.Printf("[WebRTC] Using screenshot fallback: %dx%d", screenWidth, screenHeight)
	}

	// Get encoder's aligned dimensions
	encoderWidth := s.videoEncoder.GetWidth()
	encoderHeight := s.videoEncoder.GetHeight()

	// Check if we need to pad (encoder dimensions might be larger due to alignment)
	needsPadding := encoderWidth != screenWidth || encoderHeight != screenHeight

	// Check if we can use direct BGRA encoding (Media Foundation)
	useDirectBGRA := s.useDXGI && s.videoEncoder.IsHardware()

	log.Printf("Starting screen capture: screen=%dx%d, encoder=%dx%d, padding=%v, useDXGI=%v, directBGRA=%v, hardware=%v @ %d fps",
		screenWidth, screenHeight, encoderWidth, encoderHeight, needsPadding, s.useDXGI, useDirectBGRA, s.videoEncoder.IsHardware(), fps)

	// Send remote info to dashboard
	s.sendRemoteInfo(screenWidth, screenHeight)

	// Track last cursor position and shape to avoid sending duplicates
	lastCursorX, lastCursorY := -1, -1
	lastCursorShape := ""
	frameCount := 0
	skipCount := 0
	strategySkipCount := 0
	const maxConsecutiveSkips = 5 // Don't skip more than 5 frames in a row

	// Frame timing instrumentation
	var frameID uint64 = 0
	timingInterval := 30 // Send timing data every N frames

	for {
		select {
		case <-s.ctx.Done():
			log.Printf("Screen capture stopped for session %s (frames=%d, skipped=%d, strategySkips=%d)", s.ID, frameCount, skipCount, strategySkipCount)
			return

		case <-cursorTicker.C:
			// Send cursor position updates via data channel (for local cursor rendering)
			cursorX, cursorY := getCursorPos()
			if cursorX != lastCursorX || cursorY != lastCursorY {
				s.sendCursorUpdate(cursorX, cursorY, true)
				lastCursorX, lastCursorY = cursorX, cursorY
			}

			// Check for cursor shape changes
			cursorShape := getCursorShape()
			if cursorShape != lastCursorShape {
				s.sendCursorShape(cursorShape)
				lastCursorShape = cursorShape
			}

		case <-ticker.C:
			if !s.Active {
				return
			}

			var data []byte
			var err error

			if s.useDXGI && s.dxgiCapture != nil {
				// Use DXGI capture (faster, with dirty rectangles)
				frame, captureErr := s.dxgiCapture.CaptureFrame(50)
				if captureErr != nil {
					log.Printf("DXGI capture failed: %v", captureErr)
					continue
				}
				if frame == nil {
					// No new frame (screen unchanged)
					skipCount++
					continue
				}

				// Use capture strategy for smart frame decisions
				var decision FrameDecision
				if s.captureStrategy != nil {
					decision = s.captureStrategy.Decide(frame.DirtyRects)

					if !decision.ShouldEncode && strategySkipCount < maxConsecutiveSkips {
						strategySkipCount++
						continue
					}
					strategySkipCount = 0

					// Apply quality adjustment
					if decision.QualityAdjust != 0 {
						s.adjustEncoderQuality(decision.QualityAdjust)
					}

					// Force keyframe if needed
					if decision.ForceKeyframe {
						s.videoEncoder.ForceKeyframe()
					}
				}

				// Use direct BGRA encoding if supported (Media Foundation hardware)
				if useDirectBGRA {
					data, err = s.videoEncoder.EncodeBGRA(frame.Data, frame.Width, frame.Height, frame.Stride, decision.ForceKeyframe)
				} else {
					// Convert BGRA to RGBA then YCbCr for OpenH264
					img := bgraToRGBA(frame.Data, frame.Width, frame.Height, frame.Stride)
					var ycbcr *image.YCbCr
					if needsPadding {
						ycbcr = rgbaToYCbCrPadded(img, encoderWidth, encoderHeight)
					} else {
						ycbcr = rgbaToYCbCr(img)
					}
					data, err = s.videoEncoder.Encode(ycbcr)
				}
			} else {
				// Fall back to screenshot library - with timing instrumentation
				frameID++
				t0 := time.Now()

				img, captureErr := screenshot.CaptureRect(bounds)
				if captureErr != nil {
					log.Printf("Failed to capture screen: %v", captureErr)
					continue
				}
				t1 := time.Now()

				// NOTE: Cursor is NOT drawn on the video - client renders cursor locally via CursorOverlay
				// This eliminates the full pipeline latency (capture→encode→transmit→decode→render)
				// and provides instant cursor feedback at native mouse polling rate (~125-1000Hz)
				// Server cursor position is sent via DataChannel for correction/sync

				// Convert to YCbCr (with padding if needed for encoder alignment)
				var ycbcr *image.YCbCr
				if needsPadding {
					ycbcr = rgbaToYCbCrPadded(img, encoderWidth, encoderHeight)
				} else {
					ycbcr = rgbaToYCbCr(img)
				}
				t2 := time.Now()

				// Encode to H.264
				data, err = s.videoEncoder.Encode(ycbcr)
				t3 := time.Now()

				// Send timing data every N frames (for latency debugging)
				if frameID%uint64(timingInterval) == 0 {
					s.sendFrameTiming(FrameTiming{
						Type:      "frameTiming",
						FrameID:   frameID,
						CaptureMs: float64(t1.Sub(t0).Microseconds()) / 1000.0,
						ConvertMs: float64(t2.Sub(t1).Microseconds()) / 1000.0,
						EncodeMs:  float64(t3.Sub(t2).Microseconds()) / 1000.0,
						TotalMs:   float64(t3.Sub(t0).Microseconds()) / 1000.0,
						Timestamp: t0.UnixMicro(),
					})
				}
			}

			if err != nil {
				log.Printf("Failed to encode frame: %v", err)
				continue
			}

			if data == nil {
				skipCount++
				continue // Frame was skipped
			}

			// Write to WebRTC track
			if err := s.VideoTrack.WriteSample(media.Sample{
				Data:     data,
				Duration: frameDuration,
			}); err != nil {
				log.Printf("Failed to write sample: %v", err)
			}
			frameCount++
		}
	}
}

// adjustEncoderQuality adjusts encoder bitrate based on capture strategy recommendation
func (s *Session) adjustEncoderQuality(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentBitrate := s.encoderBitrate

	switch delta {
	case -2:
		s.encoderBitrate = currentBitrate * 50 / 100 // 50%
	case -1:
		s.encoderBitrate = currentBitrate * 75 / 100 // 75%
	case 1:
		s.encoderBitrate = currentBitrate * 125 / 100 // 125%
	case 2:
		s.encoderBitrate = currentBitrate * 150 / 100 // 150%
	}

	// Clamp to reasonable range
	if s.encoderBitrate < 500_000 {
		s.encoderBitrate = 500_000 // Min 500 Kbps
	}
	if s.encoderBitrate > 10_000_000 {
		s.encoderBitrate = 10_000_000 // Max 10 Mbps
	}

	s.videoEncoder.SetBitrate(s.encoderBitrate)
}

// Windows cursor constants
const (
	IDC_ARROW    = 32512
	IDC_IBEAM    = 32513
	IDC_WAIT     = 32514
	IDC_CROSS    = 32515
	IDC_UPARROW  = 32516
	IDC_SIZE     = 32640
	IDC_ICON     = 32641
	IDC_SIZENWSE = 32642
	IDC_SIZENESW = 32643
	IDC_SIZEWE   = 32644
	IDC_SIZENS   = 32645
	IDC_SIZEALL  = 32646
	IDC_NO       = 32648
	IDC_HAND     = 32649
	IDC_APPSTART = 32650
	IDC_HELP     = 32651
)

// CURSORINFO structure
type cursorInfo struct {
	CbSize      uint32
	Flags       uint32
	HCursor     uintptr
	PtScreenPos point
}

var (
	procGetCursorInfo = user32.NewProc("GetCursorInfo")
	procLoadCursor    = user32.NewProc("LoadCursorW")

	// Cache standard cursor handles for comparison
	standardCursors     map[uintptr]string
	standardCursorsOnce sync.Once
)

func initStandardCursors() {
	standardCursorsOnce.Do(func() {
		standardCursors = make(map[uintptr]string)
		cursors := []struct {
			id    uintptr
			name  string
		}{
			{IDC_ARROW, "default"},
			{IDC_IBEAM, "text"},
			{IDC_WAIT, "wait"},
			{IDC_CROSS, "crosshair"},
			{IDC_HAND, "pointer"},
			{IDC_SIZEALL, "move"},
			{IDC_NO, "not-allowed"},
			{IDC_SIZENWSE, "nwse-resize"},
			{IDC_SIZENESW, "nesw-resize"},
			{IDC_SIZEWE, "ew-resize"},
			{IDC_SIZENS, "ns-resize"},
		}
		for _, c := range cursors {
			h, _, _ := procLoadCursor.Call(0, c.id)
			if h != 0 {
				standardCursors[h] = c.name
			}
		}
	})
}

// getCursorShape returns the current cursor shape type
func getCursorShape() string {
	initStandardCursors()

	var ci cursorInfo
	ci.CbSize = uint32(unsafe.Sizeof(ci))

	ret, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if ret == 0 {
		return "default"
	}

	// Look up cursor type
	if name, ok := standardCursors[ci.HCursor]; ok {
		return name
	}

	return "default"
}

// sendCursorUpdate sends cursor position to dashboard via data channel
func (s *Session) sendCursorUpdate(x, y int, visible bool) {
	s.mu.Lock()
	dc := s.DataChannel
	s.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	msg := CursorMessage{
		Type:    "cursor",
		X:       x,
		Y:       y,
		Visible: visible,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	dc.SendText(string(data))
}

// sendCursorShape sends cursor shape to dashboard via data channel
func (s *Session) sendCursorShape(shapeType string) {
	s.mu.Lock()
	dc := s.DataChannel
	s.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	// Default hotspots for different cursor types
	hotspot := CursorHotspot{X: 0, Y: 0}
	switch shapeType {
	case "pointer":
		hotspot = CursorHotspot{X: 6, Y: 0}
	case "text":
		hotspot = CursorHotspot{X: 4, Y: 9}
	case "crosshair":
		hotspot = CursorHotspot{X: 10, Y: 10}
	case "move":
		hotspot = CursorHotspot{X: 11, Y: 11}
	case "not-allowed":
		hotspot = CursorHotspot{X: 10, Y: 10}
	case "wait":
		hotspot = CursorHotspot{X: 0, Y: 0}
	}

	msg := CursorShapeMessage{
		Type: "cursorShape",
		Shape: CursorShape{
			ShapeType: shapeType,
			Hotspot:   hotspot,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	dc.SendText(string(data))
}

// sendFrameTiming sends frame latency data to dashboard for debugging
func (s *Session) sendFrameTiming(timing FrameTiming) {
	s.mu.Lock()
	dc := s.DataChannel
	s.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	data, err := json.Marshal(timing)
	if err != nil {
		return
	}
	dc.SendText(string(data))
}

// sendPong responds to a ping with a pong for RTT measurement
func (s *Session) sendPong(clientT int64) {
	s.mu.Lock()
	dc := s.DataChannel
	s.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	msg := PingMessage{
		Type:    "pong",
		ClientT: clientT,
		ServerT: time.Now().UnixMicro(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	dc.SendText(string(data))
}

// sendRemoteInfo sends screen dimensions to dashboard
func (s *Session) sendRemoteInfo(width, height int) {
	s.mu.Lock()
	dc := s.DataChannel
	s.mu.Unlock()

	// Wait for data channel to open (up to 5 seconds)
	for i := 0; i < 50; i++ {
		s.mu.Lock()
		dc = s.DataChannel
		s.mu.Unlock()
		if dc != nil && dc.ReadyState() == webrtc.DataChannelStateOpen {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		log.Printf("[WebRTC] Could not send remote info - data channel not ready")
		return
	}

	msg := RemoteInfoMessage{
		Type:   "remoteInfo",
		Width:  width,
		Height: height,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	dc.SendText(string(data))
	log.Printf("[WebRTC] Sent remote info: %dx%d", width, height)
}

// bgraToRGBA converts BGRA pixel data to RGBA image
func bgraToRGBA(data []byte, width, height, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		srcRow := y * stride
		dstRow := y * img.Stride

		for x := 0; x < width; x++ {
			srcOff := srcRow + x*4
			dstOff := dstRow + x*4

			// BGRA -> RGBA
			img.Pix[dstOff+0] = data[srcOff+2] // R
			img.Pix[dstOff+1] = data[srcOff+1] // G
			img.Pix[dstOff+2] = data[srcOff+0] // B
			img.Pix[dstOff+3] = data[srcOff+3] // A
		}
	}

	return img
}

// CreateOffer creates an SDP offer for the session
func (s *Session) CreateOffer() (string, error) {
	offer, err := s.PeerConnection.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}

	err = s.PeerConnection.SetLocalDescription(offer)
	if err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(s.PeerConnection)
	select {
	case <-gatherComplete:
		log.Printf("ICE gathering complete for session %s", s.ID)
	case <-time.After(10 * time.Second):
		log.Printf("ICE gathering timeout for session %s, proceeding with available candidates", s.ID)
	}

	localDesc := s.PeerConnection.LocalDescription()
	if localDesc != nil {
		return localDesc.SDP, nil
	}
	return offer.SDP, nil
}


// filterMDNSCandidates removes mDNS candidates (*.local) from SDP to prevent hangs
func filterMDNSCandidates(sdp string) string {
	lines := strings.Split(sdp, "\r\n")
	filtered := make([]string, 0, len(lines))
	mdnsPattern := regexp.MustCompile(`\.local\s`)
	removedCount := 0

	for _, line := range lines {
		// Remove ICE candidates that contain .local (mDNS)
		if strings.HasPrefix(line, "a=candidate:") && mdnsPattern.MatchString(line) {
			log.Printf("[WebRTC] Filtering mDNS candidate: %s", line[:min(len(line), 80)])
			removedCount++
			continue
		}
		filtered = append(filtered, line)
	}

	if removedCount > 0 {
		log.Printf("[WebRTC] Filtered %d mDNS candidates from SDP", removedCount)
	}
	return strings.Join(filtered, "\r\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SetRemoteDescription sets the remote SDP (offer or answer)
func (s *Session) SetRemoteDescription(sdpType, sdp string) error {
	log.Printf("[WebRTC] SetRemoteDescription called, type=%s, sdp length=%d", sdpType, len(sdp))

	// Write SDP to file for debugging
	sdpFile := "C:\\ProgramData\\Sentinel\\last_sdp.txt"
	if err := os.WriteFile(sdpFile, []byte(sdp), 0644); err != nil {
		log.Printf("[WebRTC] Failed to write SDP to file: %v", err)
	} else {
		log.Printf("[WebRTC] SDP written to %s", sdpFile)
	}

	// Filter out mDNS candidates to prevent hangs in Windows services
	sdp = filterMDNSCandidates(sdp)
	log.Printf("[WebRTC] After filtering, sdp length=%d", len(sdp))

	var sdpTypeEnum webrtc.SDPType
	switch sdpType {
	case "answer":
		sdpTypeEnum = webrtc.SDPTypeAnswer
	case "offer":
		sdpTypeEnum = webrtc.SDPTypeOffer
	default:
		sdpTypeEnum = webrtc.SDPTypeAnswer
	}
	log.Printf("[WebRTC] SetRemoteDescription: sdpTypeEnum=%v", sdpTypeEnum)

	log.Printf("[WebRTC] SetRemoteDescription: Calling PeerConnection.SetRemoteDescription...")
	
	// Use a channel to add timeout protection
	done := make(chan error, 1)
	go func() {
		done <- s.PeerConnection.SetRemoteDescription(webrtc.SessionDescription{
			Type: sdpTypeEnum,
			SDP:  sdp,
		})
	}()
	
	var err error
	select {
	case err = <-done:
		log.Printf("[WebRTC] SetRemoteDescription: returned, err=%v", err)
	case <-time.After(15 * time.Second):
		log.Printf("[WebRTC] SetRemoteDescription: TIMEOUT after 15 seconds!")
		return fmt.Errorf("SetRemoteDescription timeout after 15 seconds")
	}
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}
	log.Printf("[WebRTC] SetRemoteDescription: Success")
	return nil
}

// CreateAnswer creates an SDP answer after setting remote offer
func (s *Session) CreateAnswer() (string, error) {
	answer, err := s.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create answer: %w", err)
	}

	err = s.PeerConnection.SetLocalDescription(answer)
	if err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(s.PeerConnection)
	select {
	case <-gatherComplete:
	case <-time.After(10 * time.Second):
		log.Printf("ICE gathering timed out, continuing with available candidates")
	}

	localDesc := s.PeerConnection.LocalDescription()
	if localDesc != nil {
		return localDesc.SDP, nil
	}
	return answer.SDP, nil
}

// AddICECandidate adds a remote ICE candidate
func (s *Session) AddICECandidate(candidateJSON string) error {
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return fmt.Errorf("failed to unmarshal ICE candidate: %w", err)
	}
	if err := s.PeerConnection.AddICECandidate(candidate); err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}
	return nil
}

// Stop stops the WebRTC session
func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Active {
		return
	}

	s.Active = false
	s.Connected = false

	if s.cancel != nil {
		s.cancel()
	}

	if s.videoEncoder != nil {
		s.videoEncoder.Close()
	} else if s.encoder != nil {
		// Legacy fallback (shouldn't happen)
		s.encoder.close()
	}

	if s.dxgiCapture != nil {
		s.dxgiCapture.Release()
		s.dxgiCapture = nil
	}

	if s.DataChannel != nil {
		s.DataChannel.Close()
	}

	if s.PeerConnection != nil {
		s.PeerConnection.Close()
	}

	log.Printf("WebRTC session %s stopped", s.ID)
}

// GetSession returns a session by ID
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// StopSession stops and removes a session
func (m *Manager) StopSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[sessionID]; ok {
		session.Stop()
		delete(m.sessions, sessionID)
		log.Printf("WebRTC session %s removed from manager", sessionID)
	}
}

// HandleSignal processes incoming signaling messages from the viewer
func (m *Manager) HandleSignal(signal SignalMessage) error {
	session, ok := m.GetSession(signal.SessionID)
	if !ok {
		return fmt.Errorf("session %s not found", signal.SessionID)
	}

	switch signal.Type {
	case "answer":
		log.Printf("Processing SDP answer for session %s", signal.SessionID)
		return session.SetRemoteDescription("answer", signal.SDP)
	case "candidate":
		log.Printf("Processing ICE candidate for session %s", signal.SessionID)
		return session.AddICECandidate(signal.Candidate)
	default:
		log.Printf("Unknown signal type: %s", signal.Type)
	}
	return nil
}

// GetActiveSessions returns the count of active sessions
func (m *Manager) GetActiveSessions() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
