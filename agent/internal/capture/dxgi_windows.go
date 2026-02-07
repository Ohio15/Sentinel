//go:build windows

package capture

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// DXGI and D3D11 GUIDs
var (
	IID_IDXGIDevice          = GUID{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	IID_IDXGIAdapter         = GUID{0x2411e7e1, 0x12ac, 0x4ccf, [8]byte{0xbd, 0x14, 0x97, 0x98, 0xe8, 0x53, 0x4d, 0xc0}}
	IID_IDXGIOutput          = GUID{0xae02eedb, 0xc735, 0x4690, [8]byte{0x8d, 0x52, 0x5a, 0x8d, 0xc2, 0x02, 0x13, 0xaa}}
	IID_IDXGIOutput1         = GUID{0x00cddea8, 0x939b, 0x4b83, [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc}}
	IID_ID3D11Texture2D      = GUID{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

// GUID structure
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// D3D11 and DXGI constants
const (
	D3D11_SDK_VERSION              = 7
	D3D_DRIVER_TYPE_HARDWARE       = 1
	D3D_DRIVER_TYPE_WARP           = 5
	D3D11_CREATE_DEVICE_BGRA_SUPPORT = 0x20

	DXGI_FORMAT_B8G8R8A8_UNORM     = 87
	DXGI_FORMAT_R8G8B8A8_UNORM     = 28

	D3D11_USAGE_STAGING            = 3
	D3D11_CPU_ACCESS_READ          = 0x20000

	DXGI_ERROR_WAIT_TIMEOUT        = 0x88790006
	DXGI_ERROR_ACCESS_LOST         = 0x887A0026
	DXGI_ERROR_INVALID_CALL        = 0x887A0001
)

// D3D11 structures
type D3D11_TEXTURE2D_DESC struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     DXGI_SAMPLE_DESC
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type DXGI_SAMPLE_DESC struct {
	Count   uint32
	Quality uint32
}

type DXGI_OUTPUT_DESC struct {
	DeviceName         [32]uint16
	DesktopCoordinates RECT
	AttachedToDesktop  int32
	Rotation           uint32
	Monitor            uintptr
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type DXGI_OUTDUPL_DESC struct {
	ModeDesc                   DXGI_MODE_DESC
	Rotation                   uint32
	DesktopImageInSystemMemory int32
}

type DXGI_MODE_DESC struct {
	Width            uint32
	Height           uint32
	RefreshRate      DXGI_RATIONAL
	Format           uint32
	ScanlineOrdering uint32
	Scaling          uint32
}

type DXGI_RATIONAL struct {
	Numerator   uint32
	Denominator uint32
}

type DXGI_OUTDUPL_FRAME_INFO struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerPosition           DXGI_OUTDUPL_POINTER_POSITION
	TotalMetadataBufferSize   uint32
	PointerShapeBufferSize    uint32
}

type DXGI_OUTDUPL_POINTER_POSITION struct {
	Position POINT
	Visible  int32
}

type POINT struct {
	X int32
	Y int32
}

type D3D11_MAPPED_SUBRESOURCE struct {
	pData      uintptr
	RowPitch   uint32
	DepthPitch uint32
}

type D3D11_BOX struct {
	Left   uint32
	Top    uint32
	Front  uint32
	Right  uint32
	Bottom uint32
	Back   uint32
}

// DLL and function pointers
var (
	d3d11                   = syscall.NewLazyDLL("d3d11.dll")
	dxgi                    = syscall.NewLazyDLL("dxgi.dll")
	procD3D11CreateDevice   = d3d11.NewProc("D3D11CreateDevice")
	procCreateDXGIFactory1  = dxgi.NewProc("CreateDXGIFactory1")
)

// COM interface VTable indices
const (
	// IUnknown
	vtQueryInterface = 0
	vtAddRef         = 1
	vtRelease        = 2

	// IDXGIFactory
	vtEnumAdapters   = 7

	// IDXGIAdapter
	vtEnumOutputs    = 7

	// IDXGIOutput
	vtGetDesc        = 7

	// IDXGIOutput1
	vtDuplicateOutput = 22

	// IDXGIOutputDuplication
	vtGetDesc_Dup           = 7
	vtAcquireNextFrame      = 8
	vtGetFrameDirtyRects    = 9
	vtGetFrameMoveRects     = 10
	vtGetFramePointerShape  = 11
	vtMapDesktopSurface     = 12
	vtUnMapDesktopSurface   = 13
	vtReleaseFrame          = 14

	// ID3D11Device
	vtCreateTexture2D       = 5
	vtGetImmediateContext   = 40

	// ID3D11DeviceContext
	vtMap                   = 14
	vtUnmap                 = 15
	vtCopyResource          = 47
	vtCopySubresourceRegion = 46

	// IDXGIResource
	vtGetSharedHandle       = 10
)

// DXGICapture handles desktop duplication
type DXGICapture struct {
	device        uintptr // ID3D11Device
	context       uintptr // ID3D11DeviceContext
	output1       uintptr // IDXGIOutput1
	duplication   uintptr // IDXGIOutputDuplication
	stagingTex    uintptr // ID3D11Texture2D (staging)

	width         int
	height        int
	stride        int
	monitorIndex  int

	frameBuffer   []byte
	dirtyRects    []RECT
	moveRects     []byte

	lastCursor    CursorData
	cursorBuffer  []byte

	mu            sync.Mutex
	initialized   bool
}

// NewDXGICapture creates a new DXGI desktop capture for the specified monitor
func NewDXGICapture(monitorIndex int) (*DXGICapture, error) {
	c := &DXGICapture{
		monitorIndex: monitorIndex,
		dirtyRects:   make([]RECT, 64),
		moveRects:    make([]byte, 64*24), // DXGI_OUTDUPL_MOVE_RECT is 24 bytes
		cursorBuffer: make([]byte, 128*128*4), // Max cursor size
	}

	if err := c.initialize(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *DXGICapture) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create D3D11 device
	var device, context uintptr
	var featureLevel uint32

	// Try hardware first, then WARP
	driverTypes := []uint32{D3D_DRIVER_TYPE_HARDWARE, D3D_DRIVER_TYPE_WARP}
	var hr uintptr

	for _, driverType := range driverTypes {
		hr, _, _ = procD3D11CreateDevice.Call(
			0, // pAdapter
			uintptr(driverType),
			0, // Software
			D3D11_CREATE_DEVICE_BGRA_SUPPORT,
			0, // pFeatureLevels
			0, // FeatureLevels count
			D3D11_SDK_VERSION,
			uintptr(unsafe.Pointer(&device)),
			uintptr(unsafe.Pointer(&featureLevel)),
			uintptr(unsafe.Pointer(&context)),
		)
		if hr == 0 {
			break
		}
	}

	if hr != 0 {
		return fmt.Errorf("D3D11CreateDevice failed: 0x%x", hr)
	}

	c.device = device
	c.context = context

	// Get DXGI device
	var dxgiDevice uintptr
	hr = c.queryInterface(device, &IID_IDXGIDevice, &dxgiDevice)
	if hr != 0 {
		c.Release()
		return fmt.Errorf("QueryInterface IDXGIDevice failed: 0x%x", hr)
	}
	defer c.release(dxgiDevice)

	// Get DXGI adapter
	var adapter uintptr
	hr = c.callMethod(dxgiDevice, 7, uintptr(unsafe.Pointer(&adapter))) // GetParent
	if hr != 0 {
		c.Release()
		return fmt.Errorf("GetAdapter failed: 0x%x", hr)
	}
	defer c.release(adapter)

	// Get output (monitor)
	var output uintptr
	hr = c.callMethod(adapter, vtEnumOutputs, uintptr(c.monitorIndex), uintptr(unsafe.Pointer(&output)))
	if hr != 0 {
		c.Release()
		return fmt.Errorf("EnumOutputs failed: 0x%x (monitor %d may not exist)", hr, c.monitorIndex)
	}
	defer c.release(output)

	// Get output description
	var outputDesc DXGI_OUTPUT_DESC
	hr = c.callMethod(output, vtGetDesc, uintptr(unsafe.Pointer(&outputDesc)))
	if hr != 0 {
		c.Release()
		return fmt.Errorf("GetDesc failed: 0x%x", hr)
	}

	c.width = int(outputDesc.DesktopCoordinates.Right - outputDesc.DesktopCoordinates.Left)
	c.height = int(outputDesc.DesktopCoordinates.Bottom - outputDesc.DesktopCoordinates.Top)
	c.stride = c.width * 4

	// Query for IDXGIOutput1
	var output1 uintptr
	hr = c.queryInterface(output, &IID_IDXGIOutput1, &output1)
	if hr != 0 {
		c.Release()
		return fmt.Errorf("QueryInterface IDXGIOutput1 failed: 0x%x", hr)
	}
	c.output1 = output1

	// Create output duplication
	var duplication uintptr
	hr = c.callMethod(output1, vtDuplicateOutput, device, uintptr(unsafe.Pointer(&duplication)))
	if hr != 0 {
		c.Release()
		return fmt.Errorf("DuplicateOutput failed: 0x%x", hr)
	}
	c.duplication = duplication

	// Create staging texture for CPU read
	stagingDesc := D3D11_TEXTURE2D_DESC{
		Width:          uint32(c.width),
		Height:         uint32(c.height),
		MipLevels:      1,
		ArraySize:      1,
		Format:         DXGI_FORMAT_B8G8R8A8_UNORM,
		SampleDesc:     DXGI_SAMPLE_DESC{Count: 1, Quality: 0},
		Usage:          D3D11_USAGE_STAGING,
		BindFlags:      0,
		CPUAccessFlags: D3D11_CPU_ACCESS_READ,
		MiscFlags:      0,
	}

	var stagingTex uintptr
	hr = c.callMethod(device, vtCreateTexture2D,
		uintptr(unsafe.Pointer(&stagingDesc)),
		0,
		uintptr(unsafe.Pointer(&stagingTex)),
	)
	if hr != 0 {
		c.Release()
		return fmt.Errorf("CreateTexture2D (staging) failed: 0x%x", hr)
	}
	c.stagingTex = stagingTex

	// Allocate frame buffer
	c.frameBuffer = make([]byte, c.stride*c.height)

	c.initialized = true
	return nil
}

// CaptureFrame acquires the next frame from the desktop
func (c *DXGICapture) CaptureFrame(timeoutMs int) (*CapturedFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return nil, errors.New("capture not initialized")
	}

	var frameInfo DXGI_OUTDUPL_FRAME_INFO
	var desktopResource uintptr

	// Acquire next frame
	hr := c.callMethod(c.duplication, vtAcquireNextFrame,
		uintptr(timeoutMs),
		uintptr(unsafe.Pointer(&frameInfo)),
		uintptr(unsafe.Pointer(&desktopResource)),
	)

	if hr == DXGI_ERROR_WAIT_TIMEOUT {
		return nil, nil // No new frame
	}

	if hr == DXGI_ERROR_ACCESS_LOST {
		// Need to reinitialize (display mode change, etc.)
		c.cleanup()
		if err := c.initialize(); err != nil {
			return nil, fmt.Errorf("reinitialize failed: %w", err)
		}
		return nil, errors.New("access lost, reinitialized")
	}

	if hr != 0 {
		return nil, fmt.Errorf("AcquireNextFrame failed: 0x%x", hr)
	}

	defer c.release(desktopResource)
	defer c.callMethod(c.duplication, vtReleaseFrame)

	// Get the desktop texture
	var desktopTex uintptr
	hr = c.queryInterface(desktopResource, &IID_ID3D11Texture2D, &desktopTex)
	if hr != 0 {
		return nil, fmt.Errorf("QueryInterface ID3D11Texture2D failed: 0x%x", hr)
	}
	defer c.release(desktopTex)

	// Copy to staging texture
	c.callMethod(c.context, vtCopyResource, c.stagingTex, desktopTex)

	// Map staging texture
	var mapped D3D11_MAPPED_SUBRESOURCE
	hr = c.callMethod(c.context, vtMap, c.stagingTex, 0, 1, 0, uintptr(unsafe.Pointer(&mapped)))
	if hr != 0 {
		return nil, fmt.Errorf("Map failed: 0x%x", hr)
	}

	// Copy to frame buffer
	srcPtr := mapped.pData
	dstPtr := unsafe.Pointer(&c.frameBuffer[0])
	rowSize := c.width * 4

	for y := 0; y < c.height; y++ {
		src := unsafe.Pointer(srcPtr + uintptr(y)*uintptr(mapped.RowPitch))
		dst := unsafe.Pointer(uintptr(dstPtr) + uintptr(y*rowSize))
		copy(
			(*[1 << 30]byte)(dst)[:rowSize:rowSize],
			(*[1 << 30]byte)(src)[:rowSize:rowSize],
		)
	}

	// Unmap
	c.callMethod(c.context, vtUnmap, c.stagingTex, 0)

	// Get dirty rectangles
	var dirtyRects []image.Rectangle
	if frameInfo.TotalMetadataBufferSize > 0 {
		dirtyRects = c.getDirtyRects()
	}

	// Get cursor info
	c.updateCursor(&frameInfo)

	// Create output frame
	frame := &CapturedFrame{
		Data:       make([]byte, len(c.frameBuffer)),
		Width:      c.width,
		Height:     c.height,
		Stride:     c.stride,
		DirtyRects: dirtyRects,
		Timestamp:  time.Now(),
	}
	copy(frame.Data, c.frameBuffer)

	return frame, nil
}

func (c *DXGICapture) getDirtyRects() []image.Rectangle {
	bufferSize := uint32(len(c.dirtyRects) * 16) // RECT is 16 bytes
	var requiredSize uint32

	hr := c.callMethod(c.duplication, vtGetFrameDirtyRects,
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&c.dirtyRects[0])),
		uintptr(unsafe.Pointer(&requiredSize)),
	)

	if hr != 0 {
		return nil
	}

	numRects := int(requiredSize / 16)
	if numRects > len(c.dirtyRects) {
		numRects = len(c.dirtyRects)
	}

	rects := make([]image.Rectangle, numRects)
	for i := 0; i < numRects; i++ {
		r := c.dirtyRects[i]
		rects[i] = image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
	}

	return rects
}

