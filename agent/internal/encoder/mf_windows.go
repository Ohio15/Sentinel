//go:build windows

package encoder

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"syscall"
	"unsafe"

	"github.com/sentinel/agent/internal/winptr"
)

// Media Foundation GUIDs
var (
	MFMediaType_Video        = GUID{0x73646976, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	MFVideoFormat_H264       = GUID{0x34363248, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	MFVideoFormat_NV12       = GUID{0x3231564E, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	MFVideoFormat_ARGB32     = GUID{0x00000015, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	MFVideoFormat_RGB32      = GUID{0x00000016, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}

	MF_MT_MAJOR_TYPE                  = GUID{0x48eba18e, 0xf8c9, 0x4687, [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}}
	MF_MT_SUBTYPE                     = GUID{0xf7e34c9a, 0x42e8, 0x4714, [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}}
	MF_MT_FRAME_SIZE                  = GUID{0x1652c33d, 0xd6b2, 0x4012, [8]byte{0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d}}
	MF_MT_FRAME_RATE                  = GUID{0xc459a2e8, 0x3d2c, 0x4e44, [8]byte{0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0}}
	MF_MT_AVG_BITRATE                 = GUID{0x20332624, 0xfb0d, 0x4d9e, [8]byte{0xbd, 0x0d, 0xcb, 0xf6, 0x78, 0x6c, 0x10, 0x2e}}
	MF_MT_INTERLACE_MODE              = GUID{0xe2724bb8, 0xe676, 0x4806, [8]byte{0xb4, 0xb2, 0xa8, 0xd6, 0xef, 0xb4, 0x4c, 0xcd}}
	MF_MT_MPEG2_PROFILE               = GUID{0xad76a80b, 0x2d5c, 0x4e0b, [8]byte{0xb3, 0x75, 0x64, 0xe5, 0x20, 0x13, 0x70, 0x36}}
	MF_MT_MPEG2_LEVEL                 = GUID{0x96f66574, 0x11c5, 0x4015, [8]byte{0x86, 0x66, 0xbf, 0xf5, 0x16, 0x43, 0x6d, 0xa7}}

	MFT_CATEGORY_VIDEO_ENCODER        = GUID{0xf79eac7d, 0xe545, 0x4387, [8]byte{0xbd, 0xee, 0xd6, 0x47, 0xd7, 0xbd, 0xe4, 0x2a}}

	CODECAPI_AVEncCommonRateControlMode = GUID{0x1c0608e9, 0x370c, 0x4710, [8]byte{0x8a, 0x58, 0xcb, 0x61, 0x81, 0xc4, 0x24, 0x23}}
	CODECAPI_AVEncCommonQuality         = GUID{0xfcbf57a3, 0x7ea5, 0x4b0c, [8]byte{0x96, 0x44, 0x69, 0xb4, 0x0c, 0x39, 0xc3, 0x91}}
	CODECAPI_AVLowLatencyMode           = GUID{0x9c27891a, 0xed7a, 0x40e1, [8]byte{0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee}}
	CODECAPI_AVEncMPVGOPSize            = GUID{0x95f31b26, 0x95a4, 0x41aa, [8]byte{0x93, 0x03, 0x24, 0x6a, 0x7f, 0xc6, 0xee, 0xf1}}
	CODECAPI_AVEncVideoForceKeyFrame    = GUID{0x398c1b98, 0x8353, 0x475a, [8]byte{0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45}}
	CODECAPI_AVEncCommonMeanBitRate     = GUID{0xf7222374, 0x2144, 0x4815, [8]byte{0xb5, 0x50, 0xa3, 0x7f, 0x8e, 0x12, 0xee, 0x52}}

	IID_ICodecAPI = GUID{0x901db4c7, 0x31ce, 0x41a2, [8]byte{0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda}}
)

// GUID structure
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// Media Foundation constants
const (
	MF_VERSION = 0x00020070

	MFT_ENUM_FLAG_SYNCMFT       = 0x00000001
	MFT_ENUM_FLAG_ASYNCMFT      = 0x00000002
	MFT_ENUM_FLAG_HARDWARE      = 0x00000004
	MFT_ENUM_FLAG_SORTANDFILTER = 0x00000040

	MF_E_TRANSFORM_NEED_MORE_INPUT = 0xc00d6d72
	MF_E_NOTACCEPTING              = 0xc00d36b5

	MFVideoInterlace_Progressive = 2

	eAVEncCommonRateControlMode_CBR      = 1
	eAVEncCommonRateControlMode_VBR      = 2
	eAVEncCommonRateControlMode_Quality  = 3

	// H.264 profiles
	eAVEncH264VProfile_Base     = 66
	eAVEncH264VProfile_Main     = 77
	eAVEncH264VProfile_High     = 100

	// H.264 levels
	eAVEncH264VLevel4   = 40
	eAVEncH264VLevel4_1 = 41
	eAVEncH264VLevel5   = 50
	eAVEncH264VLevel5_1 = 51
)

// PROPVARIANT for codec API
type PROPVARIANT struct {
	Vt       uint16
	Reserved [6]byte
	Val      int64
}

const (
	VT_UI4 = 19
	VT_I4  = 3
	VT_BOOL = 11
)

// DLL and function pointers
var (
	mfplat                    = syscall.NewLazyDLL("mfplat.dll")
	mf                        = syscall.NewLazyDLL("mf.dll")
	procMFStartup             = mfplat.NewProc("MFStartup")
	procMFShutdown            = mfplat.NewProc("MFShutdown")
	procMFCreateMediaType     = mfplat.NewProc("MFCreateMediaType")
	procMFCreateSample        = mfplat.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer  = mfplat.NewProc("MFCreateMemoryBuffer")
	procMFTEnumEx             = mf.NewProc("MFTEnumEx")
)

// COM VTable indices
const (
	// IUnknown
	vtQueryInterface = 0
	vtAddRef         = 1
	vtRelease        = 2

	// IMFTransform
	vtGetStreamLimits             = 3
	vtGetStreamCount              = 4
	vtGetStreamIDs                = 5
	vtGetInputStreamInfo          = 6
	vtGetOutputStreamInfo         = 7
	vtGetAttributes               = 8
	vtGetInputStreamAttributes    = 9
	vtGetOutputStreamAttributes   = 10
	vtDeleteInputStream           = 11
	vtAddInputStreams             = 12
	vtGetInputAvailableType       = 13
	vtGetOutputAvailableType      = 14
	vtSetInputType                = 15
	vtSetOutputType               = 16
	vtGetInputCurrentType         = 17
	vtGetOutputCurrentType        = 18
	vtGetInputStatus              = 19
	vtGetOutputStatus             = 20
	vtSetOutputBounds             = 21
	vtProcessEvent                = 22
	vtProcessMessage              = 23
	vtProcessInput                = 24
	vtProcessOutput               = 25

	// IMFMediaType
	vtGetMajorType      = 17
	vtIsCompressedFormat = 18
	vtIsEqual           = 19
	vtGetRepresentation = 20
	vtFreeRepresentation = 21
	vtSetGUID           = 14
	vtSetUINT32         = 15
	vtSetUINT64         = 16

	// IMFSample
	vtGetSampleFlags    = 3
	vtSetSampleFlags    = 4
	vtGetSampleTime     = 5
	vtSetSampleTime     = 6
	vtGetSampleDuration = 7
	vtSetSampleDuration = 8
	vtGetBufferCount    = 9
	vtGetBufferByIndex  = 10
	vtConvertToContiguousBuffer = 11
	vtAddBuffer         = 12
	vtRemoveBufferByIndex = 13
	vtRemoveAllBuffers  = 14
	vtGetTotalLength    = 15
	vtCopyToBuffer      = 16

	// IMFMediaBuffer
	vtLock              = 3
	vtUnlock            = 4
	vtGetCurrentLength  = 5
	vtSetCurrentLength  = 6
	vtGetMaxLength      = 7

	// ICodecAPI
	vtIsSupported       = 3
	vtIsModifiable      = 4
	vtGetParameterRange = 5
	vtGetParameterValues = 6
	vtGetDefaultValue   = 7
	vtGetValue          = 8
	vtSetValue          = 9
)

// MFT messages
const (
	MFT_MESSAGE_COMMAND_FLUSH          = 0
	MFT_MESSAGE_COMMAND_DRAIN          = 1
	MFT_MESSAGE_SET_D3D_MANAGER        = 2
	MFT_MESSAGE_DROP_SAMPLES           = 3
	MFT_MESSAGE_NOTIFY_BEGIN_STREAMING = 0x10000000
	MFT_MESSAGE_NOTIFY_END_STREAMING   = 0x10000001
	MFT_MESSAGE_NOTIFY_END_OF_STREAM   = 0x10000002
	MFT_MESSAGE_NOTIFY_START_OF_STREAM = 0x10000003
)

// MFT_OUTPUT_DATA_BUFFER structure
type MFT_OUTPUT_DATA_BUFFER struct {
	StreamID uint32
	Sample   uintptr
	Status   uint32
	Events   uintptr
}

// EncoderConfig specifies encoding parameters
type EncoderConfig struct {
	Width         int
	Height        int
	FrameRate     int
	Bitrate       int
	Profile       string // "baseline", "main", "high"
	LowLatency    bool
	GOPSize       int // Keyframe interval in frames
}

// H264Encoder wraps Media Foundation encoder
type H264Encoder struct {
	transform    uintptr // IMFTransform
	codecAPI     uintptr // ICodecAPI
	inputType    uintptr // IMFMediaType
	outputType   uintptr // IMFMediaType

	config       EncoderConfig
	isHardware   bool
	frameIndex   int64
	sps          []byte
	pps          []byte
	extradata    []byte

	mu           sync.Mutex
	initialized  bool
	mfStarted    bool
}

var mfInitOnce sync.Once
var mfInitErr error

func initMF() error {
	mfInitOnce.Do(func() {
		hr, _, _ := procMFStartup.Call(MF_VERSION, 0)
		if hr != 0 {
			mfInitErr = fmt.Errorf("MFStartup failed: 0x%x", hr)
		}
	})
	return mfInitErr
}

// NewH264Encoder creates encoder, preferring hardware
func NewH264Encoder(config EncoderConfig) (*H264Encoder, error) {
	if err := initMF(); err != nil {
		return nil, err
	}

	// Set defaults
	if config.FrameRate <= 0 {
		config.FrameRate = 30
	}
	if config.Bitrate <= 0 {
		config.Bitrate = 4000000 // 4 Mbps
	}
	if config.GOPSize <= 0 {
		config.GOPSize = config.FrameRate * 2 // 2 seconds
	}
	if config.Profile == "" {
		config.Profile = "main"
	}

	e := &H264Encoder{
		config:    config,
		mfStarted: true,
	}

	// Try hardware encoder first
	if err := e.initEncoder(true); err != nil {
		// Fall back to software
		if err2 := e.initEncoder(false); err2 != nil {
			return nil, fmt.Errorf("no encoder available: hardware=%v, software=%v", err, err2)
		}
	}

	return e, nil
}

func (e *H264Encoder) initEncoder(preferHardware bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Enumerate H.264 encoders
	var flags uint32 = MFT_ENUM_FLAG_SYNCMFT | MFT_ENUM_FLAG_SORTANDFILTER
	if preferHardware {
		flags |= MFT_ENUM_FLAG_HARDWARE
	}

	var activateCount uint32
	var activateArray uintptr

	inputType := MFVideoFormat_NV12 // Most encoders prefer NV12

	hr, _, _ := procMFTEnumEx.Call(
		uintptr(unsafe.Pointer(&MFT_CATEGORY_VIDEO_ENCODER)),
		uintptr(flags),
		0, // No input type restriction
		uintptr(unsafe.Pointer(&struct {
			guidMajorType GUID
			guidSubtype   GUID
		}{MFMediaType_Video, MFVideoFormat_H264})),
		uintptr(unsafe.Pointer(&activateArray)),
		uintptr(unsafe.Pointer(&activateCount)),
	)
	_ = inputType

	if hr != 0 {
		return fmt.Errorf("MFTEnumEx failed: 0x%x", hr)
	}

	if activateCount == 0 {
		return errors.New("no H.264 encoder found")
	}

	// Get first activate
	activates := unsafe.Slice((*uintptr)(winptr.FromUintptr(activateArray)), activateCount)
	activate := activates[0]

	// Release unused activates
	for i := uint32(1); i < activateCount; i++ {
		e.release(activates[i])
	}

	// Activate the MFT
	var transform uintptr
	hr = e.callMethod(activate, 17, // ActivateObject
		uintptr(unsafe.Pointer(&GUID{0xbf94c121, 0x5b05, 0x4e6f, [8]byte{0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d}})), // IID_IMFTransform
		uintptr(unsafe.Pointer(&transform)),
	)
	e.release(activate)

	if hr != 0 {
		return fmt.Errorf("ActivateObject failed: 0x%x", hr)
	}

	e.transform = transform
	e.isHardware = preferHardware

	// Query for ICodecAPI to set encoder properties
	e.queryInterface(transform, &IID_ICodecAPI, &e.codecAPI)

	// Configure encoder
	if err := e.configureEncoder(); err != nil {
		e.Release()
		return err
	}

	e.initialized = true
	return nil
}

func (e *H264Encoder) configureEncoder() error {
	// Create and set output type (H.264)
	var outputType uintptr
	hr, _, _ := procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&outputType)))
	if hr != 0 {
		return fmt.Errorf("MFCreateMediaType (output) failed: 0x%x", hr)
	}
	e.outputType = outputType

	// Set output attributes
	e.setGUID(outputType, &MF_MT_MAJOR_TYPE, &MFMediaType_Video)
	e.setGUID(outputType, &MF_MT_SUBTYPE, &MFVideoFormat_H264)
	e.setUINT64(outputType, &MF_MT_FRAME_SIZE, packSize(e.config.Width, e.config.Height))
	e.setUINT64(outputType, &MF_MT_FRAME_RATE, packRatio(e.config.FrameRate, 1))
	e.setUINT32(outputType, &MF_MT_AVG_BITRATE, uint32(e.config.Bitrate))
	e.setUINT32(outputType, &MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive)

	// Set profile
	profile := eAVEncH264VProfile_Main
	switch e.config.Profile {
	case "baseline":
		profile = eAVEncH264VProfile_Base
	case "high":
		profile = eAVEncH264VProfile_High
	}
	e.setUINT32(outputType, &MF_MT_MPEG2_PROFILE, uint32(profile))
	e.setUINT32(outputType, &MF_MT_MPEG2_LEVEL, eAVEncH264VLevel4_1)

	// Set output type on transform
	hr = e.callMethod(e.transform, vtSetOutputType, 0, outputType, 0)
	if hr != 0 {
		return fmt.Errorf("SetOutputType failed: 0x%x", hr)
	}

	// Create and set input type (NV12)
	var inputType uintptr
	hr, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&inputType)))
	if hr != 0 {
		return fmt.Errorf("MFCreateMediaType (input) failed: 0x%x", hr)
	}
	e.inputType = inputType

	// Set input attributes
	e.setGUID(inputType, &MF_MT_MAJOR_TYPE, &MFMediaType_Video)
	e.setGUID(inputType, &MF_MT_SUBTYPE, &MFVideoFormat_NV12)
	e.setUINT64(inputType, &MF_MT_FRAME_SIZE, packSize(e.config.Width, e.config.Height))
	e.setUINT64(inputType, &MF_MT_FRAME_RATE, packRatio(e.config.FrameRate, 1))
	e.setUINT32(inputType, &MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive)

	// Set input type on transform
	hr = e.callMethod(e.transform, vtSetInputType, 0, inputType, 0)
	if hr != 0 {
		return fmt.Errorf("SetInputType failed: 0x%x", hr)
	}

	// Configure via CodecAPI
	if e.codecAPI != 0 {
		// Enable low latency mode
		if e.config.LowLatency {
			e.setCodecValue(&CODECAPI_AVLowLatencyMode, VT_BOOL, 1)
		}

		// Set CBR rate control
		e.setCodecValue(&CODECAPI_AVEncCommonRateControlMode, VT_UI4, eAVEncCommonRateControlMode_CBR)

		// Set GOP size
		e.setCodecValue(&CODECAPI_AVEncMPVGOPSize, VT_UI4, int64(e.config.GOPSize))
	}

	// Notify encoder we're starting
	e.callMethod(e.transform, vtProcessMessage, MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)
	e.callMethod(e.transform, vtProcessMessage, MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0)

	return nil
}

