//go:build windows

package audio

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sentinel/agent/internal/winptr"
)

// WASAPI GUIDs
var (
	CLSID_MMDeviceEnumerator = GUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	IID_IMMDeviceEnumerator  = GUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	IID_IAudioClient         = GUID{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	IID_IAudioCaptureClient  = GUID{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	IID_IMMDevice            = GUID{0xD666063F, 0x1587, 0x4E43, [8]byte{0x81, 0xF1, 0xB9, 0x48, 0xE8, 0x07, 0x36, 0x3F}}

	PKEY_Device_FriendlyName = PROPERTYKEY{
		FmtID: GUID{0xa45c254e, 0xdf1c, 0x4efd, [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0}},
		PID:   14,
	}
)

// GUID structure
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// PROPERTYKEY structure
type PROPERTYKEY struct {
	FmtID GUID
	PID   uint32
}

// WAVEFORMATEX structure
type WAVEFORMATEX struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	ExtraSize      uint16
}

// WAVEFORMATEXTENSIBLE structure
type WAVEFORMATEXTENSIBLE struct {
	Format        WAVEFORMATEX
	Samples       uint16
	ChannelMask   uint32
	SubFormat     GUID
}

// Constants
const (
	WAVE_FORMAT_PCM        = 1
	WAVE_FORMAT_IEEE_FLOAT = 3
	WAVE_FORMAT_EXTENSIBLE = 0xFFFE

	AUDCLNT_SHAREMODE_SHARED    = 0
	AUDCLNT_SHAREMODE_EXCLUSIVE = 1

	AUDCLNT_STREAMFLAGS_LOOPBACK       = 0x00020000
	AUDCLNT_STREAMFLAGS_EVENTCALLBACK  = 0x00040000
	AUDCLNT_STREAMFLAGS_NOPERSIST      = 0x00080000

	DEVICE_STATE_ACTIVE = 0x00000001

	eRender  = 0
	eCapture = 1
	eAll     = 2

	eConsole      = 0
	eMultimedia   = 1
	eCommunications = 2

	STGM_READ = 0x00000000
)

// COM vtable indices
const (
	// IUnknown
	vtQueryInterface = 0
	vtAddRef         = 1
	vtRelease        = 2

	// IMMDeviceEnumerator
	vtEnumAudioEndpoints    = 3
	vtGetDefaultAudioEndpoint = 4
	vtGetDevice             = 5

	// IMMDeviceCollection
	vtGetCount = 3
	vtItem     = 4

	// IMMDevice
	vtActivate         = 3
	vtOpenPropertyStore = 4
	vtGetId            = 5
	vtGetState         = 6

	// IAudioClient
	vtInitialize         = 3
	vtGetBufferSize      = 4
	vtGetStreamLatency   = 5
	vtGetCurrentPadding  = 6
	vtIsFormatSupported  = 7
	vtGetMixFormat       = 8
	vtGetDevicePeriod    = 9
	vtStart              = 10
	vtStop               = 11
	vtReset              = 12
	vtSetEventHandle     = 13
	vtGetService         = 14

	// IAudioCaptureClient
	vtGetBuffer      = 3
	vtReleaseBuffer  = 4
	vtGetNextPacketSize = 5

	// IPropertyStore
	vtGetCount_PS = 3
	vtGetAt       = 4
	vtGetValue    = 5
)

// DLL handles
var (
	ole32                  = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	procCoUninitialize     = ole32.NewProc("CoUninitialize")
	procCoCreateInstance   = ole32.NewProc("CoCreateInstance")
	procPropVariantClear   = ole32.NewProc("PropVariantClear")
)

// WASAPICapture implements IAudioCapture using Windows WASAPI
type WASAPICapture struct {
	deviceEnumerator uintptr // IMMDeviceEnumerator
	device           uintptr // IMMDevice
	audioClient      uintptr // IAudioClient
	captureClient    uintptr // IAudioCaptureClient

	format       AudioFormat
	deviceID     string
	deviceName   string
	isLoopback   bool
	volume       float64

	callback     func(samples *AudioSamples)
	capturing    bool
	stopChan     chan struct{}
	bufferFrames uint32

	mu sync.RWMutex
	initialized bool
	comInit     bool
}

// NewWASAPICapture creates a new WASAPI audio capture
func NewWASAPICapture() *WASAPICapture {
	return &WASAPICapture{
		volume:   1.0,
		stopChan: make(chan struct{}),
	}
}

// Initialize sets up audio capture
func (w *WASAPICapture) Initialize(deviceID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Initialize COM
	hr, _, _ := procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if hr != 0 && hr != 1 { // S_OK or S_FALSE (already initialized)
		return fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
	}
	w.comInit = true

	// Create device enumerator
	var enumerator uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&CLSID_MMDeviceEnumerator)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&IID_IMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 {
		return fmt.Errorf("CoCreateInstance failed: 0x%x", hr)
	}
	w.deviceEnumerator = enumerator

	// Get device
	var device uintptr
	if deviceID == "" {
		// Get default render device (for loopback capture)
		hr = w.callMethod(enumerator, vtGetDefaultAudioEndpoint,
			eRender, eConsole, uintptr(unsafe.Pointer(&device)))
		w.isLoopback = true
	} else {
		// Get specific device
		deviceIDPtr, _ := syscall.UTF16PtrFromString(deviceID)
		hr = w.callMethod(enumerator, vtGetDevice,
			uintptr(unsafe.Pointer(deviceIDPtr)), uintptr(unsafe.Pointer(&device)))
	}
	if hr != 0 {
		return fmt.Errorf("GetDevice failed: 0x%x", hr)
	}
	w.device = device
	w.deviceID = deviceID

	// Get device name
	w.deviceName = w.getDeviceName(device)

	// Activate audio client
	var audioClient uintptr
	hr = w.callMethod(device, vtActivate,
		uintptr(unsafe.Pointer(&IID_IAudioClient)),
		1, // CLSCTX_ALL
		0,
		uintptr(unsafe.Pointer(&audioClient)),
	)
	if hr != 0 {
		return fmt.Errorf("Activate IAudioClient failed: 0x%x", hr)
	}
	w.audioClient = audioClient

	// Get mix format
	var pFormat uintptr
	hr = w.callMethod(audioClient, vtGetMixFormat, uintptr(unsafe.Pointer(&pFormat)))
	if hr != 0 {
		return fmt.Errorf("GetMixFormat failed: 0x%x", hr)
	}

	wfx := (*WAVEFORMATEX)(winptr.FromUintptr(pFormat))
	w.format = AudioFormat{
		SampleRate: int(wfx.SamplesPerSec),
		Channels:   int(wfx.Channels),
		BitDepth:   int(wfx.BitsPerSample),
	}

	if wfx.FormatTag == WAVE_FORMAT_IEEE_FLOAT ||
	   (wfx.FormatTag == WAVE_FORMAT_EXTENSIBLE && wfx.BitsPerSample == 32) {
		w.format.SampleFormat = SampleFormatF32
	} else {
		w.format.SampleFormat = SampleFormatS16
	}

	log.Printf("[WASAPI] Format: %d Hz, %d channels, %d bits",
		w.format.SampleRate, w.format.Channels, w.format.BitDepth)

	// Initialize audio client
	var streamFlags uint32 = AUDCLNT_STREAMFLAGS_LOOPBACK
	if !w.isLoopback {
		streamFlags = 0
	}

	// 100ns units, 200ms buffer
	bufferDuration := int64(200 * 10000) // 200ms in 100ns units

	hr = w.callMethod(audioClient, vtInitialize,
		AUDCLNT_SHAREMODE_SHARED,
		uintptr(streamFlags),
		uintptr(bufferDuration),
		0, // periodicity (0 = default)
		pFormat,
		0, // session GUID
	)
	if hr != 0 {
		return fmt.Errorf("Initialize audio client failed: 0x%x", hr)
	}

	// Get buffer size
	var bufferFrames uint32
	hr = w.callMethod(audioClient, vtGetBufferSize, uintptr(unsafe.Pointer(&bufferFrames)))
	if hr != 0 {
		return fmt.Errorf("GetBufferSize failed: 0x%x", hr)
	}
	w.bufferFrames = bufferFrames

	// Get capture client
	var captureClient uintptr
	hr = w.callMethod(audioClient, vtGetService,
		uintptr(unsafe.Pointer(&IID_IAudioCaptureClient)),
		uintptr(unsafe.Pointer(&captureClient)),
	)
	if hr != 0 {
		return fmt.Errorf("GetService IAudioCaptureClient failed: 0x%x", hr)
	}
	w.captureClient = captureClient

	w.initialized = true
	log.Printf("[WASAPI] Initialized: %s, buffer: %d frames", w.deviceName, w.bufferFrames)

	return nil
}