func (c *DXGICapture) updateCursor(frameInfo *DXGI_OUTDUPL_FRAME_INFO) {
	c.lastCursor.X = int(frameInfo.PointerPosition.Position.X)
	c.lastCursor.Y = int(frameInfo.PointerPosition.Position.Y)
	c.lastCursor.Visible = frameInfo.PointerPosition.Visible != 0

	// Get cursor shape if changed
	if frameInfo.PointerShapeBufferSize > 0 {
		c.getCursorShape(frameInfo.PointerShapeBufferSize)
	}
}

func (c *DXGICapture) getCursorShape(bufferSize uint32) {
	if int(bufferSize) > len(c.cursorBuffer) {
		c.cursorBuffer = make([]byte, bufferSize)
	}

	var shapeInfo struct {
		Type    uint32
		Width   uint32
		Height  uint32
		Pitch   uint32
		HotSpot POINT
	}
	var requiredSize uint32

	hr := c.callMethod(c.duplication, vtGetFramePointerShape,
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&c.cursorBuffer[0])),
		uintptr(unsafe.Pointer(&requiredSize)),
		uintptr(unsafe.Pointer(&shapeInfo)),
	)

	if hr == 0 {
		c.lastCursor.ShapeType = int(shapeInfo.Type)
		c.lastCursor.Width = int(shapeInfo.Width)
		c.lastCursor.Height = int(shapeInfo.Height)
		c.lastCursor.HotspotX = int(shapeInfo.HotSpot.X)
		c.lastCursor.HotspotY = int(shapeInfo.HotSpot.Y)
		c.lastCursor.ImageData = make([]byte, requiredSize)
		copy(c.lastCursor.ImageData, c.cursorBuffer[:requiredSize])
	}
}

