//go:build windows

package clipboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procEmptyClipboard                = user32.NewProc("EmptyClipboard")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procSetClipboardData              = user32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber    = user32.NewProc("GetClipboardSequenceNumber")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procIsClipboardFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	procEnumClipboardFormats          = user32.NewProc("EnumClipboardFormats")
	procGetClipboardFormatNameW       = user32.NewProc("GetClipboardFormatNameW")
	procRegisterClipboardFormatW      = user32.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc   = kernel32.NewProc("GlobalAlloc")
	procGlobalFree    = kernel32.NewProc("GlobalFree")
	procGlobalLock    = kernel32.NewProc("GlobalLock")
	procGlobalUnlock  = kernel32.NewProc("GlobalUnlock")
	procGlobalSize    = kernel32.NewProc("GlobalSize")

	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")

	// Shell32 for file handling
	procDragQueryFileW = shell32.NewProc("DragQueryFileW")

	// GDI32 for DIB handling
	procGetDIBits       = gdi32.NewProc("GetDIBits")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC        = gdi32.NewProc("DeleteDC")
	procSelectObject    = gdi32.NewProc("SelectObject")
)

// Windows clipboard format constants
const (
	CF_TEXT         = 1
	CF_BITMAP       = 2
	CF_DIB          = 8
	CF_DIBV5        = 17
	CF_UNICODETEXT  = 13
	CF_HDROP        = 15

	GMEM_MOVEABLE = 0x0002
	GMEM_ZEROINIT = 0x0040

	WM_CLIPBOARDUPDATE = 0x031D
	WM_DESTROY         = 0x0002

	WS_EX_TOOLWINDOW = 0x00000080
	HWND_MESSAGE     = ^uintptr(2) // (HWND)-3
)

// Registered clipboard format IDs (set at runtime)
var (
	cfHTML uintptr
	cfRTF  uintptr
	cfPNG  uintptr
)

// WNDCLASSEXW structure
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// MSG structure
type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// BITMAPINFOHEADER for DIB processing
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// WindowsClipboard implements IClipboard for Windows
type WindowsClipboard struct {
	config     ClipboardConfig
	hwnd       uintptr
	callback   func(content *ClipboardContent)
	lastSeq    uint32
	lastID     string
	running    bool
	stopCh     chan struct{}
	mu         sync.RWMutex
	rateLimiter *rateLimiter
}

// rateLimiter controls clipboard sync frequency
type rateLimiter struct {
	lastSync time.Time
	interval time.Duration
	mu       sync.Mutex
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	return &rateLimiter{
		interval: interval,
	}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.lastSync) < r.interval {
		return false
	}
	r.lastSync = now
	return true
}

var (
	clipboardWndProc   uintptr
	windowsClipboard   *WindowsClipboard
)

func init() {
	// Register custom clipboard formats
	cfHTML, _, _ = procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("HTML Format"))))
	cfRTF, _, _ = procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Rich Text Format"))))
	cfPNG, _, _ = procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("PNG"))))
}

// NewWindowsClipboard creates a new Windows clipboard handler
func NewWindowsClipboard(config ClipboardConfig) *WindowsClipboard {
	if config.SyncInterval == 0 {
		config.SyncInterval = 200 * time.Millisecond
	}

	c := &WindowsClipboard{
		config:      config,
		stopCh:      make(chan struct{}),
		rateLimiter: newRateLimiter(config.SyncInterval),
	}
	windowsClipboard = c
	return c
}

// Initialize sets up clipboard monitoring
func (c *WindowsClipboard) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get initial sequence number
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	c.lastSeq = uint32(seq)

	log.Printf("[Clipboard] Initialized with config: direction=%s, text=%v, images=%v, files=%v",
		c.config.Direction, c.config.EnableText, c.config.EnableImages, c.config.EnableFiles)

	return nil
}

// GetContent retrieves current clipboard content with all available formats
func (c *WindowsClipboard) GetContent() (*ClipboardContent, error) {
	c.mu.RLock()
	config := c.config
	c.mu.RUnlock()

	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return nil, errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	content := &ClipboardContent{
		ID:        GenerateContentID(),
		Timestamp: time.Now().UnixMilli(),
		Source:    "host",
		Formats:   make([]ClipboardFormat, 0),
	}

	// Enumerate available formats
	var format uintptr = 0
	for {
		format, _, _ = procEnumClipboardFormats.Call(format)
		if format == 0 {
			break
		}

		cf, err := c.readFormat(format, config)
		if err == nil && cf != nil {
			content.Formats = append(content.Formats, *cf)
		}
	}

	if len(content.Formats) == 0 {
		return nil, ErrEmpty
	}

	return content, nil
}