// Encode takes NV12 frame data and returns H.264 NAL units
func (e *H264Encoder) Encode(nv12Data []byte, forceKeyframe bool) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, errors.New("encoder not initialized")
	}

	// Force keyframe if requested
	if forceKeyframe && e.codecAPI != 0 {
		e.setCodecValue(&CODECAPI_AVEncVideoForceKeyFrame, VT_UI4, 1)
	}

	// Create input sample
	sample, buffer, err := e.createSample(nv12Data)
	if err != nil {
		return nil, err
	}
	defer e.release(sample)
	defer e.release(buffer)

	// Set sample time
	timestamp := e.frameIndex * (10000000 / int64(e.config.FrameRate)) // 100ns units
	e.callMethod(sample, vtSetSampleTime+3, uintptr(timestamp)) // Offset for IMFSample
	e.callMethod(sample, vtSetSampleDuration+3, uintptr(10000000/int64(e.config.FrameRate)))

	// Process input
	hr := e.callMethod(e.transform, vtProcessInput, 0, sample, 0)
	if hr != 0 && hr != MF_E_NOTACCEPTING {
		return nil, fmt.Errorf("ProcessInput failed: 0x%x", hr)
	}

	e.frameIndex++

	// Get output
	return e.getOutput()
}

// EncodeBGRA converts BGRA to NV12 and encodes
func (e *H264Encoder) EncodeBGRA(bgraData []byte, width, height, stride int, forceKeyframe bool) ([]byte, error) {
	// Convert BGRA to NV12
	nv12 := e.bgraToNV12(bgraData, width, height, stride)
	return e.Encode(nv12, forceKeyframe)
}

