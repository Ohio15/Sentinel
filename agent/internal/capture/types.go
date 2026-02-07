// Package capture provides screen capture functionality
package capture

import (
	"image"
	"time"
)

// CapturedFrame contains frame data and metadata
type CapturedFrame struct {
	Data       []byte            // BGRA pixel data
	Width      int
	Height     int
	Stride     int
	DirtyRects []image.Rectangle // Only these regions changed
	Timestamp  time.Time
}

// CursorData contains cursor information
type CursorData struct {
	X         int
	Y         int
	Visible   bool
	ShapeType int    // 0=monochrome, 1=color, 2=masked color
	Width     int
	Height    int
	HotspotX  int
	HotspotY  int
	ImageData []byte // BGRA cursor image
}