// readFormat reads a specific clipboard format
func (c *WindowsClipboard) readFormat(format uintptr, config ClipboardConfig) (*ClipboardFormat, error) {
	switch format {
	case CF_UNICODETEXT:
		if !config.EnableText {
			return nil, nil
		}
		return c.readTextFormat()

	case CF_DIB, CF_DIBV5:
		if !config.EnableImages {
			return nil, nil
		}
		return c.readDIBFormat(format)

	case CF_HDROP:
		if !config.EnableFiles {
			return nil, nil
		}
		return c.readFilesFormat()

	default:
		// Check for registered formats
		if format == cfHTML && config.EnableText {
			return c.readHTMLFormat()
		}
		if format == cfRTF && config.EnableText {
			return c.readRTFFormat()
		}
		if format == cfPNG && config.EnableImages {
			return c.readPNGFormat()
		}
	}

	return nil, nil
}

// readTextFormat reads Unicode text from clipboard
func (c *WindowsClipboard) readTextFormat() (*ClipboardFormat, error) {
	handle, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, ErrEmpty
	}

	// Convert UTF-16 to string
	utf16Slice := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:size/2:size/2]
	var end int
	for i, ch := range utf16Slice {
		if ch == 0 {
			end = i
			break
		}
		end = i + 1
	}

	text := string(utf16.Decode(utf16Slice[:end]))

	// Check size limit
	if len(text) > c.config.MaxTextSize {
		return &ClipboardFormat{
			Type:      FormatText,
			Size:      len(text),
			Data:      text[:c.config.MaxTextSize],
			Truncated: true,
			MimeType:  "text/plain; charset=utf-8",
		}, nil
	}

	return &ClipboardFormat{
		Type:     FormatText,
		Size:     len(text),
		Data:     text,
		MimeType: "text/plain; charset=utf-8",
	}, nil
}

// readHTMLFormat reads HTML from clipboard
func (c *WindowsClipboard) readHTMLFormat() (*ClipboardFormat, error) {
	if cfHTML == 0 {
		return nil, ErrFormatNotFound
	}

	handle, _, _ := procGetClipboardData.Call(cfHTML)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, ErrEmpty
	}

	// HTML format is UTF-8
	data := make([]byte, size)
	copy(data, (*[1 << 20]byte)(unsafe.Pointer(ptr))[:size:size])

	// Find null terminator
	for i, b := range data {
		if b == 0 {
			data = data[:i]
			break
		}
	}

	html := string(data)

	if len(html) > c.config.MaxTextSize {
		return &ClipboardFormat{
			Type:      FormatHTML,
			Size:      len(html),
			Data:      html[:c.config.MaxTextSize],
			Truncated: true,
			MimeType:  "text/html",
		}, nil
	}

	return &ClipboardFormat{
		Type:     FormatHTML,
		Size:     len(html),
		Data:     html,
		MimeType: "text/html",
	}, nil
}

// readRTFFormat reads RTF from clipboard
func (c *WindowsClipboard) readRTFFormat() (*ClipboardFormat, error) {
	if cfRTF == 0 {
		return nil, ErrFormatNotFound
	}

	handle, _, _ := procGetClipboardData.Call(cfRTF)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, ErrEmpty
	}

	data := make([]byte, size)
	copy(data, (*[1 << 20]byte)(unsafe.Pointer(ptr))[:size:size])

	// Find null terminator
	for i, b := range data {
		if b == 0 {
			data = data[:i]
			break
		}
	}

	rtf := string(data)

	if len(rtf) > c.config.MaxTextSize {
		return &ClipboardFormat{
			Type:      FormatRTF,
			Size:      len(rtf),
			Data:      rtf[:c.config.MaxTextSize],
			Truncated: true,
			MimeType:  "text/rtf",
		}, nil
	}

	return &ClipboardFormat{
		Type:     FormatRTF,
		Size:     len(rtf),
		Data:     rtf,
		MimeType: "text/rtf",
	}, nil
}