func (e *H264Encoder) bgraToNV12(bgra []byte, width, height, stride int) []byte {
	// NV12 format: Y plane followed by interleaved UV plane
	ySize := width * height
	uvSize := (width / 2) * (height / 2) * 2
	nv12 := make([]byte, ySize+uvSize)

	// Y plane
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcIdx := y*stride + x*4
			b := int(bgra[srcIdx])
			g := int(bgra[srcIdx+1])
			r := int(bgra[srcIdx+2])

			// ITU-R BT.601 Y conversion
			yVal := ((66*r + 129*g + 25*b + 128) >> 8) + 16
			if yVal > 255 {
				yVal = 255
			} else if yVal < 0 {
				yVal = 0
			}
			nv12[y*width+x] = byte(yVal)
		}
	}

	// UV plane (subsampled 2x2)
	uvOffset := ySize
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x += 2 {
			// Average 2x2 block
			var rSum, gSum, bSum int
			for dy := 0; dy < 2 && y+dy < height; dy++ {
				for dx := 0; dx < 2 && x+dx < width; dx++ {
					srcIdx := (y+dy)*stride + (x+dx)*4
					bSum += int(bgra[srcIdx])
					gSum += int(bgra[srcIdx+1])
					rSum += int(bgra[srcIdx+2])
				}
			}
			r := rSum / 4
			g := gSum / 4
			b := bSum / 4

			// ITU-R BT.601 UV conversion
			u := ((-38*r - 74*g + 112*b + 128) >> 8) + 128
			v := ((112*r - 94*g - 18*b + 128) >> 8) + 128

			if u > 255 {
				u = 255
			} else if u < 0 {
				u = 0
			}
			if v > 255 {
				v = 255
			} else if v < 0 {
				v = 0
			}

			uvIdx := uvOffset + (y/2)*(width) + (x/2)*2
			nv12[uvIdx] = byte(u)
			nv12[uvIdx+1] = byte(v)
		}
	}

	return nv12
}

