//go:build windows

package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"regexp"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/kbinani/screenshot"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/y9o/go-openh264"
)

// ICEServer represents a STUN/TURN server configuration
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SessionConfig contains configuration for a WebRTC session
type SessionConfig struct {
	SessionID  string      `json:"sessionId"`
	ICEServers []ICEServer `json:"iceServers"`
	Quality    string      `json:"quality"` // "low", "medium", "high"
}

// SignalMessage represents a signaling message (SDP or ICE candidate)
type SignalMessage struct {
	Type      string `json:"type"` // "offer", "answer", "candidate"
	SessionID string `json:"sessionId"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

// InputEvent represents a mouse or keyboard input event
type InputEvent struct {
	Type      string   `json:"type"` // "mouse" or "keyboard"
	Event     string   `json:"event"`
	X         float64  `json:"x,omitempty"`
	Y         float64  `json:"y,omitempty"`
	Button    int      `json:"button,omitempty"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	DeltaY    float64  `json:"deltaY,omitempty"`
}

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
	encoder        *h264Encoder
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
func getVideoConstraints(quality string) (int, int, int, int) {
	switch quality {
	case "low":
		return 1280, 720, 10, 800000 // 720p, 10fps, 800kbps
	case "high":
		return 1920, 1080, 30, 3000000 // 1080p, 30fps, 3Mbps
	default: // medium
		return 1920, 1080, 15, 1500000 // 1080p, 15fps, 1.5Mbps
	}
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
	if len(iceServers) == 0 {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
			{URLs: []string{"stun:stun2.l.google.com:19302"}},
		}
	}

	// Get quality settings for fps and bitrate
	_, _, fps, bitrate := getVideoConstraints(config.Quality)

	// Use quality-based dimensions for the encoder
	// We don't call GetDisplayBounds here because it can cause native crashes
	// when running as a Windows service. The actual screen bounds are obtained
	// later in startScreenCapture where we can handle failures more gracefully.
	screenWidth, screenHeight, _, _ := getVideoConstraints(config.Quality)
	log.Printf("[WebRTC] Using quality-based dimensions: %dx%d", screenWidth, screenHeight)
	log.Printf("[WebRTC] Quality settings: fps=%d, bitrate=%d", fps, bitrate)

	// Create H.264 encoder with actual screen dimensions (alignment happens inside)
	log.Printf("[WebRTC] Creating H.264 encoder with dims %dx%d, bitrate %d...", screenWidth, screenHeight, bitrate)
	log.Printf("[WebRTC] About to call newH264Encoder...")
	encoder, err := newH264Encoder(screenWidth, screenHeight, bitrate)
	log.Printf("[WebRTC] newH264Encoder returned, err=%v", err)
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}
	log.Printf("[WebRTC] H.264 encoder created successfully (internal dims: %dx%d)", encoder.width, encoder.height)

	// Create media engine with H.264 codec
	log.Printf("[WebRTC] Creating media engine...")
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		encoder.close()
		return nil, fmt.Errorf("failed to register H264 codec: %w", err)
	}
	log.Printf("[WebRTC] Media engine created")

	// Create interceptor registry for PLI support
	interceptorRegistry := &interceptor.Registry{}
	intervalPLIFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		encoder.close()
		return nil, fmt.Errorf("failed to create PLI interceptor: %w", err)
	}
	interceptorRegistry.Add(intervalPLIFactory)

	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		encoder.close()
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
		encoder.close()
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}
	log.Printf("[WebRTC] Peer connection created")

	// Create video track
	log.Printf("[WebRTC] Creating video track...")
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		"video",
		"screen",
	)
	if err != nil {
		log.Printf("[WebRTC] ERROR: Failed to create video track: %v", err)
		encoder.close()
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}
	log.Printf("[WebRTC] Video track created successfully")

	// Add video track to peer connection
	log.Printf("[WebRTC] Adding video track to peer connection...")
	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		log.Printf("[WebRTC] ERROR: Failed to add video track: %v", err)
		encoder.close()
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}
	log.Printf("[WebRTC] Video track added to peer connection")

	// Read incoming RTCP packets for PLI handling
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		ID:             config.SessionID,
		PeerConnection: peerConnection,
		VideoTrack:     videoTrack,
		Quality:        config.Quality,
		Active:         true,
		Connected:      false,
		OnSignal:       onSignal,
		OnInput:        onInput,
		ctx:            ctx,
		cancel:         cancel,
		encoder:        encoder,
	}

	// Create data channel for input events
	log.Printf("[WebRTC] Creating data channel...")
	ordered := true
	dataChannel, err := peerConnection.CreateDataChannel("input", &webrtc.DataChannelInit{
		Ordered: &ordered,
	})
	if err != nil {
		log.Printf("[WebRTC] ERROR: Failed to create data channel: %v", err)
		encoder.close()
		peerConnection.Close()
		cancel()
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}
	log.Printf("[WebRTC] Data channel created")
	session.DataChannel = dataChannel
	log.Printf("[WebRTC] Setting up data channel event handlers...")

	// Handle data channel events
	dataChannel.OnOpen(func() {
		log.Printf("WebRTC data channel opened for session %s", session.ID)
		session.mu.Lock()
		session.Connected = true
		session.mu.Unlock()
	})

	dataChannel.OnClose(func() {
		log.Printf("WebRTC data channel closed for session %s", session.ID)
		session.mu.Lock()
		session.Connected = false
		session.mu.Unlock()
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		var input InputEvent
		if err := json.Unmarshal(msg.Data, &input); err != nil {
			log.Printf("Failed to unmarshal input event: %v", err)
			return
		}
		if session.OnInput != nil {
			session.OnInput(input)
		}
	})
	log.Printf("[WebRTC] Data channel event handlers set up")

	// Handle ICE candidates
	log.Printf("[WebRTC] Setting up ICE candidate handler...")
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		candidateJSON, err := json.Marshal(candidate.ToJSON())
		if err != nil {
			log.Printf("Failed to marshal ICE candidate: %v", err)
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
	})
	log.Printf("[WebRTC] ICE connection state handler set up")

	log.Printf("[WebRTC] Storing session in manager...")
	m.sessions[config.SessionID] = session

	log.Printf("WebRTC session %s created (quality: %s, %dx%d@%dfps, %dkbps)",
		config.SessionID, config.Quality, screenWidth, screenHeight, fps, bitrate/1000)
	return session, nil
}