// readDIBFormat reads DIB image and converts to PNG
func (c *WindowsClipboard) readDIBFormat(format uintptr) (*ClipboardFormat, error) {
	handle, _, _ := procGetClipboardData.Call(format)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size < uintptr(unsafe.Sizeof(BITMAPINFOHEADER{})) {
		return nil, errors.New("invalid DIB data")
	}

	// Read BITMAPINFOHEADER
	header := (*BITMAPINFOHEADER)(unsafe.Pointer(ptr))

	width := int(header.BiWidth)
	height := int(header.BiHeight)
	if height < 0 {
		height = -height
	}

	bitCount := int(header.BiBitCount)
	if bitCount != 24 && bitCount != 32 {
		return nil, fmt.Errorf("unsupported bit count: %d", bitCount)
	}

	// Calculate pixel data offset and row stride
	headerSize := int(header.BiSize)
	rowStride := ((width*bitCount + 31) / 32) * 4
	pixelDataSize := rowStride * height

	if int(size) < headerSize+pixelDataSize {
		return nil, errors.New("DIB data too small")
	}

	// Convert DIB to image.RGBA
	pixelData := (*[1 << 26]byte)(unsafe.Pointer(ptr + uintptr(headerSize)))[:pixelDataSize:pixelDataSize]
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	bytesPerPixel := bitCount / 8
	bottomUp := header.BiHeight > 0

	for y := 0; y < height; y++ {
		srcY := y
		if bottomUp {
			srcY = height - 1 - y
		}
		srcRow := pixelData[srcY*rowStride:]

		for x := 0; x < width; x++ {
			offset := x * bytesPerPixel
			b := srcRow[offset]
			g := srcRow[offset+1]
			r := srcRow[offset+2]
			a := uint8(255)
			if bytesPerPixel == 4 {
				a = srcRow[offset+3]
			}

			dstOffset := (y*width + x) * 4
			img.Pix[dstOffset] = r
			img.Pix[dstOffset+1] = g
			img.Pix[dstOffset+2] = b
			img.Pix[dstOffset+3] = a
		}
	}

	// Encode as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("PNG encode failed: %w", err)
	}

	pngData := buf.Bytes()

	// Check size limit
	if len(pngData) > c.config.MaxImageSize {
		return nil, ErrSizeLimitExceeded
	}

	return &ClipboardFormat{
		Type:     FormatPNG,
		Size:     len(pngData),
		Data:     base64.StdEncoding.EncodeToString(pngData),
		MimeType: "image/png",
	}, nil
}

// readPNGFormat reads PNG directly from clipboard
func (c *WindowsClipboard) readPNGFormat() (*ClipboardFormat, error) {
	if cfPNG == 0 {
		return nil, ErrFormatNotFound
	}

	handle, _, _ := procGetClipboardData.Call(cfPNG)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, ErrEmpty
	}

	if int(size) > c.config.MaxImageSize {
		return nil, ErrSizeLimitExceeded
	}

	data := make([]byte, size)
	copy(data, (*[1 << 26]byte)(unsafe.Pointer(ptr))[:size:size])

	return &ClipboardFormat{
		Type:     FormatPNG,
		Size:     int(size),
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: "image/png",
	}, nil
}

