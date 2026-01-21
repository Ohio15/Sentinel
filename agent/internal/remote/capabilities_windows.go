//go:build windows

package remote

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	shcore                     = syscall.NewLazyDLL("shcore.dll")
	dxgi                       = syscall.NewLazyDLL("dxgi.dll")
	d3d11                      = syscall.NewLazyDLL("d3d11.dll")
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	ntdll                      = syscall.NewLazyDLL("ntdll.dll")

	procEnumDisplayMonitors    = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
	procEnumDisplaySettingsW   = user32.NewProc("EnumDisplaySettingsW")
	procGetDpiForMonitor       = shcore.NewProc("GetDpiForMonitor")
	procCreateDXGIFactory1     = dxgi.NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice      = d3d11.NewProc("D3D11CreateDevice")
	procGetVersionExW          = kernel32.NewProc("GetVersionExW")
	procRtlGetVersion          = ntdll.NewProc("RtlGetVersion")
)

// MONITORINFOEX structure
type MONITORINFOEXW struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

// DEVMODE structure (partial)
type DEVMODEW struct {
	DmDeviceName       [32]uint16
	DmSpecVersion      uint16
	DmDriverVersion    uint16
	DmSize             uint16
	DmDriverExtra      uint16
	DmFields           uint32
	DmPosition         struct{ X, Y int32 }
	DmDisplayOrientation uint32
	DmDisplayFixedOutput uint32
	DmColor            int16
	DmDuplex           int16
	DmYResolution      int16
	DmTTOption         int16
	DmCollate          int16
	DmFormName         [32]uint16
	DmLogPixels        uint16
	DmBitsPerPel       uint32
	DmPelsWidth        uint32
	DmPelsHeight       uint32
	DmDisplayFlags     uint32
	DmDisplayFrequency uint32
	// Additional fields omitted for brevity
}

// OSVERSIONINFOEXW structure
type OSVERSIONINFOEXW struct {
	DwOSVersionInfoSize uint32
	DwMajorVersion      uint32
	DwMinorVersion      uint32
	DwBuildNumber       uint32
	DwPlatformId        uint32
	SzCSDVersion        [128]uint16
	WServicePackMajor   uint16
	WServicePackMinor   uint16
	WSuiteMask          uint16
	WProductType        byte
	WReserved           byte
}

const (
	MONITOR_DEFAULTTOPRIMARY = 0x00000001
	MONITORINFOF_PRIMARY     = 0x00000001
	ENUM_CURRENT_SETTINGS    = 0xFFFFFFFF
	MDT_EFFECTIVE_DPI        = 0
)

