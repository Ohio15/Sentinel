//go:build windows

package clipboard

import (
	"errors"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procAddClipboardFormatListener = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")

	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
)

const (
	CF_UNICODETEXT = 13
	CF_TEXT        = 1
	CF_HTML        = 49171 // Typically registered dynamically

	GMEM_MOVEABLE = 0x0002
	GMEM_ZEROINIT = 0x0040

	WM_CLIPBOARDUPDATE = 0x031D
	WM_DESTROY         = 0x0002

	WS_EX_TOOLWINDOW = 0x00000080
	HWND_MESSAGE     = ^uintptr(2) // (HWND)-3
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

// ClipboardData represents clipboard content
type ClipboardData struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ClipboardHandler manages clipboard monitoring and sync
type ClipboardHandler struct {
	onChange     func(data ClipboardData)
	lastSeq      uint32
	lastContent  string
	hwnd         uintptr
	running      bool
	stopCh       chan struct{}
	mu           sync.Mutex
}

var (
	clipboardWndProc uintptr
	clipboardHandler *ClipboardHandler
)

// NewClipboardHandler creates a new clipboard handler with change notification
func NewClipboardHandler(onChange func(ClipboardData)) *ClipboardHandler {
	h := &ClipboardHandler{
		onChange: onChange,
		stopCh:   make(chan struct{}),
	}
	clipboardHandler = h

	return h
}

// Start begins monitoring clipboard changes
func (h *ClipboardHandler) Start() error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return nil
	}
	h.running = true
	h.mu.Unlock()

	// Get initial sequence number
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	h.lastSeq = uint32(seq)

	// Try to create a message-only window for clipboard notifications
	go h.runMessageLoop()

	// Also start a polling fallback
	go h.pollLoop()

	return nil
}

func (h *ClipboardHandler) runMessageLoop() {
	// Register window class
	className := syscall.StringToUTF16Ptr("SentinelClipboardMonitor")

	clipboardWndProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		switch msg {
		case WM_CLIPBOARDUPDATE:
			if clipboardHandler != nil {
				clipboardHandler.handleClipboardChange()
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

	// Create message-only window
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
		return
	}

	h.mu.Lock()
	h.hwnd = hwnd
	h.mu.Unlock()

	// Register for clipboard notifications
	procAddClipboardFormatListener.Call(hwnd)

	// Message loop
	var msg MSG
	for {
		select {
		case <-h.stopCh:
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

func (h *ClipboardHandler) pollLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkClipboardChange()
		}
	}
}

func (h *ClipboardHandler) checkClipboardChange() {
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	newSeq := uint32(seq)

	h.mu.Lock()
	if newSeq != h.lastSeq {
		h.lastSeq = newSeq
		h.mu.Unlock()
		h.handleClipboardChange()
	} else {
		h.mu.Unlock()
	}
}

func (h *ClipboardHandler) handleClipboardChange() {
	text, err := h.GetText()
	if err != nil {
		return
	}

	h.mu.Lock()
	if text == h.lastContent {
		h.mu.Unlock()
		return
	}
	h.lastContent = text
	h.mu.Unlock()

	if h.onChange != nil {
		h.onChange(ClipboardData{
			Text:      text,
			Timestamp: time.Now().UnixMilli(),
		})
	}
}

// GetText returns the current clipboard text
func (h *ClipboardHandler) GetText() (string, error) {
	// Check if text format is available
	ret, _, _ := procIsClipboardFormatAvailable.Call(CF_UNICODETEXT)
	if ret == 0 {
		return "", nil
	}

	ret, _, _ = procOpenClipboard.Call(0)
	if ret == 0 {
		return "", errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	handle, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if handle == 0 {
		return "", nil
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", errors.New("failed to lock clipboard data")
	}
	defer procGlobalUnlock.Call(handle)

	// Get size and read UTF-16 string
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return "", nil
	}

	// Convert UTF-16 to string
	utf16Slice := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:size/2:size/2]

	// Find null terminator
	var end int
	for i, c := range utf16Slice {
		if c == 0 {
			end = i
			break
		}
		end = i + 1
	}

	return string(utf16.Decode(utf16Slice[:end])), nil
}

// SetText sets the clipboard text
func (h *ClipboardHandler) SetText(text string) error {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return errors.New("failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	if text == "" {
		return nil
	}

	// Convert to UTF-16
	utf16Text := utf16.Encode([]rune(text + "\x00"))
	size := len(utf16Text) * 2

	// Allocate global memory
	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return errors.New("failed to allocate memory")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to lock memory")
	}

	// Copy data
	dst := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:len(utf16Text):len(utf16Text)]
	copy(dst, utf16Text)

	procGlobalUnlock.Call(hMem)

	// Set clipboard data
	ret, _, _ = procSetClipboardData.Call(CF_UNICODETEXT, hMem)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return errors.New("failed to set clipboard data")
	}

	// Update last content to prevent echo
	h.mu.Lock()
	h.lastContent = text
	h.mu.Unlock()

	return nil
}

// Stop stops the clipboard handler
func (h *ClipboardHandler) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	h.mu.Unlock()

	close(h.stopCh)
}
