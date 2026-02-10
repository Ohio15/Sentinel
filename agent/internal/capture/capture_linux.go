//go:build linux && !arm64 && !arm

package capture

/*
#cgo LDFLAGS: -lX11 -lXext -lXfixes
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/extensions/XShm.h>
#include <X11/extensions/Xfixes.h>
#include <sys/shm.h>
#include <stdlib.h>
#include <string.h>

// Wrapper to avoid Go cgo limitations with unions
static int xshm_get_shmid(XShmSegmentInfo *info) {
    return info->shmid;
}

static void xshm_set_shmid(XShmSegmentInfo *info, int id) {
    info->shmid = id;
}

static char* xshm_get_shmaddr(XShmSegmentInfo *info) {
    return info->shmaddr;
}

static void xshm_set_shmaddr(XShmSegmentInfo *info, char *addr) {
    info->shmaddr = addr;
}

static void xshm_set_readOnly(XShmSegmentInfo *info, Bool readOnly) {
    info->readOnly = readOnly;
}

// Wrapper for XDestroyImage macro
static void destroy_ximage(XImage *image) {
    if (image != NULL) {
        XDestroyImage(image);
    }
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// X11Capture handles screen capture on Linux using XShm
type X11Capture struct {
	display    *C.Display
	screen     C.int
	rootWindow C.Window
	width      int
	height     int
	depth      int

	// XShm components
	shmInfo    C.XShmSegmentInfo
	xImage     *C.XImage
	useShm     bool

	// Cursor tracking
	lastCursor CursorData

	// Frame buffer
	frameBuffer []byte

	mu          sync.Mutex
	initialized bool
}

// NewX11Capture creates a new X11 screen capture for the specified display
func NewX11Capture(displayName string) (*X11Capture, error) {
	c := &X11Capture{}

	if err := c.initialize(displayName); err != nil {
		return nil, err
	}

	return c, nil
}

// NewDXGICapture is an alias for X11Capture on Linux to maintain API compatibility
func NewDXGICapture(monitorIndex int) (*X11Capture, error) {
	displayName := ""
	if monitorIndex > 0 {
		displayName = fmt.Sprintf(":%d.%d", 0, monitorIndex)
	}
	return NewX11Capture(displayName)
}

func (c *X11Capture) initialize(displayName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Open X display
	var cDisplayName *C.char
	if displayName != "" {
		cDisplayName = C.CString(displayName)
		defer C.free(unsafe.Pointer(cDisplayName))
	}

	c.display = C.XOpenDisplay(cDisplayName)
	if c.display == nil {
		return errors.New("failed to open X display")
	}

	// Get default screen
	c.screen = C.XDefaultScreen(c.display)
	c.rootWindow = C.XRootWindow(c.display, c.screen)

	// Get screen dimensions
	c.width = int(C.XDisplayWidth(c.display, c.screen))
	c.height = int(C.XDisplayHeight(c.display, c.screen))
	c.depth = int(C.XDefaultDepth(c.display, c.screen))

	// Try to use XShm for faster capture
	c.useShm = c.initShm()

	if !c.useShm {
		// Fall back to regular XGetImage - allocate frame buffer
		c.frameBuffer = make([]byte, c.width*c.height*4)
	}

	c.initialized = true
	return nil
}

func (c *X11Capture) initShm() bool {
	// Check if XShm extension is available
	if C.XShmQueryExtension(c.display) == 0 {
		return false
	}

	// Create shared memory XImage
	c.xImage = C.XShmCreateImage(
		c.display,
		C.XDefaultVisual(c.display, c.screen),
		C.uint(c.depth),
		C.ZPixmap,
		nil,
		&c.shmInfo,
		C.uint(c.width),
		C.uint(c.height),
	)

	if c.xImage == nil {
		return false
	}

	// Allocate shared memory
	shmid := C.shmget(C.IPC_PRIVATE, C.size_t(c.xImage.bytes_per_line*c.xImage.height), C.IPC_CREAT|0777)
	if shmid < 0 {
		C.destroy_ximage(c.xImage)
		c.xImage = nil
		return false
	}
	C.xshm_set_shmid(&c.shmInfo, shmid)

	// Attach shared memory
	shmaddr := C.shmat(shmid, nil, 0)
	if shmaddr == unsafe.Pointer(uintptr(^uint(0))) {
		C.shmctl(shmid, C.IPC_RMID, nil)
		C.destroy_ximage(c.xImage)
		c.xImage = nil
		return false
	}
	C.xshm_set_shmaddr(&c.shmInfo, (*C.char)(shmaddr))
	c.xImage.data = (*C.char)(shmaddr)
	C.xshm_set_readOnly(&c.shmInfo, C.False)

	// Attach to X server
	if C.XShmAttach(c.display, &c.shmInfo) == 0 {
		C.shmdt(shmaddr)
		C.shmctl(shmid, C.IPC_RMID, nil)
		C.destroy_ximage(c.xImage)
		c.xImage = nil
		return false
	}

	return true
}

// CaptureFrame captures the current screen content
func (c *X11Capture) CaptureFrame(timeoutMs int) (*CapturedFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return nil, errors.New("capture not initialized")
	}

	var data []byte

	if c.useShm {
		// Use XShm for fast capture
		if C.XShmGetImage(c.display, c.rootWindow, c.xImage, 0, 0, C.AllPlanes) == 0 {
			return nil, errors.New("XShmGetImage failed")
		}

		// Copy image data
		dataSize := int(c.xImage.bytes_per_line) * c.height
		data = make([]byte, c.width*c.height*4)

		// Convert from X11 format (usually BGRX) to BGRA
		src := (*[1 << 30]byte)(unsafe.Pointer(c.xImage.data))[:dataSize:dataSize]
		bytesPerPixel := int(c.xImage.bits_per_pixel) / 8
		srcStride := int(c.xImage.bytes_per_line)

		for y := 0; y < c.height; y++ {
			for x := 0; x < c.width; x++ {
				srcOff := y*srcStride + x*bytesPerPixel
				dstOff := (y*c.width + x) * 4

				if bytesPerPixel == 4 {
					// BGRX -> BGRA
					data[dstOff] = src[srcOff]     // B
					data[dstOff+1] = src[srcOff+1] // G
					data[dstOff+2] = src[srcOff+2] // R
					data[dstOff+3] = 255           // A
				} else if bytesPerPixel == 3 {
					// BGR -> BGRA
					data[dstOff] = src[srcOff]     // B
					data[dstOff+1] = src[srcOff+1] // G
					data[dstOff+2] = src[srcOff+2] // R
					data[dstOff+3] = 255           // A
				}
			}
		}
	} else {
		// Fall back to XGetImage (slower)
		xImage := C.XGetImage(
			c.display,
			c.rootWindow,
			0, 0,
			C.uint(c.width), C.uint(c.height),
			C.AllPlanes,
			C.ZPixmap,
		)
		if xImage == nil {
			return nil, errors.New("XGetImage failed")
		}
		defer C.destroy_ximage(xImage)

		data = make([]byte, c.width*c.height*4)
		bytesPerPixel := int(xImage.bits_per_pixel) / 8
		srcStride := int(xImage.bytes_per_line)
		src := (*[1 << 30]byte)(unsafe.Pointer(xImage.data))[:srcStride*c.height : srcStride*c.height]

		for y := 0; y < c.height; y++ {
			for x := 0; x < c.width; x++ {
				srcOff := y*srcStride + x*bytesPerPixel
				dstOff := (y*c.width + x) * 4

				if bytesPerPixel >= 3 {
					data[dstOff] = src[srcOff]     // B
					data[dstOff+1] = src[srcOff+1] // G
					data[dstOff+2] = src[srcOff+2] // R
					data[dstOff+3] = 255           // A
				}
			}
		}
	}

	// Update cursor info
	c.updateCursor()

	frame := &CapturedFrame{
		Data:      data,
		Width:     c.width,
		Height:    c.height,
		Stride:    c.width * 4,
		Timestamp: time.Now(),
	}

	return frame, nil
}

func (c *X11Capture) updateCursor() {
	// Get cursor position
	var rootReturn, childReturn C.Window
	var rootX, rootY, winX, winY C.int
	var mask C.uint

	C.XQueryPointer(c.display, c.rootWindow, &rootReturn, &childReturn,
		&rootX, &rootY, &winX, &winY, &mask)

	c.lastCursor.X = int(rootX)
	c.lastCursor.Y = int(rootY)
	c.lastCursor.Visible = true

	// Get cursor image using XFixes
	cursorImage := C.XFixesGetCursorImage(c.display)
	if cursorImage != nil {
		c.lastCursor.Width = int(cursorImage.width)
		c.lastCursor.Height = int(cursorImage.height)
		c.lastCursor.HotspotX = int(cursorImage.xhot)
		c.lastCursor.HotspotY = int(cursorImage.yhot)

		// Convert cursor pixels (ARGB format, 32-bit per pixel)
		pixelCount := int(cursorImage.width) * int(cursorImage.height)
		pixels := (*[1 << 20]C.ulong)(unsafe.Pointer(cursorImage.pixels))[:pixelCount:pixelCount]
		c.lastCursor.ImageData = make([]byte, pixelCount*4)

		for i := 0; i < pixelCount; i++ {
			// Convert from ARGB to BGRA
			pixel := uint32(pixels[i])
			c.lastCursor.ImageData[i*4] = byte(pixel)         // B
			c.lastCursor.ImageData[i*4+1] = byte(pixel >> 8)  // G
			c.lastCursor.ImageData[i*4+2] = byte(pixel >> 16) // R
			c.lastCursor.ImageData[i*4+3] = byte(pixel >> 24) // A
		}

		C.XFree(unsafe.Pointer(cursorImage))
	}
}

// GetCursor returns the current cursor data
func (c *X11Capture) GetCursor() CursorData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCursor
}

// GetDimensions returns the capture dimensions
func (c *X11Capture) GetDimensions() (width, height int) {
	return c.width, c.height
}

// Release frees all resources
func (c *X11Capture) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return
	}

	if c.useShm && c.xImage != nil {
		C.XShmDetach(c.display, &c.shmInfo)
		shmaddr := C.xshm_get_shmaddr(&c.shmInfo)
		if shmaddr != nil {
			C.shmdt(unsafe.Pointer(shmaddr))
		}
		shmid := C.xshm_get_shmid(&c.shmInfo)
		if shmid >= 0 {
			C.shmctl(shmid, C.IPC_RMID, nil)
		}
		C.destroy_ximage(c.xImage)
		c.xImage = nil
	}

	if c.display != nil {
		C.XCloseDisplay(c.display)
		c.display = nil
	}

	c.initialized = false
}

// DXGICapture is an alias for X11Capture on Linux
type DXGICapture = X11Capture

// Compile-time check that X11Capture implements the same methods as DXGICapture
var _ interface {
	CaptureFrame(timeoutMs int) (*CapturedFrame, error)
	GetCursor() CursorData
	GetDimensions() (width, height int)
	Release()
} = (*X11Capture)(nil)