func (e *H264Encoder) createSample(data []byte) (uintptr, uintptr, error) {
	// Create buffer
	var buffer uintptr
	hr, _, _ := procMFCreateMemoryBuffer.Call(uintptr(len(data)), uintptr(unsafe.Pointer(&buffer)))
	if hr != 0 {
		return 0, 0, fmt.Errorf("MFCreateMemoryBuffer failed: 0x%x", hr)
	}

	// Lock buffer and copy data
	var bufPtr uintptr
	var maxLen, curLen uint32
	hr = e.callMethod(buffer, vtLock, uintptr(unsafe.Pointer(&bufPtr)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))
	if hr != 0 {
		e.release(buffer)
		return 0, 0, fmt.Errorf("Lock buffer failed: 0x%x", hr)
	}

	copy(unsafe.Slice((*byte)(winptr.FromUintptr(bufPtr)), len(data)), data)
	e.callMethod(buffer, vtUnlock)
	e.callMethod(buffer, vtSetCurrentLength, uintptr(len(data)))

	// Create sample
	var sample uintptr
	hr, _, _ = procMFCreateSample.Call(uintptr(unsafe.Pointer(&sample)))
	if hr != 0 {
		e.release(buffer)
		return 0, 0, fmt.Errorf("MFCreateSample failed: 0x%x", hr)
	}

	// Add buffer to sample
	hr = e.callMethod(sample, vtAddBuffer+3, buffer) // Offset for IMFSample
	if hr != 0 {
		e.release(sample)
		e.release(buffer)
		return 0, 0, fmt.Errorf("AddBuffer failed: 0x%x", hr)
	}

	return sample, buffer, nil
}