// readFilesFormat reads file references from clipboard (CF_HDROP)
func (c *WindowsClipboard) readFilesFormat() (*ClipboardFormat, error) {
	handle, _, _ := procGetClipboardData.Call(CF_HDROP)
	if handle == 0 {
		return nil, ErrFormatNotFound
	}

	// Get number of files
	count, _, _ := procDragQueryFileW.Call(handle, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		return nil, ErrEmpty
	}

	files := make([]FileRef, 0, count)
	buf := make([]uint16, 260) // MAX_PATH

	for i := uintptr(0); i < count; i++ {
		// Get file path
		pathLen, _, _ := procDragQueryFileW.Call(handle, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if pathLen == 0 {
			continue
		}

		path := syscall.UTF16ToString(buf[:pathLen])

		// Get file info
		fileInfo, err := os.Stat(path)
		if err != nil {
			continue
		}

		// Calculate hash for small files (< 1MB)
		var hash string
		if fileInfo.Size() < 1024*1024 {
			if h, err := calculateFileHash(path); err == nil {
				hash = h
			}
		}

		files = append(files, FileRef{
			Name: fileInfo.Name(),
			Size: fileInfo.Size(),
			Path: path, // Server-side only
			Hash: hash,
		})
	}

	if len(files) == 0 {
		return nil, ErrEmpty
	}

	return &ClipboardFormat{
		Type:     FormatFiles,
		Size:     len(files),
		Files:    files,
		MimeType: "application/x-file-list",
	}, nil
}

// calculateFileHash calculates SHA-256 hash of a file
func calculateFileHash(path string) (string, error) {
	// Note: In production, use os.ReadFile with proper error handling
	// This is a simplified version
	data, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer syscall.Close(data)

	// Read file content (limited to 1MB for hash)
	buf := make([]byte, 1024*1024)
	n, _ := syscall.Read(data, buf)
	if n == 0 {
		return "", errors.New("empty file")
	}

	h := sha256.Sum256(buf[:n])
	return fmt.Sprintf("%x", h), nil
}

// SetContent sets clipboard content
func (c *WindowsClipboard) SetContent(content *ClipboardContent) error {
	if content == nil || len(content.Formats) == 0 {
		return ErrEmpty
	}

	c.mu.Lock()
	c.lastID = content.ID
	c.mu.Unlock()

	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	for _, format := range content.Formats {
		var err error
		switch format.Type {
		case FormatText:
			err = c.writeTextFormat(format.Data)
		case FormatHTML:
			err = c.writeHTMLFormat(format.Data)
		case FormatRTF:
			err = c.writeRTFFormat(format.Data)
		case FormatPNG, FormatJPEG, FormatBitmap:
			err = c.writeImageFormat(format.Data)
		}
		if err != nil {
			log.Printf("[Clipboard] Failed to write format %s: %v", format.Type, err)
		}
	}

	return nil
}

// writeTextFormat writes Unicode text to clipboard
func (c *WindowsClipboard) writeTextFormat(text string) error {
	utf16Text := utf16.Encode([]rune(text + "\x00"))
	size := len(utf16Text) * 2

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return errors.New("failed to allocate memory")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to lock memory")
	}

	dst := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:len(utf16Text):len(utf16Text)]
	copy(dst, utf16Text)
	procGlobalUnlock.Call(hMem)

	ret, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, hMem)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to set clipboard data")
	}

	return nil
}

// writeHTMLFormat writes HTML to clipboard
func (c *WindowsClipboard) writeHTMLFormat(html string) error {
	if cfHTML == 0 {
		return ErrFormatNotFound
	}

	// HTML clipboard format requires specific header
	// Format: Version:0.9\r\nStartHTML:XXXX\r\nEndHTML:XXXX\r\nStartFragment:XXXX\r\nEndFragment:XXXX\r\n
	header := fmt.Sprintf(
		"Version:0.9\r\nStartHTML:%08d\r\nEndHTML:%08d\r\nStartFragment:%08d\r\nEndFragment:%08d\r\n",
		97, 97+len(html)+36, 97, 97+len(html),
	)
	fullHTML := header + "<!--StartFragment-->" + html + "<!--EndFragment-->"

	data := []byte(fullHTML + "\x00")

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
	if hMem == 0 {
		return errors.New("failed to allocate memory")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to lock memory")
	}

	copy((*[1 << 20]byte)(unsafe.Pointer(ptr))[:len(data):len(data)], data)
	procGlobalUnlock.Call(hMem)

	ret, _, _ := procSetClipboardData.Call(cfHTML, hMem)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to set clipboard data")
	}

	return nil
}

// writeRTFFormat writes RTF to clipboard
func (c *WindowsClipboard) writeRTFFormat(rtf string) error {
	if cfRTF == 0 {
		return ErrFormatNotFound
	}

	data := []byte(rtf + "\x00")

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
	if hMem == 0 {
		return errors.New("failed to allocate memory")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to lock memory")
	}

	copy((*[1 << 20]byte)(unsafe.Pointer(ptr))[:len(data):len(data)], data)
	procGlobalUnlock.Call(hMem)

	ret, _, _ := procSetClipboardData.Call(cfRTF, hMem)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to set clipboard data")
	}

	return nil
}