// Start begins audio capture
func (w *WASAPICapture) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized {
		return ErrNotInitialized
	}
	if w.capturing {
		return nil
	}

	// Start audio client
	hr := w.callMethod(w.audioClient, vtStart)
	if hr != 0 {
		return fmt.Errorf("Start audio client failed: 0x%x", hr)
	}

	w.capturing = true
	w.stopChan = make(chan struct{})

	// Start capture goroutine
	go w.captureLoop()

	log.Printf("[WASAPI] Capture started")
	return nil
}

// Stop stops audio capture
func (w *WASAPICapture) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.capturing {
		return nil
	}

	close(w.stopChan)
	w.capturing = false

	// Stop audio client
	hr := w.callMethod(w.audioClient, vtStop)
	if hr != 0 {
		log.Printf("[WASAPI] Stop failed: 0x%x", hr)
	}

	log.Printf("[WASAPI] Capture stopped")
	return nil
}

// captureLoop runs the audio capture loop
func (w *WASAPICapture) captureLoop() {
	// Capture interval based on buffer size
	interval := time.Duration(w.bufferFrames) * time.Second / time.Duration(w.format.SampleRate) / 4
	if interval < 5*time.Millisecond {
		interval = 5 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.readPackets()
		}
	}
}

// readPackets reads available audio packets
func (w *WASAPICapture) readPackets() {
	w.mu.RLock()
	if !w.capturing || w.captureClient == 0 {
		w.mu.RUnlock()
		return
	}
	captureClient := w.captureClient
	callback := w.callback
	format := w.format
	volume := w.volume
	w.mu.RUnlock()

	for {
		// Get next packet size
		var packetLength uint32
		hr := w.callMethod(captureClient, vtGetNextPacketSize, uintptr(unsafe.Pointer(&packetLength)))
		if hr != 0 || packetLength == 0 {
			break
		}

		// Get buffer
		var data uintptr
		var numFrames uint32
		var flags uint32
		var devicePosition, qpcPosition uint64

		hr = w.callMethod(captureClient, vtGetBuffer,
			uintptr(unsafe.Pointer(&data)),
			uintptr(unsafe.Pointer(&numFrames)),
			uintptr(unsafe.Pointer(&flags)),
			uintptr(unsafe.Pointer(&devicePosition)),
			uintptr(unsafe.Pointer(&qpcPosition)),
		)
		if hr != 0 {
			break
		}

		// Process audio data
		if callback != nil && numFrames > 0 {
			bytesPerSample := format.BitDepth / 8
			bytesPerFrame := bytesPerSample * format.Channels
			dataSize := int(numFrames) * bytesPerFrame

			// Copy data
			audioData := make([]byte, dataSize)
			copy(audioData, unsafe.Slice((*byte)(winptr.FromUintptr(data)), dataSize))

			// Apply volume
			if volume < 1.0 {
				applyVolume(audioData, format.SampleFormat, volume)
			}

			samples := &AudioSamples{
				Data:       audioData,
				Samples:    int(numFrames),
				Channels:   format.Channels,
				SampleRate: format.SampleRate,
				Format:     format.SampleFormat,
				Timestamp:  time.Now(),
				Duration:   time.Duration(numFrames) * time.Second / time.Duration(format.SampleRate),
			}

			callback(samples)
		}

		// Release buffer
		w.callMethod(captureClient, vtReleaseBuffer, uintptr(numFrames))
	}
}