func (e *H264Encoder) getOutput() ([]byte, error) {
	// Allocate output buffer
	outputBufferSize := e.config.Width * e.config.Height * 2 // Generous size
	var outputBuffer uintptr
	hr, _, _ := procMFCreateMemoryBuffer.Call(uintptr(outputBufferSize), uintptr(unsafe.Pointer(&outputBuffer)))
	if hr != 0 {
		return nil, fmt.Errorf("MFCreateMemoryBuffer (output) failed: 0x%x", hr)
	}
	defer e.release(outputBuffer)

	// Create output sample
	var outputSample uintptr
	hr, _, _ = procMFCreateSample.Call(uintptr(unsafe.Pointer(&outputSample)))
	if hr != 0 {
		return nil, fmt.Errorf("MFCreateSample (output) failed: 0x%x", hr)
	}
	defer e.release(outputSample)

	hr = e.callMethod(outputSample, vtAddBuffer+3, outputBuffer)
	if hr != 0 {
		return nil, fmt.Errorf("AddBuffer (output) failed: 0x%x", hr)
	}

	// Process output
	outputData := MFT_OUTPUT_DATA_BUFFER{
		StreamID: 0,
		Sample:   outputSample,
		Status:   0,
		Events:   0,
	}

	var status uint32
	hr = e.callMethod(e.transform, vtProcessOutput, 0, 1, uintptr(unsafe.Pointer(&outputData)), uintptr(unsafe.Pointer(&status)))

	if hr == MF_E_TRANSFORM_NEED_MORE_INPUT {
		return nil, nil // No output yet
	}

	if hr != 0 {
		return nil, fmt.Errorf("ProcessOutput failed: 0x%x", hr)
	}

	// Get output data
	var bufPtr uintptr
	var maxLen, curLen uint32
	hr = e.callMethod(outputBuffer, vtLock, uintptr(unsafe.Pointer(&bufPtr)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))
	if hr != 0 {
		return nil, fmt.Errorf("Lock output buffer failed: 0x%x", hr)
	}

	result := make([]byte, curLen)
	copy(result, unsafe.Slice((*byte)(winptr.FromUintptr(bufPtr)), curLen))
	e.callMethod(outputBuffer, vtUnlock)

	return result, nil
}