// writeImageFormat writes PNG image to clipboard as DIB
func (c *WindowsClipboard) writeImageFormat(base64Data string) error {
	// Decode base64
	pngData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("base64 decode failed: %w", err)
	}

	// Decode PNG
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("PNG decode failed: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Create DIB
	rowStride := ((width*32 + 31) / 32) * 4
	pixelDataSize := rowStride * height
	headerSize := int(unsafe.Sizeof(BITMAPINFOHEADER{}))
	totalSize := headerSize + pixelDataSize

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE|GMEM_ZEROINIT, uintptr(totalSize))
	if hMem == 0 {
		return errors.New("failed to allocate memory")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to lock memory")
	}

	// Write BITMAPINFOHEADER
	header := (*BITMAPINFOHEADER)(unsafe.Pointer(ptr))
	header.BiSize = uint32(headerSize)
	header.BiWidth = int32(width)
	header.BiHeight = int32(height) // Positive = bottom-up
	header.BiPlanes = 1
	header.BiBitCount = 32
	header.BiCompression = 0 // BI_RGB
	header.BiSizeImage = uint32(pixelDataSize)

	// Write pixel data (bottom-up, BGRA)
	pixelData := (*[1 << 26]byte)(unsafe.Pointer(ptr + uintptr(headerSize)))[:pixelDataSize:pixelDataSize]

	for y := 0; y < height; y++ {
		dstY := height - 1 - y // Bottom-up
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			offset := dstY*rowStride + x*4
			pixelData[offset] = uint8(b >> 8)   // B
			pixelData[offset+1] = uint8(g >> 8) // G
			pixelData[offset+2] = uint8(r >> 8) // R
			pixelData[offset+3] = uint8(a >> 8) // A
		}
	}

	procGlobalUnlock.Call(hMem)

	ret, _, _ := procSetClipboardData.Call(CF_DIB, hMem)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to set clipboard data")
	}

	return nil
}

// GetText retrieves text from clipboard
func (c *WindowsClipboard) GetText() (string, error) {
	ret, _, _ := procIsClipboardFormatAvailable.Call(CF_UNICODETEXT)
	if ret == 0 {
		return "", ErrFormatNotFound
	}

	ret, _, _ = procOpenClipboard.Call(0)
	if ret == 0 {
		return "", errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	format, err := c.readTextFormat()
	if err != nil {
		return "", err
	}

	return format.Data, nil
}

// SetText sets text to clipboard
func (c *WindowsClipboard) SetText(text string) error {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	return c.writeTextFormat(text)
}

// GetImage retrieves image from clipboard as PNG bytes
func (c *WindowsClipboard) GetImage() ([]byte, error) {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return nil, errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	// Try PNG first
	if cfPNG != 0 {
		if format, err := c.readPNGFormat(); err == nil {
			return base64.StdEncoding.DecodeString(format.Data)
		}
	}

	// Fall back to DIB
	ret, _, _ = procIsClipboardFormatAvailable.Call(CF_DIB)
	if ret != 0 {
		if format, err := c.readDIBFormat(CF_DIB); err == nil {
			return base64.StdEncoding.DecodeString(format.Data)
		}
	}

	return nil, ErrFormatNotFound
}

// SetImage sets image to clipboard from PNG bytes
func (c *WindowsClipboard) SetImage(pngData []byte) error {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	return c.writeImageFormat(base64.StdEncoding.EncodeToString(pngData))
}

// GetFiles retrieves file references from clipboard
func (c *WindowsClipboard) GetFiles() ([]FileRef, error) {
	ret, _, _ := procIsClipboardFormatAvailable.Call(CF_HDROP)
	if ret == 0 {
		return nil, ErrFormatNotFound
	}

	ret, _, _ = procOpenClipboard.Call(0)
	if ret == 0 {
		return nil, errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	format, err := c.readFilesFormat()
	if err != nil {
		return nil, err
	}

	return format.Files, nil
}

// Watch starts monitoring for clipboard changes
func (c *WindowsClipboard) Watch(callback func(content *ClipboardContent)) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.callback = callback
	c.mu.Unlock()

	// Start message loop for clipboard notifications
	go c.runMessageLoop()

	// Start polling fallback
	go c.pollLoop()

	log.Printf("[Clipboard] Started watching for changes")
	return nil
}

// StopWatch stops monitoring
func (c *WindowsClipboard) StopWatch() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopCh)
	log.Printf("[Clipboard] Stopped watching")
}