// SetVolume sets the capture volume
func (w *WASAPICapture) SetVolume(level float64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	w.volume = level
	return nil
}

// GetVolume returns the current capture volume
func (w *WASAPICapture) GetVolume() float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.volume
}

// GetDevices returns available audio devices
func (w *WASAPICapture) GetDevices() ([]AudioDevice, error) {
	w.mu.RLock()
	enumerator := w.deviceEnumerator
	w.mu.RUnlock()

	if enumerator == 0 {
		return nil, ErrNotInitialized
	}

	var devices []AudioDevice

	// Enumerate render devices (for loopback)
	var collection uintptr
	hr := w.callMethod(enumerator, vtEnumAudioEndpoints,
		eRender, DEVICE_STATE_ACTIVE, uintptr(unsafe.Pointer(&collection)))
	if hr != 0 {
		return nil, fmt.Errorf("EnumAudioEndpoints failed: 0x%x", hr)
	}
	defer w.release(collection)

	var count uint32
	hr = w.callMethod(collection, vtGetCount, uintptr(unsafe.Pointer(&count)))
	if hr != 0 {
		return nil, fmt.Errorf("GetCount failed: 0x%x", hr)
	}

	for i := uint32(0); i < count; i++ {
		var device uintptr
		hr = w.callMethod(collection, vtItem, uintptr(i), uintptr(unsafe.Pointer(&device)))
		if hr != 0 {
			continue
		}

		// Get device ID
		var deviceID *uint16
		hr = w.callMethod(device, vtGetId, uintptr(unsafe.Pointer(&deviceID)))
		if hr == 0 && deviceID != nil {
			id := syscall.UTF16ToString((*[256]uint16)(unsafe.Pointer(deviceID))[:])

			devices = append(devices, AudioDevice{
				ID:         id,
				Name:       w.getDeviceName(device),
				IsDefault:  i == 0, // First is usually default
				IsLoopback: true,
				SampleRate: 48000, // Will be determined on activation
				Channels:   2,
				BitDepth:   32,
			})
		}

		w.release(device)
	}

	return devices, nil
}

