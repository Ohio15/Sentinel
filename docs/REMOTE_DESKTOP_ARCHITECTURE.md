# Sentinel Remote Desktop - Complete Architecture Specification

**Version:** 1.0.0
**Status:** Implementation Ready
**Last Updated:** 2026-02-01

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [System Overview](#2-system-overview)
3. [Screen Capture Subsystem](#3-screen-capture-subsystem)
4. [Video Encoding Pipeline](#4-video-encoding-pipeline)
5. [Cursor Subsystem](#5-cursor-subsystem)
6. [Input Handling Architecture](#6-input-handling-architecture)
7. [Transport Layer Architecture](#7-transport-layer-architecture)
8. [Session Management](#8-session-management)
9. [Quality Adaptation System](#9-quality-adaptation-system)
10. [Client Viewer Architecture](#10-client-viewer-architecture)
11. [Security Architecture](#11-security-architecture)
12. [Platform Abstraction Layer](#12-platform-abstraction-layer)
13. [Performance Targets](#13-performance-targets)
14. [Error Handling Matrix](#14-error-handling-matrix)
15. [Implementation Roadmap](#15-implementation-roadmap)

---

## 1. Executive Summary

This document specifies the complete architecture for Sentinel's Remote Desktop subsystem, designed to match or exceed commercial solutions (TeamViewer, AnyDesk, RustDesk) in functionality and performance.

### Key Design Principles

1. **Latency First**: Target <100ms end-to-end latency under normal conditions
2. **Adaptive Quality**: Dynamic bitrate/resolution based on network conditions
3. **Hardware Acceleration**: Prefer GPU encoding/decoding when available
4. **Platform Agnostic**: Clean abstractions for Windows, Linux, macOS support
5. **Security by Default**: E2E encryption, zero-trust session management

### Current Implementation Status

| Component | Status | Location |
|-----------|--------|----------|
| DXGI Screen Capture | ✅ Implemented | `agent/internal/capture/dxgi_windows.go` |
| OpenH264 Encoding | ✅ Implemented | `agent/internal/webrtc/webrtc.go` |
| WebRTC Transport | ✅ Implemented | `agent/internal/webrtc/webrtc.go` |
| Input Injection (Windows) | ✅ Implemented | `agent/internal/desktop/helper/input_windows.go` |
| Cursor Tracking | ✅ Implemented | `agent/internal/webrtc/webrtc.go` |
| Capture Strategy | ✅ Implemented | `agent/internal/webrtc/capture_strategy.go` |
| Hardware Encoding (MF) | 🔄 Partial | `agent/internal/encoder/mf_windows.go` |
| Linux/macOS Support | ❌ Not Started | - |
| Clipboard Sync | ❌ Not Started | - |
| Audio Streaming | ❌ Not Started | - |

---

## 2. System Overview

### 2.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           SENTINEL REMOTE DESKTOP                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                        AGENT (Remote Machine)                          │ │
│  │                                                                         │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │ │
│  │  │   Screen     │  │    Video     │  │   Cursor     │  │   Input    │ │ │
│  │  │   Capture    │──│   Encoder    │──│  Subsystem   │  │  Injector  │ │ │
│  │  │  Subsystem   │  │   Pipeline   │  │              │  │            │ │ │
│  │  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └─────▲──────┘ │ │
│  │         │                 │                 │                │        │ │
│  │         └─────────────────┼─────────────────┘                │        │ │
│  │                           │                                  │        │ │
│  │                    ┌──────▼──────┐                           │        │ │
│  │                    │  Transport  │◄──────────────────────────┘        │ │
│  │                    │    Layer    │                                    │ │
│  │                    │  (WebRTC)   │                                    │ │
│  │                    └──────┬──────┘                                    │ │
│  └───────────────────────────┼────────────────────────────────────────────┘ │
│                              │                                              │
│                              │ DTLS-SRTP / DataChannel                      │
│                              │                                              │
│  ┌───────────────────────────┼────────────────────────────────────────────┐ │
│  │                           │      VIEWER (Dashboard/Client)             │ │
│  │                    ┌──────▼──────┐                                     │ │
│  │                    │  Transport  │                                     │ │
│  │                    │    Layer    │                                     │ │
│  │                    │  (WebRTC)   │                                     │ │
│  │                    └──────┬──────┘                                     │ │
│  │         ┌─────────────────┼─────────────────┐                          │ │
│  │         │                 │                 │                          │ │
│  │  ┌──────▼───────┐  ┌──────▼───────┐  ┌──────▼───────┐  ┌────────────┐ │ │
│  │  │    Video     │  │   Cursor     │  │    Input     │  │  Clipboard │ │ │
│  │  │   Decoder    │──│   Renderer   │  │   Capture    │  │    Sync    │ │ │
│  │  │              │  │              │  │              │  │            │ │ │
│  │  └──────┬───────┘  └──────────────┘  └──────────────┘  └────────────┘ │ │
│  │         │                                                              │ │
│  │  ┌──────▼───────┐                                                      │ │
│  │  │   Canvas/    │                                                      │ │
│  │  │   WebGL      │                                                      │ │
│  │  │   Renderer   │                                                      │ │
│  │  └──────────────┘                                                      │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Interaction Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              DATA FLOW OVERVIEW                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  AGENT SIDE (Go)                           VIEWER SIDE (TypeScript)         │
│  ══════════════                            ════════════════════════         │
│                                                                              │
│  [Display]                                              [Canvas]            │
│      │                                                      ▲               │
│      ▼                                                      │               │
│  ┌────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐   │               │
│  │ DXGI   │───►│ YCbCr   │───►│ H.264   │───►│  RTP    │───┼──► Video      │
│  │Capture │    │ Convert │    │ Encode  │    │ Packets │   │    Track      │
│  └────────┘    └─────────┘    └─────────┘    └─────────┘   │               │
│      │              │              │              │         │               │
│      │         [Frame Queue]  [NAL Queue]   [Send Queue]   │               │
│      │                                                      │               │
│      ▼                                                      │               │
│  ┌────────┐    ┌─────────────────────────────────────────┐ │               │
│  │Cursor  │───►│            DataChannel (JSON)           │─┼──► Cursor     │
│  │ Info   │    │  {type:"cursor", x:N, y:N, shape:"..."}│ │    Overlay    │
│  └────────┘    └─────────────────────────────────────────┘ │               │
│                                                             │               │
│                ┌─────────────────────────────────────────┐ │               │
│  ┌────────┐    │            DataChannel (JSON)           │◄┼─── Input      │
│  │SendInput│◄──│  {type:"mouse", x:N, y:N, button:N}    │ │    Events     │
│  └────────┘    └─────────────────────────────────────────┘ │               │
│                                                             │               │
│  ══════════════════════════════════════════════════════════╧═══════════════│
│                         WebRTC PeerConnection                               │
│                    (ICE/DTLS-SRTP/SCTP encrypted)                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Screen Capture Subsystem

### 3.1 Platform-Specific Capture Methods

| Platform | Primary Method | Fallback | Hardware Accel |
|----------|---------------|----------|----------------|
| Windows 10/11 | DXGI Desktop Duplication | GDI BitBlt | Yes (GPU texture) |
| Linux X11 | XShm + XComposite | X11 GetImage | No |
| Linux Wayland | PipeWire + DMA-BUF | Portal Screenshot | Yes (DMA-BUF) |
| macOS | CGDisplayStream | CGWindowListCreateImage | Yes (IOSurface) |

### 3.2 Windows DXGI Capture Pipeline

**Current Implementation:** `agent/internal/capture/dxgi_windows.go`

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         DXGI DESKTOP DUPLICATION                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌───────────┐ │
│  │ D3D11Device  │───►│ IDXGIOutput1 │───►│OutputDuplicat│───►│ Desktop   │ │
│  │   Create     │    │    Query     │    │    ion       │    │ Texture   │ │
│  └──────────────┘    └──────────────┘    └──────────────┘    └─────┬─────┘ │
│                                                                     │       │
│                                                                     ▼       │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌───────────┐ │
│  │  Frame       │◄───│   Map to     │◄───│  Copy to     │◄───│  Acquire  │ │
│  │  Buffer      │    │    CPU       │    │  Staging Tex │    │NextFrame  │ │
│  │  (BGRA)      │    │  (Mapped)    │    │  (GPU→CPU)   │    │           │ │
│  └──────────────┘    └──────────────┘    └──────────────┘    └───────────┘ │
│         │                                                                   │
│         ▼                                                                   │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                        DIRTY RECTANGLES                               │  │
│  │  GetFrameDirtyRects() → Only encode changed regions                   │  │
│  │  Reduces bandwidth by 50-90% for static content                       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.3 Capture Interface Definition

```go
// IScreenCapture defines the platform abstraction for screen capture
type IScreenCapture interface {
    // Initialize sets up capture for the specified monitor
    Initialize(monitorIndex int) error

    // CaptureFrame captures a single frame with timeout
    // Returns nil if no changes (dirty rect optimization)
    CaptureFrame(timeoutMs int) (*CapturedFrame, error)

    // GetDimensions returns current capture dimensions
    GetDimensions() (width, height int)

    // GetMonitorInfo returns information about available monitors
    GetMonitorInfo() []MonitorInfo

    // SetRegion restricts capture to a specific region (optional)
    SetRegion(x, y, width, height int) error

    // Release frees all resources
    Release()
}

// CapturedFrame contains frame data and metadata
type CapturedFrame struct {
    Data       []byte            // Pixel data (BGRA format)
    Width      int
    Height     int
    Stride     int               // Bytes per row
    DirtyRects []image.Rectangle // Changed regions only
    Timestamp  time.Time
    FrameID    uint64            // Monotonic frame counter
}

// MonitorInfo describes a display
type MonitorInfo struct {
    Index       int
    Name        string
    Bounds      image.Rectangle
    IsPrimary   bool
    ScaleFactor float64          // DPI scaling (1.0 = 100%)
}
```

### 3.4 Frame Rate Management

```go
// FrameRateController manages adaptive frame rate
type FrameRateController struct {
    TargetFPS      int           // Desired FPS (10-60)
    CurrentFPS     int           // Actual achieved FPS
    MinFPS         int           // Floor (never go below)
    MaxFPS         int           // Ceiling (never exceed)

    frameTimes     []time.Duration // Rolling window
    lastCapture    time.Time
    adaptiveMode   bool
}

// Quality presets
var QualityPresets = map[string]FrameRateConfig{
    "low":      {FPS: 10, MaxBitrate: 800_000},
    "medium":   {FPS: 20, MaxBitrate: 2_000_000},
    "high":     {FPS: 30, MaxBitrate: 4_000_000},
    "ultra":    {FPS: 60, MaxBitrate: 8_000_000},
}
```

### 3.5 Multi-Monitor Support

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MULTI-MONITOR HANDLING                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Virtual Desktop (Unified Coordinate Space)                                 │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                                                                      │   │
│  │   ┌──────────────────┐     ┌──────────────────┐                     │   │
│  │   │   Monitor 0      │     │   Monitor 1      │                     │   │
│  │   │   (Primary)      │     │                  │                     │   │
│  │   │   1920x1080      │     │   2560x1440      │                     │   │
│  │   │   @ 0,0          │     │   @ 1920,0       │                     │   │
│  │   │                  │     │                  │                     │   │
│  │   └──────────────────┘     └──────────────────┘                     │   │
│  │                                                                      │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  Capture Modes:                                                             │
│  1. Single Monitor  - Capture one display only                              │
│  2. All Monitors    - Capture entire virtual desktop                        │
│  3. Window          - Capture specific window (future)                      │
│  4. Region          - Capture user-defined rectangle                        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. Video Encoding Pipeline

### 4.1 Encoder Selection Hierarchy

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        ENCODER SELECTION PRIORITY                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  1. NVIDIA NVENC (if NVIDIA GPU present)                                    │
│     └─► H.264/H.265, lowest latency, best quality/perf ratio               │
│                                                                              │
│  2. Intel Quick Sync (if Intel CPU/iGPU)                                    │
│     └─► H.264/H.265, good latency, widely available                        │
│                                                                              │
│  3. AMD VCE/VCN (if AMD GPU present)                                        │
│     └─► H.264/H.265, good performance                                       │
│                                                                              │
│  4. Windows Media Foundation (if hardware support)                          │
│     └─► Abstraction layer, auto-selects best HW encoder                    │
│                                                                              │
│  5. OpenH264 (software fallback - ALWAYS available)                         │
│     └─► H.264 only, CPU intensive but guaranteed to work                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.2 VideoEncoder Interface

**Current Implementation:** `agent/internal/webrtc/encoder_interface.go`

```go
// VideoEncoder is the interface for H.264 video encoders
type VideoEncoder interface {
    // Encode encodes a YCbCr 4:2:0 frame to H.264 NAL units
    Encode(ycbcr *image.YCbCr) ([]byte, error)

    // EncodeBGRA encodes BGRA directly (hardware encoders only)
    EncodeBGRA(bgra []byte, width, height, stride int, forceKeyframe bool) ([]byte, error)

    // ForceKeyframe forces next frame to be an I-frame
    ForceKeyframe()

    // SetBitrate adjusts target bitrate dynamically
    SetBitrate(bps int) error

    // SetFrameRate adjusts target frame rate
    SetFrameRate(fps int) error

    // GetWidth returns configured width
    GetWidth() int

    // GetHeight returns configured height
    GetHeight() int

    // IsHardware returns true if hardware accelerated
    IsHardware() bool

    // GetCodec returns the codec type
    GetCodec() CodecType

    // Close releases encoder resources
    Close()
}

type CodecType int
const (
    CodecH264 CodecType = iota
    CodecH265
    CodecVP8
    CodecVP9
    CodecAV1
)
```

### 4.3 Encoding Parameters

```go
// EncoderConfig defines encoding parameters
type EncoderConfig struct {
    // Resolution
    Width  int
    Height int

    // Rate Control
    Bitrate        int     // Target bitrate (bps)
    MaxBitrate     int     // Maximum bitrate (bps)
    MinBitrate     int     // Minimum bitrate (bps)
    RateControlMode RCMode // CBR, VBR, CQP

    // Frame Settings
    FrameRate      int     // Target FPS
    KeyframeInterval int   // Frames between I-frames (GOP size)
    BFrames        int     // Number of B-frames (0 for low latency)

    // Quality
    Profile        H264Profile // Baseline, Main, High
    Level          string      // "4.1", "5.1", etc.
    Preset         string      // "ultrafast", "fast", "medium"
    Tune           string      // "zerolatency", "film", etc.

    // Hardware Specific
    UseHardware    bool
    PreferredHW    HWEncoderType
}

type RCMode int
const (
    RCModeCBR RCMode = iota // Constant Bitrate
    RCModeVBR               // Variable Bitrate
    RCModeCQP               // Constant QP (quality)
)

// Recommended settings for remote desktop
var LowLatencyConfig = EncoderConfig{
    RateControlMode:  RCModeCBR,
    KeyframeInterval: 60,        // Keyframe every 2 seconds at 30fps
    BFrames:          0,         // No B-frames for lowest latency
    Profile:          ProfileBaseline,
    Preset:           "ultrafast",
    Tune:             "zerolatency",
}
```

### 4.4 Frame Queue Management

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          FRAME QUEUE PIPELINE                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Capture          Convert           Encode            Send                 │
│   Thread           Thread            Thread            Thread               │
│      │                │                 │                │                  │
│      ▼                ▼                 ▼                ▼                  │
│  ┌───────┐        ┌───────┐        ┌───────┐        ┌───────┐              │
│  │Frame 1│───────►│YCbCr 1│───────►│ NAL 1 │───────►│ RTP 1 │──► Network  │
│  ├───────┤        ├───────┤        ├───────┤        ├───────┤              │
│  │Frame 2│        │YCbCr 2│        │ NAL 2 │        │ RTP 2 │              │
│  ├───────┤        ├───────┤        ├───────┤        ├───────┤              │
│  │Frame 3│        │YCbCr 3│        │ NAL 3 │        │ RTP 3 │              │
│  └───────┘        └───────┘        └───────┘        └───────┘              │
│                                                                              │
│  Queue Sizes (configurable):                                                │
│  - Capture Queue: 2 frames (minimize memory, drop oldest if full)          │
│  - Encode Queue:  3 frames (buffer for encoder latency)                    │
│  - Send Queue:    Unlimited (network handles backpressure via congestion)  │
│                                                                              │
│  Backpressure Handling:                                                     │
│  - If encode queue full → drop oldest unencoded frame                      │
│  - If network slow → reduce bitrate/fps, not queue frames                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.5 Color Space Conversion

```go
// rgbaToYCbCr converts RGBA to YCbCr 4:2:0 using ITU-R BT.601
// This is the standard for H.264 video
func rgbaToYCbCr(rgba *image.RGBA) *image.YCbCr {
    // Y  =  16 + 65.481*R + 128.553*G + 24.966*B  (scaled to 0-255)
    // Cb = 128 - 37.797*R - 74.203*G  + 112.0*B
    // Cr = 128 + 112.0*R  - 93.786*G  - 18.214*B

    // 4:2:0 subsampling: Cb/Cr sampled at half resolution
    // Every 2x2 block of Y shares one Cb and one Cr value
}

// For hardware encoders that accept BGRA directly:
// - NVENC: NV12 (Y plane + interleaved UV)
// - Media Foundation: NV12 or BGRA (auto-converted)
// - OpenH264: I420 (Y + U + V planar)
```

---

## 5. Cursor Subsystem

### 5.1 Cursor Data Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CURSOR SUBSYSTEM                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  AGENT SIDE                                                                 │
│  ══════════                                                                 │
│                                                                              │
│  ┌────────────────┐     ┌────────────────┐     ┌────────────────┐          │
│  │ GetCursorInfo  │────►│ Cursor Cache   │────►│ DataChannel    │          │
│  │ (60Hz polling) │     │ (deduplicate)  │     │ (JSON message) │          │
│  └────────────────┘     └────────────────┘     └────────────────┘          │
│         │                      │                      │                     │
│         ▼                      ▼                      ▼                     │
│  ┌────────────────┐     ┌────────────────┐     ┌────────────────┐          │
│  │ Position (x,y) │     │ Shape Type     │     │ Custom Cursor  │          │
│  │ Updated every  │     │ (hash-based    │     │ (base64 PNG    │          │
│  │ 16ms if moved  │     │  comparison)   │     │  when custom)  │          │
│  └────────────────┘     └────────────────┘     └────────────────┘          │
│                                                                              │
│  ─────────────────────────── Network ───────────────────────────────────── │
│                                                                              │
│  VIEWER SIDE                                                                │
│  ═══════════                                                                │
│                                                                              │
│  ┌────────────────┐     ┌────────────────┐     ┌────────────────┐          │
│  │ Local Cursor   │◄────│ Position       │◄────│ DataChannel    │          │
│  │ Prediction     │     │ Interpolation  │     │ (receive)      │          │
│  └────────────────┘     └────────────────┘     └────────────────┘          │
│         │                                                                   │
│         ▼                                                                   │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │                    CURSOR RENDERING OPTIONS                         │    │
│  │                                                                     │    │
│  │  Option 1: CSS cursor (lowest latency, uses browser native)        │    │
│  │  Option 2: Canvas overlay (custom cursors, synchronized)           │    │
│  │  Option 3: Embedded in video (highest accuracy, more latency)      │    │
│  │                                                                     │    │
│  │  Current: CSS cursor with shape synchronization                    │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Cursor Message Protocol

```typescript
// Position update (sent at 60Hz when cursor moves)
interface CursorPositionMessage {
    type: "cursor";
    x: number;        // Screen X coordinate
    y: number;        // Screen Y coordinate
    visible: boolean; // Cursor visibility state
}

// Shape change (sent only when cursor shape changes)
interface CursorShapeMessage {
    type: "cursorShape";
    shape: {
        type: "default" | "pointer" | "text" | "wait" | "crosshair" |
              "move" | "not-allowed" | "ew-resize" | "ns-resize" |
              "nwse-resize" | "nesw-resize" | "custom";
        hotspot: { x: number; y: number };
        image?: string;  // Base64 PNG for custom cursors
        width?: number;  // Custom cursor dimensions
        height?: number;
    };
}
```

### 5.3 Cursor Shape Detection (Windows)

**Current Implementation:** `agent/internal/webrtc/webrtc.go:1272-1354`

```go
// Standard Windows cursor IDs mapped to CSS cursor types
var standardCursors = map[uintptr]string{
    IDC_ARROW:    "default",
    IDC_IBEAM:    "text",
    IDC_WAIT:     "wait",
    IDC_CROSS:    "crosshair",
    IDC_HAND:     "pointer",
    IDC_SIZEALL:  "move",
    IDC_NO:       "not-allowed",
    IDC_SIZENWSE: "nwse-resize",
    IDC_SIZENESW: "nesw-resize",
    IDC_SIZEWE:   "ew-resize",
    IDC_SIZENS:   "ns-resize",
}

// getCursorShape returns the current cursor type
func getCursorShape() string {
    var ci cursorInfo
    ci.CbSize = uint32(unsafe.Sizeof(ci))
    GetCursorInfo(&ci)

    if name, ok := standardCursors[ci.HCursor]; ok {
        return name
    }
    return "default"
}
```

### 5.4 Local Cursor Prediction

To eliminate perceived latency, the viewer renders the cursor locally:

```typescript
class CursorPredictor {
    private lastPosition: {x: number, y: number};
    private lastServerUpdate: number;
    private velocity: {vx: number, vy: number};

    // Called on local mouse move
    onLocalMove(x: number, y: number) {
        // Show cursor immediately at local position
        this.renderCursor(x, y);
    }

    // Called when server position arrives
    onServerUpdate(serverX: number, serverY: number) {
        // Smoothly reconcile if difference is small
        // Snap if difference is large (user moved quickly)
        const diff = Math.hypot(serverX - this.lastPosition.x,
                                serverY - this.lastPosition.y);

        if (diff < 50) {
            this.interpolateTo(serverX, serverY);
        } else {
            this.snapTo(serverX, serverY);
        }
    }
}
```

---

## 6. Input Handling Architecture

### 6.1 Input Pipeline Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         INPUT HANDLING PIPELINE                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  VIEWER (Browser)                                                           │
│  ════════════════                                                           │
│                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │
│  │ DOM Event    │───►│ Normalize &  │───►│ Coordinate   │                  │
│  │ Listeners    │    │ Validate     │    │ Transform    │                  │
│  │ (mouse/kbd)  │    │              │    │ (viewport→   │                  │
│  └──────────────┘    └──────────────┘    │  screen)     │                  │
│                                          └──────┬───────┘                  │
│                                                 │                           │
│                                                 ▼                           │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │              DataChannel.send(JSON.stringify(event))                  │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                   │                                         │
│  ─────────────────────────────────┼─────────────────────────────────────── │
│                                   │ Network                                 │
│  ─────────────────────────────────┼─────────────────────────────────────── │
│                                   ▼                                         │
│  AGENT (Windows)                                                            │
│  ═══════════════                                                            │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │              DataChannel.OnMessage(callback)                          │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                   │                                         │
│                                   ▼                                         │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │
│  │ JSON Parse   │───►│ Coordinate   │───►│ SendInput    │                  │
│  │ & Validate   │    │ Transform    │    │ API Call     │                  │
│  │              │    │ (DPI aware)  │    │              │                  │
│  └──────────────┘    └──────────────┘    └──────────────┘                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 Input Event Protocol

```typescript
// Mouse event structure
interface MouseInputEvent {
    type: "move" | "down" | "up" | "wheel";
    x: number;           // X coordinate in video frame space
    y: number;           // Y coordinate in video frame space
    button?: number;     // 0=left, 1=middle, 2=right, 3=x1, 4=x2
    deltaY?: number;     // Wheel delta (positive=down, negative=up)
    deltaX?: number;     // Horizontal scroll (if supported)
}

// Keyboard event structure
interface KeyboardInputEvent {
    type: "keydown" | "keyup";
    key: string;         // Key value ("a", "Enter", "F1", etc.)
    code: string;        // Physical key code ("KeyA", "Enter", "F1")
    modifiers?: {
        ctrl: boolean;
        alt: boolean;
        shift: boolean;
        meta: boolean;   // Windows key / Command key
    };
}

// Special sequences
interface SpecialInputEvent {
    type: "special";
    action: "ctrl-alt-del" | "alt-tab" | "win-key" | "print-screen";
}
```

### 6.3 Coordinate Transformation

```go
// Coordinate transformation chain:
// 1. Browser viewport coords → Video element coords
// 2. Video element coords → Remote screen coords
// 3. Remote screen coords → Virtual desktop coords (multi-monitor)

type CoordinateTransformer struct {
    // Video dimensions (what we're encoding)
    videoWidth, videoHeight int

    // Remote screen dimensions
    screenWidth, screenHeight int

    // Screen offset in virtual desktop (multi-monitor)
    screenOffsetX, screenOffsetY int

    // DPI scale factor
    dpiScale float64
}

func (t *CoordinateTransformer) Transform(inputX, inputY float64) (screenX, screenY int) {
    // Scale from video space to screen space
    scaleX := float64(t.screenWidth) / float64(t.videoWidth)
    scaleY := float64(t.screenHeight) / float64(t.videoHeight)

    screenX = int(inputX*scaleX) + t.screenOffsetX
    screenY = int(inputY*scaleY) + t.screenOffsetY

    return screenX, screenY
}
```

### 6.4 Windows Input Injection

**Current Implementation:** `agent/internal/desktop/helper/input_windows.go`

```go
// Mouse input using SendInput API (preferred over mouse_event)
func injectMouseClick(screenX, screenY int, button int, down bool) {
    // First, move cursor to position
    SetCursorPos(screenX, screenY)

    // Then send button event
    var flags uint32
    if down {
        switch button {
        case 0: flags = MOUSEEVENTF_LEFTDOWN
        case 1: flags = MOUSEEVENTF_MIDDLEDOWN
        case 2: flags = MOUSEEVENTF_RIGHTDOWN
        }
    } else {
        switch button {
        case 0: flags = MOUSEEVENTF_LEFTUP
        case 1: flags = MOUSEEVENTF_MIDDLEUP
        case 2: flags = MOUSEEVENTF_RIGHTUP
        }
    }

    input := INPUT_MOUSE{
        Type: INPUT_TYPE_MOUSE,
        Mi: MOUSEINPUT{
            DwFlags: flags,
        },
    }
    SendInput(1, &input, sizeof(input))
}

// Keyboard input with proper scancode handling
func injectKeyPress(vk uint16, down bool) {
    var flags uint32
    if !down {
        flags |= KEYEVENTF_KEYUP
    }

    // Extended keys need special flag
    if isExtendedKey(vk) {
        flags |= KEYEVENTF_EXTENDEDKEY
    }

    input := INPUT_KBD{
        Type: INPUT_TYPE_KEYBOARD,
        Ki: KEYBDINPUT{
            Vk:      vk,
            Scan:    MapVirtualKey(vk, 0),
            DwFlags: flags,
        },
    }
    SendInput(1, &input, sizeof(input))
}
```

### 6.5 Special Key Handling

```go
// Ctrl+Alt+Del requires special handling (SAS - Secure Attention Sequence)
// Cannot be sent via normal SendInput - requires either:
// 1. Running as SYSTEM with SeTcbPrivilege
// 2. Using SendSAS() from sas.dll (Windows 7+)
// 3. Physical keyboard simulation via driver

func sendCtrlAltDel() error {
    // Method 1: Try SAS library
    if sasLib, err := syscall.LoadDLL("sas.dll"); err == nil {
        if sendSAS, err := sasLib.FindProc("SendSAS"); err == nil {
            sendSAS.Call(0) // FALSE = don't require Ctrl+Alt+Del screen
            return nil
        }
    }

    // Method 2: Simulate via accessibility APIs
    // This works for launching task manager but not true SAS
    return simulateCtrlAltDelViaAccessibility()
}

// Alt+Tab interception
// Normally blocked by UIPI (User Interface Privilege Isolation)
// Solution: Run desktop process in user session, not as service
```

### 6.6 Platform Abstraction for Input

```go
// IInputInjector defines platform-agnostic input injection
type IInputInjector interface {
    // Mouse operations
    MoveMouse(x, y int) error
    MouseDown(button int) error
    MouseUp(button int) error
    MouseWheel(deltaX, deltaY int) error

    // Keyboard operations
    KeyDown(keyCode string, modifiers Modifiers) error
    KeyUp(keyCode string, modifiers Modifiers) error
    TypeText(text string) error

    // Special sequences
    SendSpecialSequence(seq SpecialSequence) error

    // Configuration
    SetDPIScale(scale float64)
    SetScreenOffset(x, y int)
}

type SpecialSequence int
const (
    SeqCtrlAltDel SpecialSequence = iota
    SeqAltTab
    SeqAltF4
    SeqWinKey
    SeqPrintScreen
)
```

---

## 7. Transport Layer Architecture

### 7.1 WebRTC Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         WEBRTC TRANSPORT LAYER                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                         PEER CONNECTION                                  ││
│  │                                                                          ││
│  │  ┌────────────────────────────────────────────────────────────────────┐ ││
│  │  │                    ICE (Interactive Connectivity Establishment)     │ ││
│  │  │                                                                     │ ││
│  │  │  STUN Servers:           TURN Servers (relay fallback):            │ ││
│  │  │  • stun.l.google.com     • relay.metered.ca (UDP/TCP/TLS)          │ ││
│  │  │  • stun1.l.google.com    • Custom TURN (Sentinel infrastructure)   │ ││
│  │  │                                                                     │ ││
│  │  │  Candidate Types:                                                   │ ││
│  │  │  • host     - Direct local network                                 │ ││
│  │  │  • srflx    - Server reflexive (NAT public address)               │ ││
│  │  │  • relay    - TURN relay (firewall traversal)                     │ ││
│  │  └────────────────────────────────────────────────────────────────────┘ ││
│  │                                                                          ││
│  │  ┌────────────────────────────────────────────────────────────────────┐ ││
│  │  │                         DTLS-SRTP                                   │ ││
│  │  │                                                                     │ ││
│  │  │  • Encryption: AES-128-GCM or AES-256-GCM                          │ ││
│  │  │  • Key Exchange: DTLS 1.2 handshake                                │ ││
│  │  │  • Perfect Forward Secrecy via ephemeral keys                      │ ││
│  │  └────────────────────────────────────────────────────────────────────┘ ││
│  │                                                                          ││
│  │  ┌─────────────────────┐  ┌─────────────────────┐                      ││
│  │  │    MEDIA TRACK      │  │    DATA CHANNEL     │                      ││
│  │  │                     │  │                     │                      ││
│  │  │  • Video (H.264)    │  │  • Input events     │                      ││
│  │  │  • Audio (Opus)     │  │  • Cursor updates   │                      ││
│  │  │  • RTP/RTCP         │  │  • Control messages │                      ││
│  │  │  • Congestion ctrl  │  │  • SCTP (reliable)  │                      ││
│  │  └─────────────────────┘  └─────────────────────┘                      ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.2 Signaling Protocol

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         SIGNALING FLOW (WebSocket)                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  VIEWER                    SERVER                    AGENT                  │
│     │                         │                         │                   │
│     │  1. Request Session     │                         │                   │
│     │ ───────────────────────►│                         │                   │
│     │                         │  2. Create Session      │                   │
│     │                         │ ───────────────────────►│                   │
│     │                         │                         │                   │
│     │                         │  3. Session Ready       │                   │
│     │                         │ ◄───────────────────────│                   │
│     │  4. Session Created     │                         │                   │
│     │ ◄───────────────────────│                         │                   │
│     │                         │                         │                   │
│     │  5. SDP Offer           │                         │                   │
│     │ ───────────────────────►│  6. Forward Offer       │                   │
│     │                         │ ───────────────────────►│                   │
│     │                         │                         │                   │
│     │                         │  7. SDP Answer          │                   │
│     │                         │ ◄───────────────────────│                   │
│     │  8. Forward Answer      │                         │                   │
│     │ ◄───────────────────────│                         │                   │
│     │                         │                         │                   │
│     │  9. ICE Candidates (trickle)                      │                   │
│     │ ◄────────────────────────────────────────────────►│                   │
│     │                         │                         │                   │
│     │  ═══════════════════════════════════════════════  │                   │
│     │           DIRECT P2P CONNECTION ESTABLISHED       │                   │
│     │  ═══════════════════════════════════════════════  │                   │
│     │                                                   │                   │
│     │  10. Video/Audio RTP  ◄═══════════════════════════│                   │
│     │      Data Channel     ◄═══════════════════════════│                   │
│     │                                                   │                   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.3 Signaling Message Types

```typescript
// Signaling messages (sent via WebSocket to server)
type SignalingMessage =
    | { type: "session.create"; deviceId: string; quality: string }
    | { type: "session.join"; sessionId: string }
    | { type: "session.end"; sessionId: string }
    | { type: "sdp.offer"; sessionId: string; sdp: string }
    | { type: "sdp.answer"; sessionId: string; sdp: string }
    | { type: "ice.candidate"; sessionId: string; candidate: RTCIceCandidateInit }
    | { type: "session.error"; sessionId: string; error: string };

// Server-to-client messages
type ServerMessage =
    | { type: "session.created"; sessionId: string; iceServers: RTCIceServer[] }
    | { type: "session.ready"; sessionId: string }
    | { type: "sdp.offer"; sessionId: string; sdp: string }
    | { type: "sdp.answer"; sessionId: string; sdp: string }
    | { type: "ice.candidate"; sessionId: string; candidate: RTCIceCandidateInit }
    | { type: "session.ended"; sessionId: string; reason: string };
```

### 7.4 ICE Configuration

```go
// ICE server configuration
var DefaultICEServers = []webrtc.ICEServer{
    // Public STUN servers
    {URLs: []string{"stun:stun.l.google.com:19302"}},
    {URLs: []string{"stun:stun1.l.google.com:19302"}},

    // TURN servers for NAT traversal
    {
        URLs:       []string{
            "turn:a.relay.metered.ca:80",
            "turn:a.relay.metered.ca:80?transport=tcp",
            "turn:a.relay.metered.ca:443",
            "turn:a.relay.metered.ca:443?transport=tcp",
        },
        Username:   "configured-username",
        Credential: "configured-credential",
    },
}

// ICE settings for reliability
settingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled) // Avoid .local resolution issues
settingEngine.SetICETimeouts(
    5*time.Second,   // Disconnected timeout
    25*time.Second,  // Failed timeout
    2*time.Second,   // Keepalive interval
)
```

### 7.5 WebSocket Fallback

For environments where WebRTC is blocked:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      WEBSOCKET FALLBACK TRANSPORT                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  When WebRTC fails (corporate firewalls, symmetric NAT):                    │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                      BINARY WEBSOCKET PROTOCOL                          │ │
│  │                                                                         │ │
│  │  Frame Header (8 bytes):                                                │ │
│  │  ┌──────────┬──────────┬──────────┬──────────────────────────────────┐ │ │
│  │  │  Type    │  Flags   │ Sequence │        Payload Length            │ │ │
│  │  │ (1 byte) │ (1 byte) │ (2 bytes)│         (4 bytes)                │ │ │
│  │  └──────────┴──────────┴──────────┴──────────────────────────────────┘ │ │
│  │                                                                         │ │
│  │  Type Values:                                                           │ │
│  │  0x01 = Video frame (H.264 NAL units)                                  │ │
│  │  0x02 = Audio frame (Opus packets)                                     │ │
│  │  0x03 = Input event (JSON)                                             │ │
│  │  0x04 = Cursor update (binary or JSON)                                 │ │
│  │  0x05 = Control message (JSON)                                         │ │
│  │  0x06 = Clipboard data                                                 │ │
│  │  0x07 = File transfer chunk                                            │ │
│  │                                                                         │ │
│  │  Flags:                                                                 │ │
│  │  0x01 = Keyframe (for video)                                           │ │
│  │  0x02 = Compressed (zstd)                                              │ │
│  │  0x04 = Fragmented (more fragments follow)                             │ │
│  │  0x08 = Final fragment                                                 │ │
│  │                                                                         │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  Compression: zstd for non-video data (typically 50-70% reduction)         │
│  Encryption: TLS 1.3 (via WSS)                                             │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. Session Management

### 8.1 Session State Machine

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         SESSION STATE MACHINE                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                              ┌─────────────┐                                │
│                              │   CREATED   │                                │
│                              │             │                                │
│                              └──────┬──────┘                                │
│                                     │ StartSession()                        │
│                                     ▼                                       │
│                              ┌─────────────┐                                │
│      ┌───────────────────────│ CONNECTING  │───────────────────────┐       │
│      │                       │             │                       │       │
│      │ Timeout (30s)         └──────┬──────┘         ICE Failed    │       │
│      │                              │ ICE Connected                │       │
│      │                              ▼                              │       │
│      │                       ┌─────────────┐                       │       │
│      │                       │AUTHENTICATING                       │       │
│      │                       │             │                       │       │
│      │                       └──────┬──────┘                       │       │
│      │                              │ Auth Success                 │       │
│      │                              ▼                              │       │
│      │                       ┌─────────────┐                       │       │
│      │   ┌───────────────────│  CONNECTED  │◄──────────────┐      │       │
│      │   │                   │             │               │      │       │
│      │   │ Disconnect        └──────┬──────┘   Reconnect   │      │       │
│      │   │                          │                      │      │       │
│      │   │                          │ Network Loss         │      │       │
│      │   │                          ▼                      │      │       │
│      │   │                   ┌─────────────┐               │      │       │
│      │   │                   │RECONNECTING │───────────────┘      │       │
│      │   │                   │             │                      │       │
│      │   │                   └──────┬──────┘                      │       │
│      │   │                          │ Max Retries (5)             │       │
│      │   │                          ▼                             │       │
│      │   │                   ┌─────────────┐                      │       │
│      └───┼──────────────────►│   FAILED    │◄─────────────────────┘       │
│          │                   │             │                              │
│          │                   └──────┬──────┘                              │
│          │                          │                                     │
│          │                          ▼                                     │
│          │                   ┌─────────────┐                              │
│          └──────────────────►│  TERMINATED │                              │
│                              │             │                              │
│                              └─────────────┘                              │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### 8.2 Session Data Model

```go
// Session represents an active remote desktop session
type Session struct {
    ID             string           `json:"id"`
    DeviceID       string           `json:"deviceId"`
    UserID         string           `json:"userId"`
    State          SessionState     `json:"state"`

    // Connection info
    PeerConnection *webrtc.PeerConnection
    DataChannel    *webrtc.DataChannel
    VideoTrack     *webrtc.TrackLocalStaticSample

    // Configuration
    Quality        QualityPreset    `json:"quality"`
    Permissions    SessionPermissions `json:"permissions"`

    // Timing
    CreatedAt      time.Time        `json:"createdAt"`
    ConnectedAt    *time.Time       `json:"connectedAt"`
    LastActivity   time.Time        `json:"lastActivity"`

    // Stats
    BytesSent      uint64           `json:"bytesSent"`
    BytesReceived  uint64           `json:"bytesReceived"`
    FramesSent     uint64           `json:"framesSent"`

    // Internal
    ctx            context.Context
    cancel         context.CancelFunc
    mu             sync.RWMutex
}

type SessionState string
const (
    StateCreated       SessionState = "created"
    StateConnecting    SessionState = "connecting"
    StateAuthenticating SessionState = "authenticating"
    StateConnected     SessionState = "connected"
    StateReconnecting  SessionState = "reconnecting"
    StateFailed        SessionState = "failed"
    StateTerminated    SessionState = "terminated"
)

type SessionPermissions struct {
    ViewOnly      bool `json:"viewOnly"`      // Can only watch, no input
    FullControl   bool `json:"fullControl"`   // Full mouse/keyboard control
    FileTransfer  bool `json:"fileTransfer"`  // Can transfer files
    Clipboard     bool `json:"clipboard"`     // Can sync clipboard
    Audio         bool `json:"audio"`         // Can hear audio
    RecordSession bool `json:"recordSession"` // Can record to file
}
```

### 8.3 Multi-Viewer Support

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          MULTI-VIEWER ARCHITECTURE                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                         ┌─────────────────┐                                 │
│                         │   AGENT HOST    │                                 │
│                         │                 │                                 │
│                         │  ┌───────────┐  │                                 │
│                         │  │  Screen   │  │                                 │
│                         │  │  Capture  │  │                                 │
│                         │  └─────┬─────┘  │                                 │
│                         │        │        │                                 │
│                         │  ┌─────▼─────┐  │                                 │
│                         │  │  Encoder  │  │  (Single encoder instance)      │
│                         │  │           │  │                                 │
│                         │  └─────┬─────┘  │                                 │
│                         │        │        │                                 │
│                         │  ┌─────▼─────┐  │                                 │
│                         │  │ Broadcast │  │  (Fan-out to all viewers)       │
│                         │  │   Hub     │  │                                 │
│                         │  └─────┬─────┘  │                                 │
│                         └────────┼────────┘                                 │
│                                  │                                          │
│                    ┌─────────────┼─────────────┐                           │
│                    │             │             │                           │
│              ┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼─────┐                     │
│              │ Viewer 1  │ │ Viewer 2  │ │ Viewer 3  │                     │
│              │ (Admin)   │ │ (Support) │ │ (Observe) │                     │
│              │           │ │           │ │           │                     │
│              │ FULL CTRL │ │ VIEW ONLY │ │ VIEW ONLY │                     │
│              └───────────┘ └───────────┘ └───────────┘                     │
│                                                                              │
│  Input Handling:                                                            │
│  • Only one viewer can have control at a time                               │
│  • Control can be transferred via UI                                        │
│  • Admin can always take control                                            │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.4 Session Recording

```go
// SessionRecorder captures session for playback
type SessionRecorder struct {
    sessionID    string
    outputPath   string

    videoWriter  *mp4.Writer
    eventLog     *EventLog

    startTime    time.Time
    recording    bool
}

// EventLog stores timestamped events for replay
type EventLog struct {
    Events []RecordedEvent `json:"events"`
}

type RecordedEvent struct {
    Timestamp int64       `json:"ts"`    // Milliseconds from start
    Type      string      `json:"type"`  // "video", "input", "cursor"
    Data      interface{} `json:"data"`
}

// Record frame with timestamp
func (r *SessionRecorder) RecordFrame(nalUnits []byte, timestamp time.Time) error {
    elapsed := timestamp.Sub(r.startTime)
    return r.videoWriter.WriteNAL(nalUnits, elapsed)
}

// Record input event
func (r *SessionRecorder) RecordInput(event InputEvent) error {
    elapsed := time.Since(r.startTime)
    return r.eventLog.Append(RecordedEvent{
        Timestamp: elapsed.Milliseconds(),
        Type:      "input",
        Data:      event,
    })
}
```

---

## 9. Quality Adaptation System

### 9.1 Adaptive Quality Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       QUALITY ADAPTATION SYSTEM                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                       NETWORK CONDITION MONITOR                         │ │
│  │                                                                         │ │
│  │  Inputs:                          Outputs:                              │ │
│  │  • RTT (round-trip time)          • Available bandwidth estimate       │ │
│  │  • Packet loss rate               • Network stability score            │ │
│  │  • Jitter variance                • Recommended quality level          │ │
│  │  • RTCP feedback (REMB, NACK)                                          │ │
│  │                                                                         │ │
│  └────────────────────────────────────┬───────────────────────────────────┘ │
│                                       │                                     │
│                                       ▼                                     │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                       ADAPTATION CONTROLLER                             │ │
│  │                                                                         │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │ │
│  │  │  Bitrate    │  │  Frame Rate │  │  Resolution │  │   Codec     │   │ │
│  │  │  Adjuster   │  │  Adjuster   │  │  Adjuster   │  │  Selector   │   │ │
│  │  │             │  │             │  │             │  │             │   │ │
│  │  │ 500K - 10M  │  │  10 - 60    │  │  720p-4K    │  │ H264/H265   │   │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘   │ │
│  │                                                                         │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  Adaptation Rules:                                                          │
│  ═══════════════                                                           │
│                                                                              │
│  IF packet_loss > 5% THEN:                                                  │
│      reduce_bitrate(25%)                                                    │
│      force_keyframe()                                                       │
│                                                                              │
│  IF RTT > 200ms THEN:                                                       │
│      reduce_fps(50%)                                                        │
│      reduce_bitrate(10%)                                                    │
│                                                                              │
│  IF bandwidth < current_bitrate THEN:                                       │
│      set_bitrate(bandwidth * 0.85)                                          │
│                                                                              │
│  IF network_stable AND bandwidth > current_bitrate * 1.5 THEN:              │
│      increase_bitrate(10%)   // Gradual increase                           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Capture Strategy (Current Implementation)

**Location:** `agent/internal/webrtc/capture_strategy.go`

```go
// CaptureStrategy makes intelligent decisions about frame encoding
type CaptureStrategy struct {
    width, height    int
    totalPixels      int

    lastKeyframe     time.Time
    unchangedFrames  int

    // Thresholds
    keyframeInterval   time.Duration  // 5 seconds
    skipThreshold      float64        // 0.1% - skip if less changed
    qualityBoostThresh float64        // 25% - lower quality if more changed
}

type FrameDecision struct {
    ShouldEncode   bool              // Skip frame if no changes
    ForceKeyframe  bool              // Force I-frame
    QualityAdjust  int               // -2 to +2 quality delta
    Regions        []image.Rectangle // Dirty regions
}

// Decision logic:
// 1. Skip frame if < 0.1% pixels changed (static screen)
// 2. Lower quality if > 25% pixels changed (scrolling/video)
// 3. Force keyframe every 5 seconds for recovery
// 4. Higher quality for small changes (precision work)
```

### 9.3 Quality Presets

```go
var QualityProfiles = map[string]QualityProfile{
    "auto": {
        Description:    "Automatic adaptation",
        InitialBitrate: 2_000_000,
        MinBitrate:     500_000,
        MaxBitrate:     10_000_000,
        InitialFPS:     30,
        MinFPS:         10,
        MaxFPS:         60,
        AdaptiveMode:   true,
    },
    "low": {
        Description:    "Low bandwidth mode",
        InitialBitrate: 800_000,
        MinBitrate:     300_000,
        MaxBitrate:     1_500_000,
        InitialFPS:     10,
        MinFPS:         5,
        MaxFPS:         15,
        AdaptiveMode:   false,
    },
    "high": {
        Description:    "High quality LAN mode",
        InitialBitrate: 8_000_000,
        MinBitrate:     4_000_000,
        MaxBitrate:     20_000_000,
        InitialFPS:     60,
        MinFPS:         30,
        MaxFPS:         60,
        AdaptiveMode:   true,
    },
}
```

---

## 10. Client Viewer Architecture

### 10.1 Viewer Component Structure

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        CLIENT VIEWER ARCHITECTURE                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                         REACT COMPONENT TREE                            │ │
│  │                                                                         │ │
│  │  <RemoteDesktopViewer>                                                  │ │
│  │    │                                                                    │ │
│  │    ├── <ConnectionManager>         // WebRTC connection handling        │ │
│  │    │     └── useWebRTC()           // Custom hook                       │ │
│  │    │                                                                    │ │
│  │    ├── <VideoCanvas>               // Video rendering                   │ │
│  │    │     ├── <video> element       // H.264 decoding (browser native)  │ │
│  │    │     └── <canvas> overlay      // Cursor, annotations              │ │
│  │    │                                                                    │ │
│  │    ├── <InputHandler>              // Mouse/keyboard capture            │ │
│  │    │     └── useInputCapture()     // Event normalization              │ │
│  │    │                                                                    │ │
│  │    ├── <CursorOverlay>             // Remote cursor display             │ │
│  │    │                                                                    │ │
│  │    ├── <Toolbar>                   // Control buttons                   │ │
│  │    │     ├── <QualitySelector>                                         │ │
│  │    │     ├── <FullscreenToggle>                                        │ │
│  │    │     ├── <ClipboardSync>                                           │ │
│  │    │     └── <SessionInfo>                                             │ │
│  │    │                                                                    │ │
│  │    └── <StatusBar>                 // Connection stats                  │ │
│  │                                                                         │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.2 Video Rendering Pipeline

```typescript
// Video element receives MediaStream from WebRTC
const videoRef = useRef<HTMLVideoElement>(null);

useEffect(() => {
    if (peerConnection && videoRef.current) {
        peerConnection.ontrack = (event) => {
            if (event.track.kind === 'video') {
                videoRef.current!.srcObject = event.streams[0];
            }
        };
    }
}, [peerConnection]);

// Canvas overlay for cursor and annotations
const canvasRef = useRef<HTMLCanvasElement>(null);

function renderCursor(x: number, y: number, shape: CursorShape) {
    const ctx = canvasRef.current?.getContext('2d');
    if (!ctx) return;

    ctx.clearRect(0, 0, canvas.width, canvas.height);

    // Draw cursor at position
    if (shape.type === 'custom' && shape.image) {
        const img = cursorImageCache.get(shape.image);
        ctx.drawImage(img, x - shape.hotspot.x, y - shape.hotspot.y);
    } else {
        // Use CSS cursor for standard shapes
        canvas.style.cursor = shape.type;
    }
}
```

### 10.3 Input Capture

```typescript
// Comprehensive input capture with coordinate transformation
function useInputCapture(
    containerRef: RefObject<HTMLElement>,
    videoRef: RefObject<HTMLVideoElement>,
    sendInput: (event: InputEvent) => void
) {
    const transformCoordinates = useCallback((clientX: number, clientY: number) => {
        const video = videoRef.current;
        const rect = video?.getBoundingClientRect();
        if (!video || !rect) return null;

        // Account for letterboxing/pillarboxing
        const videoAspect = video.videoWidth / video.videoHeight;
        const containerAspect = rect.width / rect.height;

        let renderWidth, renderHeight, offsetX, offsetY;

        if (videoAspect > containerAspect) {
            // Pillarboxing (black bars top/bottom)
            renderWidth = rect.width;
            renderHeight = rect.width / videoAspect;
            offsetX = 0;
            offsetY = (rect.height - renderHeight) / 2;
        } else {
            // Letterboxing (black bars left/right)
            renderHeight = rect.height;
            renderWidth = rect.height * videoAspect;
            offsetX = (rect.width - renderWidth) / 2;
            offsetY = 0;
        }

        // Transform to video coordinates
        const x = ((clientX - rect.left - offsetX) / renderWidth) * video.videoWidth;
        const y = ((clientY - rect.top - offsetY) / renderHeight) * video.videoHeight;

        // Clamp to valid range
        return {
            x: Math.max(0, Math.min(video.videoWidth, x)),
            y: Math.max(0, Math.min(video.videoHeight, y))
        };
    }, [videoRef]);

    // Mouse event handlers
    useEffect(() => {
        const container = containerRef.current;
        if (!container) return;

        const onMouseMove = (e: MouseEvent) => {
            const coords = transformCoordinates(e.clientX, e.clientY);
            if (coords) {
                sendInput({ type: 'move', ...coords });
            }
        };

        const onMouseDown = (e: MouseEvent) => {
            const coords = transformCoordinates(e.clientX, e.clientY);
            if (coords) {
                sendInput({ type: 'down', ...coords, button: e.button });
            }
        };

        // ... similar for mouseup, wheel, keydown, keyup

        container.addEventListener('mousemove', onMouseMove);
        container.addEventListener('mousedown', onMouseDown);
        // ...

        return () => {
            container.removeEventListener('mousemove', onMouseMove);
            container.removeEventListener('mousedown', onMouseDown);
            // ...
        };
    }, [containerRef, transformCoordinates, sendInput]);
}
```

### 10.4 Fullscreen Handling

```typescript
function useFullscreen(elementRef: RefObject<HTMLElement>) {
    const [isFullscreen, setIsFullscreen] = useState(false);

    const enterFullscreen = useCallback(async () => {
        const element = elementRef.current;
        if (!element) return;

        try {
            if (element.requestFullscreen) {
                await element.requestFullscreen();
            } else if ((element as any).webkitRequestFullscreen) {
                await (element as any).webkitRequestFullscreen();
            }

            // Lock pointer for better mouse capture (optional)
            // element.requestPointerLock();

        } catch (err) {
            console.error('Fullscreen failed:', err);
        }
    }, [elementRef]);

    const exitFullscreen = useCallback(async () => {
        if (document.exitFullscreen) {
            await document.exitFullscreen();
        }
    }, []);

    useEffect(() => {
        const onChange = () => setIsFullscreen(!!document.fullscreenElement);
        document.addEventListener('fullscreenchange', onChange);
        return () => document.removeEventListener('fullscreenchange', onChange);
    }, []);

    return { isFullscreen, enterFullscreen, exitFullscreen };
}
```

---

## 11. Security Architecture

### 11.1 Security Layers

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          SECURITY ARCHITECTURE                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Layer 1: Transport Encryption                                              │
│  ═══════════════════════════                                               │
│  • WebRTC: DTLS-SRTP (AES-128-GCM)                                         │
│  • Signaling: WSS/TLS 1.3                                                  │
│  • Fallback WS: WSS/TLS 1.3                                                │
│                                                                              │
│  Layer 2: Authentication                                                    │
│  ════════════════════════                                                  │
│  • Session tokens (JWT, short-lived)                                       │
│  • Device certificates (mTLS for agent)                                    │
│  • User credentials (via Sentinel auth)                                    │
│                                                                              │
│  Layer 3: Authorization                                                     │
│  ════════════════════════                                                  │
│  • Role-based permissions (Admin, Operator, Viewer)                        │
│  • Per-session permission grants                                           │
│  • Device-level access control                                             │
│                                                                              │
│  Layer 4: Input Validation                                                  │
│  ═════════════════════════                                                 │
│  • Coordinate bounds checking                                              │
│  • Rate limiting (prevent DoS via input flood)                             │
│  • Command injection prevention                                            │
│                                                                              │
│  Layer 5: Session Security                                                  │
│  ═════════════════════════                                                 │
│  • Session timeout (configurable, default 30 min)                          │
│  • Idle timeout (5 min no activity → disconnect)                           │
│  • Max session duration (8 hours)                                          │
│  • Concurrent session limits                                               │
│                                                                              │
│  Layer 6: Audit & Monitoring                                                │
│  ═══════════════════════════                                               │
│  • Session start/end logging                                               │
│  • Input event logging (optional, for compliance)                          │
│  • Recording capabilities                                                  │
│  • Anomaly detection                                                       │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 11.2 Session Token Security

```go
// Session token structure
type SessionToken struct {
    SessionID   string    `json:"sid"`
    UserID      string    `json:"uid"`
    DeviceID    string    `json:"did"`
    Permissions uint32    `json:"perm"`
    ExpiresAt   time.Time `json:"exp"`
    IssuedAt    time.Time `json:"iat"`
    Nonce       string    `json:"nonce"`
}

// Token validation
func ValidateSessionToken(token string, expectedSessionID string) (*SessionToken, error) {
    // 1. Verify JWT signature
    claims, err := jwt.Verify(token, signingKey)
    if err != nil {
        return nil, fmt.Errorf("invalid signature: %w", err)
    }

    // 2. Check expiration
    if time.Now().After(claims.ExpiresAt) {
        return nil, ErrTokenExpired
    }

    // 3. Verify session ID matches
    if claims.SessionID != expectedSessionID {
        return nil, ErrSessionMismatch
    }

    // 4. Check if token was revoked
    if isRevoked(claims.SessionID, claims.Nonce) {
        return nil, ErrTokenRevoked
    }

    return claims, nil
}
```

### 11.3 Input Rate Limiting

```go
// Rate limiter for input events
type InputRateLimiter struct {
    mouseLimit    *rate.Limiter  // 1000 events/sec
    keyboardLimit *rate.Limiter  // 100 events/sec
    wheelLimit    *rate.Limiter  // 50 events/sec

    lastInput     time.Time
    burstWindow   time.Duration
    burstCount    int
}

func (r *InputRateLimiter) Allow(eventType string) bool {
    var limiter *rate.Limiter

    switch eventType {
    case "move":
        limiter = r.mouseLimit
    case "keydown", "keyup":
        limiter = r.keyboardLimit
    case "wheel":
        limiter = r.wheelLimit
    default:
        limiter = r.mouseLimit
    }

    // Detect burst (potential automated attack)
    now := time.Now()
    if now.Sub(r.lastInput) < 1*time.Millisecond {
        r.burstCount++
        if r.burstCount > 100 {
            log.Warn("Input burst detected, possible attack")
            return false
        }
    } else {
        r.burstCount = 0
    }
    r.lastInput = now

    return limiter.Allow()
}
```

---

## 12. Platform Abstraction Layer

### 12.1 Interface Definitions

```go
// ============================================================================
// SCREEN CAPTURE ABSTRACTION
// ============================================================================

type IScreenCapture interface {
    Initialize(monitorIndex int) error
    CaptureFrame(timeoutMs int) (*CapturedFrame, error)
    GetDimensions() (width, height int)
    GetMonitorInfo() []MonitorInfo
    Release()
}

// Platform implementations:
// - Windows: DXGICapture (agent/internal/capture/dxgi_windows.go)
// - Linux:   X11Capture, PipeWireCapture (TODO)
// - macOS:   CGDisplayCapture (TODO)

// ============================================================================
// INPUT INJECTION ABSTRACTION
// ============================================================================

type IInputInjector interface {
    MoveMouse(x, y int) error
    MouseDown(button MouseButton) error
    MouseUp(button MouseButton) error
    MouseWheel(deltaX, deltaY int) error

    KeyDown(key Key, modifiers Modifiers) error
    KeyUp(key Key, modifiers Modifiers) error
    TypeText(text string) error

    SendSpecialSequence(seq SpecialSequence) error
}

// Platform implementations:
// - Windows: SendInputInjector (agent/internal/desktop/helper/input_windows.go)
// - Linux:   XTestInjector, UInputInjector (TODO)
// - macOS:   CGEventInjector (TODO)

// ============================================================================
// VIDEO ENCODER ABSTRACTION
// ============================================================================

type IVideoEncoder interface {
    Encode(frame *CapturedFrame) ([]byte, error)
    SetBitrate(bps int) error
    SetFrameRate(fps int) error
    ForceKeyframe()
    GetCodec() CodecType
    IsHardware() bool
    Close()
}

// Platform implementations:
// - Windows: MediaFoundationEncoder, OpenH264Encoder
// - Linux:   VAAPI, OpenH264Encoder (TODO)
// - macOS:   VideoToolboxEncoder (TODO)
// - All:     OpenH264Encoder (software fallback)

// ============================================================================
// CURSOR CAPTURE ABSTRACTION
// ============================================================================

type ICursorCapture interface {
    GetCursorPosition() (x, y int)
    GetCursorShape() CursorShape
    GetCursorImage() (*image.RGBA, Hotspot)
}

// Platform implementations embedded in screen capture

// ============================================================================
// CLIPBOARD ABSTRACTION
// ============================================================================

type IClipboard interface {
    GetText() (string, error)
    SetText(text string) error
    GetImage() (*image.RGBA, error)
    SetImage(img *image.RGBA) error
    Watch(callback func(ClipboardContent)) error
}

// Platform implementations:
// - Windows: Win32Clipboard (TODO)
// - Linux:   X11Clipboard (TODO)
// - macOS:   NSPasteboard (TODO)
```

### 12.2 Factory Pattern

```go
// Platform-specific factory
func NewScreenCapture(platform Platform) (IScreenCapture, error) {
    switch platform {
    case PlatformWindows:
        return capture.NewDXGICapture(0)
    case PlatformLinuxX11:
        return capture.NewX11Capture(0)
    case PlatformLinuxWayland:
        return capture.NewPipeWireCapture(0)
    case PlatformMacOS:
        return capture.NewCGDisplayCapture(0)
    default:
        return nil, ErrUnsupportedPlatform
    }
}

func NewInputInjector(platform Platform) (IInputInjector, error) {
    switch platform {
    case PlatformWindows:
        return helper.NewInputInjector()
    // ... other platforms
    }
}

func NewVideoEncoder(config EncoderConfig) (IVideoEncoder, error) {
    // Try hardware encoders first
    if config.UseHardware {
        if enc, err := tryNVENC(config); err == nil {
            return enc, nil
        }
        if enc, err := tryQuickSync(config); err == nil {
            return enc, nil
        }
        if enc, err := tryMediaFoundation(config); err == nil {
            return enc, nil
        }
    }

    // Fall back to OpenH264
    return NewOpenH264Encoder(config)
}
```

---

## 13. Performance Targets

### 13.1 Latency Budget

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          LATENCY BUDGET (Target: <100ms)                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Component                          Target        Actual (measured)         │
│  ═════════════════════════════════════════════════════════════════════════ │
│                                                                              │
│  Screen Capture (DXGI)              < 5ms         ~2-3ms                    │
│  Color Conversion (BGRA→YCbCr)      < 3ms         ~1-2ms (1080p)            │
│  H.264 Encoding (hardware)          < 5ms         ~3-5ms                    │
│  H.264 Encoding (OpenH264)          < 15ms        ~10-15ms                  │
│  RTP Packetization                  < 1ms         ~0.5ms                    │
│  Network Transit (typical)          < 50ms        varies                    │
│  Jitter Buffer                      < 10ms        ~5-10ms                   │
│  H.264 Decoding (browser)           < 5ms         ~3-5ms                    │
│  Canvas/Video Render                < 5ms         ~1-2ms                    │
│  ─────────────────────────────────────────────────────────────────────────  │
│  TOTAL (hardware encode)            < 85ms        ~65-80ms                  │
│  TOTAL (software encode)            < 95ms        ~75-90ms                  │
│                                                                              │
│  Input latency (viewer → agent):                                            │
│  DataChannel → JSON parse → SendInput  < 20ms total                         │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 13.2 Throughput Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Frame Rate | 30-60 FPS | Adaptive based on network |
| Bitrate Range | 500 Kbps - 20 Mbps | Quality dependent |
| Input Events | 1000/sec | Coalesced mouse moves |
| Cursor Updates | 60/sec | Position only |
| Concurrent Sessions | 5 per agent | Resource constrained |

### 13.3 Resource Limits

```go
// Resource constraints for agent process
var ResourceLimits = struct {
    MaxCPUPercent     int   // 25% of available CPU
    MaxMemoryMB       int   // 512 MB
    MaxEncoderMemMB   int   // 256 MB for encoder
    MaxFrameQueueSize int   // 5 frames
    MaxSessionCount   int   // 5 concurrent
}{
    MaxCPUPercent:     25,
    MaxMemoryMB:       512,
    MaxEncoderMemMB:   256,
    MaxFrameQueueSize: 5,
    MaxSessionCount:   5,
}
```

---

## 14. Error Handling Matrix

### 14.1 Error Categories and Recovery

| Error Type | Detection | Recovery Strategy | User Impact |
|------------|-----------|-------------------|-------------|
| **Capture Errors** |
| DXGI Access Lost | HRESULT 0x887A0026 | Reinitialize capture | Brief freeze (1-2s) |
| Monitor Disconnected | EnumOutputs fails | Switch to primary | Automatic failover |
| Resolution Change | Dimension mismatch | Recreate encoder | Keyframe sent |
| **Encoder Errors** |
| Encoder Init Failed | Creation returns error | Fall back to OpenH264 | Possible quality drop |
| Encode Timeout | Frame not ready | Skip frame | Single frame drop |
| Hardware Encoder Crash | Exception caught | Recreate or fallback | Brief pause |
| **Network Errors** |
| ICE Failed | State = failed | Try TURN relay | 5-10s delay |
| Connection Lost | State = disconnected | Auto-reconnect (5 retries) | Temporary freeze |
| High Packet Loss | >10% loss detected | Reduce bitrate/FPS | Quality reduction |
| **Input Errors** |
| UIPI Blocked | SendInput returns 0 | Log warning | Input ignored |
| Rate Limit Hit | Limiter rejects | Queue or drop | Slight delay |
| **Session Errors** |
| Auth Token Expired | JWT validation fails | Request new token | Reconnect required |
| Session Timeout | Idle > threshold | End session | User must restart |

### 14.2 Error Codes

```go
// Error codes for remote desktop operations
const (
    ErrCodeSuccess           = 0

    // Capture errors (1xxx)
    ErrCodeCaptureInit       = 1001
    ErrCodeCaptureAccessLost = 1002
    ErrCodeCaptureTimeout    = 1003
    ErrCodeMonitorNotFound   = 1004

    // Encoder errors (2xxx)
    ErrCodeEncoderInit       = 2001
    ErrCodeEncoderFailed     = 2002
    ErrCodeEncoderTimeout    = 2003
    ErrCodeNoHardwareEncoder = 2004

    // Transport errors (3xxx)
    ErrCodeICEFailed         = 3001
    ErrCodeDTLSFailed        = 3002
    ErrCodeConnectionLost    = 3003
    ErrCodeSignalingFailed   = 3004

    // Session errors (4xxx)
    ErrCodeSessionExpired    = 4001
    ErrCodeSessionNotFound   = 4002
    ErrCodeSessionFull       = 4003
    ErrCodeUnauthorized      = 4004

    // Input errors (5xxx)
    ErrCodeInputBlocked      = 5001
    ErrCodeInputRateLimited  = 5002
    ErrCodeInvalidInput      = 5003
)
```

---

## 15. Implementation Roadmap

### Phase 1: Core Stability (Current)
- [x] DXGI screen capture
- [x] OpenH264 encoding
- [x] WebRTC transport
- [x] Basic input injection
- [x] Cursor tracking
- [ ] Fix Media Foundation encoder crashes
- [ ] Improve reconnection handling

### Phase 2: Quality Improvements
- [ ] Hardware encoder auto-selection (NVENC, QSV)
- [ ] Adaptive bitrate based on RTCP feedback
- [ ] Multi-monitor support in UI
- [ ] Session recording

### Phase 3: Features
- [ ] Clipboard synchronization
- [ ] File transfer
- [ ] Audio streaming (Opus)
- [ ] Multi-viewer support

### Phase 4: Cross-Platform
- [ ] Linux X11 capture
- [ ] Linux Wayland/PipeWire capture
- [ ] macOS CGDisplayStream capture
- [ ] Platform-specific input injection

### Phase 5: Enterprise
- [ ] Session audit logging
- [ ] Compliance recording
- [ ] Watermarking
- [ ] Central policy management

---

## Appendix A: Message Protocol Reference

### A.1 DataChannel Messages (Agent → Viewer)

```typescript
// Remote screen info
{ type: "remoteInfo", width: 1920, height: 1080 }

// Cursor position
{ type: "cursor", x: 500, y: 300, visible: true }

// Cursor shape change
{ type: "cursorShape", shape: { type: "pointer", hotspot: { x: 6, y: 0 } } }

// Session stats
{ type: "stats", fps: 30, bitrate: 2000000, latency: 45 }
```

### A.2 DataChannel Messages (Viewer → Agent)

```typescript
// Mouse move
{ type: "move", x: 500.5, y: 300.2 }

// Mouse button
{ type: "down", x: 500, y: 300, button: 0 }
{ type: "up", x: 500, y: 300, button: 0 }

// Mouse wheel
{ type: "wheel", x: 500, y: 300, deltaY: 120 }

// Keyboard
{ type: "keydown", key: "a", code: "KeyA", modifiers: { ctrl: false, alt: false, shift: false, meta: false } }
{ type: "keyup", key: "a", code: "KeyA" }

// Special sequences
{ type: "special", action: "ctrl-alt-del" }

// Quality change request
{ type: "quality", preset: "high" }
```

---

## Appendix B: Build Configuration

### B.1 Go Build Tags

```go
//go:build windows
// +build windows

// Windows-specific code (DXGI, SendInput, etc.)
```

### B.2 Required Dependencies

```
# Windows
- d3d11.dll (D3D11CreateDevice)
- dxgi.dll (CreateDXGIFactory1)
- user32.dll (SendInput, SetCursorPos)
- openh264-2.4.1-win64.dll (H.264 encoding)

# Go packages
- github.com/pion/webrtc/v4
- github.com/pion/ice/v4
- github.com/y9o/go-openh264
- github.com/kbinani/screenshot
```

---

*Document maintained by Sentinel Development Team*
*Last updated: 2026-02-01*
