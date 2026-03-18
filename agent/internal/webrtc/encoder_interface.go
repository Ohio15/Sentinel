//go:build windows

package webrtc

import (
	"fmt"
	"image"
	"log"
	"unsafe"

	openh264 "github.com/y9o/go-openh264"

	"github.com/sentinel/agent/internal/colorconv"
)

// VideoEncoder is the interface for H.264 video encoders
type VideoEncoder interface {
	// Encode encodes a YCbCr 4:2:0 frame to H.264 NAL units
	Encode(ycbcr *image.YCbCr) ([]byte, error)

	// EncodeBGRA encodes a BGRA frame directly (if supported)
	EncodeBGRA(bgra []byte, width, height, stride int, forceKeyframe bool) ([]byte, error)

	// ForceKeyframe forces the next frame to be a keyframe
	ForceKeyframe()

	// SetBitrate adjusts the target bitrate dynamically
	SetBitrate(bps int) error

	// GetWidth returns the encoder's configured width
	GetWidth() int

	// GetHeight returns the encoder's configured height
	GetHeight() int

	// IsHardware returns true if this is a hardware encoder
	IsHardware() bool

	// Close releases encoder resources
	Close()
}

// openH264Wrapper wraps the existing h264Encoder to implement VideoEncoder
type openH264Wrapper struct {
	enc           *h264Encoder
	forceKeyframe bool
}

func (w *openH264Wrapper) Encode(ycbcr *image.YCbCr) ([]byte, error) {
	return w.enc.encode(ycbcr)
}

func (w *openH264Wrapper) EncodeBGRA(bgra []byte, width, height, stride int, forceKeyframe bool) ([]byte, error) {
	ycbcr := colorconv.BGRAToI420(bgra, width, height, stride)
	if forceKeyframe {
		w.ForceKeyframe()
	}
	return w.enc.encode(ycbcr)
}

func (w *openH264Wrapper) ForceKeyframe() {
	if w.enc != nil && w.enc.encoder != nil {
		w.enc.mu.Lock()
		w.enc.encoder.ForceIntraFrame(true)
		w.enc.mu.Unlock()
	}
	w.forceKeyframe = true // Keep flag as backup
}

func (w *openH264Wrapper) SetBitrate(bps int) error {
	if w.enc == nil || w.enc.encoder == nil {
		return fmt.Errorf("encoder not initialized")
	}
	w.enc.mu.Lock()
	defer w.enc.mu.Unlock()

	bitrateInfo := openh264.SBitrateInfo{
		ILayer:   0, // SPATIAL_LAYER_ALL
		IBitrate: int32(bps),
	}
	if ret := w.enc.encoder.SetOption(openh264.ENCODER_OPTION_BITRATE, (*int)(unsafe.Pointer(&bitrateInfo))); ret != 0 {
		return fmt.Errorf("SetOption BITRATE failed: %d", ret)
	}
	log.Printf("[OpenH264] Bitrate updated to %d bps", bps)
	return nil
}

func (w *openH264Wrapper) GetWidth() int {
	return int(w.enc.width)
}

func (w *openH264Wrapper) GetHeight() int {
	return int(w.enc.height)
}

func (w *openH264Wrapper) IsHardware() bool {
	return false
}

func (w *openH264Wrapper) Close() {
	w.enc.close()
}

// bgraToRGBASlice converts BGRA to RGBA
func bgraToRGBASlice(bgra []byte, width, height, stride int) []byte {
	rgba := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcIdx := y*stride + x*4
			dstIdx := y*width*4 + x*4
			rgba[dstIdx+0] = bgra[srcIdx+2] // R from B
			rgba[dstIdx+1] = bgra[srcIdx+1] // G
			rgba[dstIdx+2] = bgra[srcIdx+0] // B from R
			rgba[dstIdx+3] = bgra[srcIdx+3] // A
		}
	}
	return rgba
}

// rgbaSliceToYCbCr converts RGBA slice to YCbCr 4:2:0
func rgbaSliceToYCbCr(rgba []byte, width, height, stride int) *image.YCbCr {
	bounds := image.Rect(0, 0, width, height)
	ycbcr := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*stride + x*4
			r := float64(rgba[offset])
			g := float64(rgba[offset+1])
			b := float64(rgba[offset+2])

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