// GetFormat returns the current audio format
func (w *WASAPICapture) GetFormat() AudioFormat {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.format
}

// OnSamples sets the callback for received audio samples
func (w *WASAPICapture) OnSamples(callback func(samples *AudioSamples)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callback = callback
}

// IsCapturing returns true if capture is active
func (w *WASAPICapture) IsCapturing() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.capturing
}

// Release frees all resources
func (w *WASAPICapture) Release() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.capturing {
		close(w.stopChan)
		w.capturing = false
	}

	if w.captureClient != 0 {
		w.release(w.captureClient)
		w.captureClient = 0
	}
	if w.audioClient != 0 {
		w.callMethod(w.audioClient, vtStop)
		w.callMethod(w.audioClient, vtReset)
		w.release(w.audioClient)
		w.audioClient = 0
	}
	if w.device != 0 {
		w.release(w.device)
		w.device = 0
	}
	if w.deviceEnumerator != 0 {
		w.release(w.deviceEnumerator)
		w.deviceEnumerator = 0
	}
	if w.comInit {
		procCoUninitialize.Call()
		w.comInit = false
	}

	w.initialized = false
	log.Printf("[WASAPI] Released")
}

// Helper methods

func (w *WASAPICapture) getDeviceName(device uintptr) string {
	var propStore uintptr
	hr := w.callMethod(device, vtOpenPropertyStore, STGM_READ, uintptr(unsafe.Pointer(&propStore)))
	if hr != 0 {
		return "Unknown"
	}
	defer w.release(propStore)

	var propVar [24]byte // PROPVARIANT is 24 bytes on 64-bit
	hr = w.callMethod(propStore, vtGetValue,
		uintptr(unsafe.Pointer(&PKEY_Device_FriendlyName)),
		uintptr(unsafe.Pointer(&propVar[0])))
	if hr != 0 {
		return "Unknown"
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&propVar[0])))

	// PROPVARIANT with VT_LPWSTR has pointer at offset 8
	ptr := *(*uintptr)(unsafe.Pointer(&propVar[8]))
	if ptr == 0 {
		return "Unknown"
	}

	return syscall.UTF16ToString(unsafe.Slice((*uint16)(winptr.FromUintptr(ptr)), 256))
}

func (w *WASAPICapture) callMethod(obj uintptr, methodIndex int, args ...uintptr) uintptr {
	vtable := *(*uintptr)(winptr.FromUintptr(obj))
	method := *(*uintptr)(winptr.Add(vtable, uintptr(methodIndex)*unsafe.Sizeof(uintptr(0))))
	allArgs := make([]uintptr, 1+len(args))
	allArgs[0] = obj
	copy(allArgs[1:], args)
	ret, _, _ := syscall.SyscallN(method, allArgs...)
	return ret
}

func (w *WASAPICapture) release(obj uintptr) {
	if obj == 0 {
		return
	}
	w.callMethod(obj, vtRelease)
}

// applyVolume applies volume adjustment to audio samples
func applyVolume(data []byte, format SampleFormat, volume float64) {
	switch format {
	case SampleFormatF32:
		samples := unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), len(data)/4)
		for i := range samples {
			samples[i] *= float32(volume)
		}
	case SampleFormatS16:
		samples := unsafe.Slice((*int16)(unsafe.Pointer(&data[0])), len(data)/2)
		for i := range samples {
			samples[i] = int16(float64(samples[i]) * volume)
		}
	}
}