// startScreenCapture captures the screen and sends frames over WebRTC
func (s *Session) startScreenCapture(fps int) {
	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	// Get primary display bounds safely
	// Use encoder dimensions as fallback if GetDisplayBounds fails
	var bounds image.Rectangle
	screenWidth := int(s.encoder.width)
	screenHeight := int(s.encoder.height)

	// Try to get actual screen bounds, but use encoder dimensions if it fails
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

	// If bounds is empty, create bounds from encoder dimensions
	if bounds.Empty() {
		bounds = image.Rect(0, 0, screenWidth, screenHeight)
	}

	// Get encoder's aligned dimensions
	encoderWidth := int(s.encoder.width)
	encoderHeight := int(s.encoder.height)

	// Check if we need to pad (encoder dimensions might be larger due to alignment)
	needsPadding := encoderWidth != screenWidth || encoderHeight != screenHeight

	log.Printf("Starting screen capture: screen=%dx%d, encoder=%dx%d, padding=%v @ %d fps",
		screenWidth, screenHeight, encoderWidth, encoderHeight, needsPadding, fps)

	for {
		select {
		case <-s.ctx.Done():
			log.Printf("Screen capture stopped for session %s", s.ID)
			return
		case <-ticker.C:
			if !s.Active {
				return
			}

			// Capture screen
			img, err := screenshot.CaptureRect(bounds)
			if err != nil {
				log.Printf("Failed to capture screen: %v", err)
				continue
			}

			// Convert to YCbCr (with padding if needed for encoder alignment)
			var ycbcr *image.YCbCr
			if needsPadding {
				ycbcr = rgbaToYCbCrPadded(img, encoderWidth, encoderHeight)
			} else {
				ycbcr = rgbaToYCbCr(img)
			}

			// Encode to H.264
			data, err := s.encoder.encode(ycbcr)
			if err != nil {
				log.Printf("Failed to encode frame: %v", err)
				continue
			}

			if data == nil {
				continue // Frame was skipped
			}

			// Write to WebRTC track
			if err := s.VideoTrack.WriteSample(media.Sample{
				Data:     data,
				Duration: frameDuration,
			}); err != nil {
				log.Printf("Failed to write sample: %v", err)
			}
		}
	}
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

	if s.encoder != nil {
		s.encoder.close()
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