// Flush drains the encoder
func (e *H264Encoder) Flush() ([][]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, nil
	}

	// Send drain message
	e.callMethod(e.transform, vtProcessMessage, MFT_MESSAGE_COMMAND_DRAIN, 0)

	// Collect remaining frames
	var frames [][]byte
	for {
		data, err := e.getOutput()
		if err != nil || data == nil {
			break
		}
		frames = append(frames, data)
	}

	return frames, nil
}

// IsHardware returns true if using hardware encoder
func (e *H264Encoder) IsHardware() bool {
	return e.isHardware
}

// GetConfig returns the encoder configuration
func (e *H264Encoder) GetConfig() EncoderConfig {
	return e.config
}

// SetBitrate dynamically adjusts the target bitrate via ICodecAPI
func (e *H264Encoder) SetBitrate(bps int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.codecAPI == 0 {
		return fmt.Errorf("CodecAPI not available")
	}

	pv := PROPVARIANT{Vt: VT_UI4, Val: int64(bps)}
	hr := e.callMethod(e.codecAPI, vtSetValue,
		uintptr(unsafe.Pointer(&CODECAPI_AVEncCommonMeanBitRate)),
		uintptr(unsafe.Pointer(&pv)),
	)
	if hr != 0 {
		return fmt.Errorf("ICodecAPI.SetValue(BitRate) failed: 0x%x", hr)
	}

	e.config.Bitrate = bps
	log.Printf("[MFEncoder] Bitrate set to %d via CodecAPI", bps)
	return nil
}