// GetCursor returns the current cursor data
func (c *DXGICapture) GetCursor() CursorData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCursor
}

// GetDimensions returns the capture dimensions
func (c *DXGICapture) GetDimensions() (width, height int) {
	return c.width, c.height
}

// Release frees all resources
func (c *DXGICapture) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup()
}

func (c *DXGICapture) cleanup() {
	if c.stagingTex != 0 {
		c.release(c.stagingTex)
		c.stagingTex = 0
	}
	if c.duplication != 0 {
		c.release(c.duplication)
		c.duplication = 0
	}
	if c.output1 != 0 {
		c.release(c.output1)
		c.output1 = 0
	}
	if c.context != 0 {
		c.release(c.context)
		c.context = 0
	}
	if c.device != 0 {
		c.release(c.device)
		c.device = 0
	}
	c.initialized = false
}

// Helper methods for COM calls
func (c *DXGICapture) queryInterface(obj uintptr, iid *GUID, out *uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(vtQueryInterface)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(method, obj, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out)))
	return ret
}

func (c *DXGICapture) addRef(obj uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(vtAddRef)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(method, obj)
	return ret
}

func (c *DXGICapture) release(obj uintptr) uintptr {
	if obj == 0 {
		return 0
	}
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(vtRelease)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(method, obj)
	return ret
}

func (c *DXGICapture) callMethod(obj uintptr, methodIndex int, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(methodIndex)*unsafe.Sizeof(uintptr(0))))
	allArgs := make([]uintptr, 1+len(args))
	allArgs[0] = obj
	copy(allArgs[1:], args)
	ret, _, _ := syscall.SyscallN(method, allArgs...)
	return ret
}