// DXGI GUIDs
var (
	IID_IDXGIFactory1 = GUID{0x770aae78, 0xf26f, 0x4dba, [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	IID_IDXGIAdapter  = GUID{0x2411e7e1, 0x12ac, 0x4ccf, [8]byte{0xbd, 0x14, 0x97, 0x98, 0xe8, 0x53, 0x4d, 0xc0}}
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// DXGI_ADAPTER_DESC structure
type DXGI_ADAPTER_DESC struct {
	Description           [128]uint16
	VendorId              uint32
	DeviceId              uint32
	SubSysId              uint32
	Revision              uint32
	DedicatedVideoMemory  uint64
	DedicatedSystemMemory uint64
	SharedSystemMemory    uint64
	AdapterLuid           struct{ LowPart, HighPart uint32 }
}

type monitorEnumData struct {
	screens []ScreenInfo
	index   int
}

func getSystemInfo() (osVersion string, gpuName string) {
	// Get OS version using RtlGetVersion (more reliable than GetVersionEx)
	var osvi OSVERSIONINFOEXW
	osvi.DwOSVersionInfoSize = uint32(unsafe.Sizeof(osvi))

	if procRtlGetVersion.Find() == nil {
		procRtlGetVersion.Call(uintptr(unsafe.Pointer(&osvi)))
	} else {
		procGetVersionExW.Call(uintptr(unsafe.Pointer(&osvi)))
	}

	osVersion = fmt.Sprintf("%d.%d.%d", osvi.DwMajorVersion, osvi.DwMinorVersion, osvi.DwBuildNumber)

	// Get GPU name via DXGI
	gpuName = getGPUName()

	return osVersion, gpuName
}

func getGPUName() string {
	if procCreateDXGIFactory1.Find() != nil {
		return "Unknown"
	}

	var factory uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&IID_IDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 {
		return "Unknown"
	}
	defer releaseComObject(factory)

	// Get first adapter
	var adapter uintptr
	vtable := *(*uintptr)(unsafe.Pointer(factory))
	enumAdapters := *(*uintptr)(unsafe.Pointer(vtable + 7*unsafe.Sizeof(uintptr(0))))
	hr, _, _ = syscall.SyscallN(enumAdapters, factory, 0, uintptr(unsafe.Pointer(&adapter)))
	if hr != 0 {
		return "Unknown"
	}
	defer releaseComObject(adapter)

	// Get adapter description
	var desc DXGI_ADAPTER_DESC
	vtable = *(*uintptr)(unsafe.Pointer(adapter))
	getDesc := *(*uintptr)(unsafe.Pointer(vtable + 8*unsafe.Sizeof(uintptr(0))))
	hr, _, _ = syscall.SyscallN(getDesc, adapter, uintptr(unsafe.Pointer(&desc)))
	if hr != 0 {
		return "Unknown"
	}

	return syscall.UTF16ToString(desc.Description[:])
}

func updateScreenInfoPlatform(screens []ScreenInfo) {
	for i := range screens {
		// Get refresh rate from display settings
		var dm DEVMODEW
		dm.DmSize = uint16(unsafe.Sizeof(dm))

		deviceName := make([]uint16, 32)
		if len(screens) > 1 {
			// Try to get device name for this monitor
			// For simplicity, we'll use the display index
		}

		ret, _, _ := procEnumDisplaySettingsW.Call(
			uintptr(unsafe.Pointer(&deviceName[0])),
			ENUM_CURRENT_SETTINGS,
			uintptr(unsafe.Pointer(&dm)),
		)

		if ret != 0 {
			screens[i].RefreshRate = int(dm.DmDisplayFrequency)
			screens[i].ColorDepth = int(dm.DmBitsPerPel)
		}

		// Get DPI scale
		screens[i].DPIScale = getDPIScale(i)
	}
}

func getDPIScale(monitorIndex int) float64 {
	if procGetDpiForMonitor.Find() != nil {
		return 1.0 // Fallback for older Windows
	}

	// Get monitor handle by index
	var monitors []uintptr
	callback := syscall.NewCallback(func(hMonitor, hdcMonitor, lprcMonitor, dwData uintptr) uintptr {
		monitors = append(monitors, hMonitor)
		return 1 // Continue enumeration
	})

	procEnumDisplayMonitors.Call(0, 0, callback, 0)

	if monitorIndex >= len(monitors) {
		return 1.0
	}

	var dpiX, dpiY uint32
	hr, _, _ := procGetDpiForMonitor.Call(
		monitors[monitorIndex],
		MDT_EFFECTIVE_DPI,
		uintptr(unsafe.Pointer(&dpiX)),
		uintptr(unsafe.Pointer(&dpiY)),
	)

	if hr != 0 {
		return 1.0
	}

	return float64(dpiX) / 96.0
}

func detectEncoders() []EncoderInfo {
	encoders := []EncoderInfo{}

	// Check for hardware encoders
	if hasNVENC() {
		encoders = append(encoders, EncoderInfo{
			Type:             "nvenc",
			MaxWidth:         4096,
			MaxHeight:        4096,
			MaxFPS:           120,
			SupportsHardware: true,
		})
	}

	if hasQuickSync() {
		encoders = append(encoders, EncoderInfo{
			Type:             "quicksync",
			MaxWidth:         4096,
			MaxHeight:        4096,
			MaxFPS:           120,
			SupportsHardware: true,
		})
	}

	if hasAMF() {
		encoders = append(encoders, EncoderInfo{
			Type:             "amf",
			MaxWidth:         4096,
			MaxHeight:        4096,
			MaxFPS:           120,
			SupportsHardware: true,
		})
	}

	// Media Foundation (always available on Windows)
	encoders = append(encoders, EncoderInfo{
		Type:             "mediafoundation",
		MaxWidth:         4096,
		MaxHeight:        4096,
		MaxFPS:           60,
		SupportsHardware: hasNVENC() || hasQuickSync() || hasAMF(), // MF uses hardware when available
	})

	// OpenH264 software fallback
	encoders = append(encoders, EncoderInfo{
		Type:             "openh264",
		MaxWidth:         4096,
		MaxHeight:        2160,
		MaxFPS:           60,
		SupportsHardware: false,
	})

	return encoders
}

func hasNVENC() bool {
	// Check for NVIDIA GPU by vendor ID
	gpuName := getGPUName()
	// Simple check - could be more sophisticated
	if len(gpuName) > 0 {
		for _, prefix := range []string{"NVIDIA", "GeForce", "Quadro", "Tesla"} {
			if len(gpuName) >= len(prefix) && gpuName[:len(prefix)] == prefix {
				return true
			}
		}
	}

	// Also check via DXGI vendor ID
	return checkVendorID(0x10DE) // NVIDIA vendor ID
}

func hasQuickSync() bool {
	// Check for Intel GPU
	return checkVendorID(0x8086) // Intel vendor ID
}

func hasAMF() bool {
	// Check for AMD GPU
	return checkVendorID(0x1002) // AMD vendor ID
}

func checkVendorID(vendorID uint32) bool {
	if procCreateDXGIFactory1.Find() != nil {
		return false
	}

	var factory uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&IID_IDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 {
		return false
	}
	defer releaseComObject(factory)

	// Enumerate adapters looking for the vendor
	vtable := *(*uintptr)(unsafe.Pointer(factory))
	enumAdapters := *(*uintptr)(unsafe.Pointer(vtable + 7*unsafe.Sizeof(uintptr(0))))

	for i := uint32(0); ; i++ {
		var adapter uintptr
		hr, _, _ = syscall.SyscallN(enumAdapters, factory, uintptr(i), uintptr(unsafe.Pointer(&adapter)))
		if hr != 0 {
			break
		}

		var desc DXGI_ADAPTER_DESC
		vtable = *(*uintptr)(unsafe.Pointer(adapter))
		getDesc := *(*uintptr)(unsafe.Pointer(vtable + 8*unsafe.Sizeof(uintptr(0))))
		syscall.SyscallN(getDesc, adapter, uintptr(unsafe.Pointer(&desc)))
		releaseComObject(adapter)

		if desc.VendorId == vendorID {
			return true
		}
	}

	return false
}

func detectDXGICapture() bool {
	// DXGI Desktop Duplication requires Windows 8+
	var osvi OSVERSIONINFOEXW
	osvi.DwOSVersionInfoSize = uint32(unsafe.Sizeof(osvi))

	if procRtlGetVersion.Find() == nil {
		procRtlGetVersion.Call(uintptr(unsafe.Pointer(&osvi)))
	}

	// Windows 8 is version 6.2
	return osvi.DwMajorVersion > 6 || (osvi.DwMajorVersion == 6 && osvi.DwMinorVersion >= 2)
}

func detectHardwareEncode() bool {
	return hasNVENC() || hasQuickSync() || hasAMF()
}

func releaseComObject(obj uintptr) {
	if obj == 0 {
		return
	}
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	release := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	syscall.SyscallN(release, obj)
}