// Release frees all resources
func (e *H264Encoder) Release() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.transform != 0 {
		e.callMethod(e.transform, vtProcessMessage, MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0)
		e.callMethod(e.transform, vtProcessMessage, MFT_MESSAGE_NOTIFY_END_STREAMING, 0)
	}

	if e.codecAPI != 0 {
		e.release(e.codecAPI)
		e.codecAPI = 0
	}
	if e.inputType != 0 {
		e.release(e.inputType)
		e.inputType = 0
	}
	if e.outputType != 0 {
		e.release(e.outputType)
		e.outputType = 0
	}
	if e.transform != 0 {
		e.release(e.transform)
		e.transform = 0
	}

	e.initialized = false
}

// Helper methods
func (e *H264Encoder) setGUID(obj uintptr, key *GUID, value *GUID) {
	e.callMethod(obj, vtSetGUID, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(value)))
}

func (e *H264Encoder) setUINT32(obj uintptr, key *GUID, value uint32) {
	e.callMethod(obj, vtSetUINT32, uintptr(unsafe.Pointer(key)), uintptr(value))
}

func (e *H264Encoder) setUINT64(obj uintptr, key *GUID, value uint64) {
	e.callMethod(obj, vtSetUINT64, uintptr(unsafe.Pointer(key)), uintptr(value))
}

func (e *H264Encoder) setCodecValue(key *GUID, vt uint16, value int64) {
	if e.codecAPI == 0 {
		return
	}
	pv := PROPVARIANT{Vt: vt, Val: value}
	e.callMethod(e.codecAPI, vtSetValue, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&pv)))
}

func (e *H264Encoder) queryInterface(obj uintptr, iid *GUID, out *uintptr) uintptr {
	vtable := *(*uintptr)(winptr.FromUintptr(obj))
	method := *(*uintptr)(winptr.Add(vtable, uintptr(vtQueryInterface)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(method, obj, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out)))
	return ret
}

func (e *H264Encoder) release(obj uintptr) uintptr {
	if obj == 0 {
		return 0
	}
	vtable := *(*uintptr)(winptr.FromUintptr(obj))
	method := *(*uintptr)(winptr.Add(vtable, uintptr(vtRelease)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(method, obj)
	return ret
}

func (e *H264Encoder) callMethod(obj uintptr, methodIndex int, args ...uintptr) uintptr {
	vtable := *(*uintptr)(winptr.FromUintptr(obj))
	method := *(*uintptr)(winptr.Add(vtable, uintptr(methodIndex)*unsafe.Sizeof(uintptr(0))))
	allArgs := make([]uintptr, 1+len(args))
	allArgs[0] = obj
	copy(allArgs[1:], args)
	ret, _, _ := syscall.SyscallN(method, allArgs...)
	return ret
}

func packSize(width, height int) uint64 {
	return (uint64(width) << 32) | uint64(height)
}

func packRatio(num, den int) uint64 {
	return (uint64(num) << 32) | uint64(den)
}