func (c *WindowsClipboard) runMessageLoop() {
	className := syscall.StringToUTF16Ptr("SentinelClipboardMonitor")

	clipboardWndProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		switch msg {
		case WM_CLIPBOARDUPDATE:
			if windowsClipboard != nil {
				windowsClipboard.handleClipboardChange()
			}
			return 0
		case WM_DESTROY:
			procPostQuitMessage.Call(0)
			return 0
		}
		ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return ret
	})

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = clipboardWndProc
	wc.LpszClassName = className

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0,
		0, 0, 0, 0,
		HWND_MESSAGE,
		0, 0, 0,
	)

	if hwnd == 0 {
		log.Printf("[Clipboard] Failed to create message window")
		return
	}

	c.mu.Lock()
	c.hwnd = hwnd
	c.mu.Unlock()

	procAddClipboardFormatListener.Call(hwnd)

	var msg MSG
	for {
		select {
		case <-c.stopCh:
			procRemoveClipboardFormatListener.Call(hwnd)
			procDestroyWindow.Call(hwnd)
			return
		default:
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if ret == 0 || ret == ^uintptr(0) {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
}

func (c *WindowsClipboard) pollLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkClipboardChange()
		}
	}
}

func (c *WindowsClipboard) checkClipboardChange() {
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	newSeq := uint32(seq)

	c.mu.Lock()
	if newSeq != c.lastSeq {
		c.lastSeq = newSeq
		c.mu.Unlock()
		c.handleClipboardChange()
	} else {
		c.mu.Unlock()
	}
}

func (c *WindowsClipboard) handleClipboardChange() {
	// Rate limiting
	if !c.rateLimiter.Allow() {
		return
	}

	// Check direction
	c.mu.RLock()
	config := c.config
	callback := c.callback
	c.mu.RUnlock()

	if config.Direction == DirectionDisabled || config.Direction == DirectionViewerToHost {
		return
	}

	content, err := c.GetContent()
	if err != nil {
		return
	}

	// Check if this is our own update
	c.mu.RLock()
	if content.ID == c.lastID {
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	if callback != nil {
		callback(content)
	}
}

// Clear clears the clipboard
func (c *WindowsClipboard) Clear() error {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()
	return nil
}

// Release frees resources
func (c *WindowsClipboard) Release() {
	c.StopWatch()
	log.Printf("[Clipboard] Released")
}

// Legacy support - ClipboardHandler wraps WindowsClipboard for backward compatibility
type ClipboardHandler struct {
	clipboard *WindowsClipboard
	onChange  func(ClipboardData)
}

// ClipboardData for legacy compatibility
type ClipboardData struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// NewClipboardHandler creates a legacy clipboard handler
func NewClipboardHandler(onChange func(ClipboardData)) *ClipboardHandler {
	config := DefaultClipboardConfig()
	clipboard := NewWindowsClipboard(config)

	h := &ClipboardHandler{
		clipboard: clipboard,
		onChange:  onChange,
	}

	return h
}

// Start begins monitoring clipboard changes
func (h *ClipboardHandler) Start() error {
	if err := h.clipboard.Initialize(); err != nil {
		return err
	}

	return h.clipboard.Watch(func(content *ClipboardContent) {
		if h.onChange != nil {
			data := ClipboardData{
				Timestamp: content.Timestamp,
			}

			// Extract text and HTML
			for _, format := range content.Formats {
				switch format.Type {
				case FormatText:
					data.Text = format.Data
				case FormatHTML:
					data.HTML = format.Data
				}
			}

			h.onChange(data)
		}
	})
}

// GetText returns the current clipboard text
func (h *ClipboardHandler) GetText() (string, error) {
	return h.clipboard.GetText()
}

// SetText sets the clipboard text
func (h *ClipboardHandler) SetText(text string) error {
	return h.clipboard.SetText(text)
}

// Stop stops the clipboard handler
func (h *ClipboardHandler) Stop() {
	h.clipboard.Release()
}

// Compile-time interface check
var _ IClipboard = (*WindowsClipboard)(nil)
