//go:build windows

package webrtc

import (
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/encoder"
)

// mfEncoderWrapper wraps the Media Foundation encoder to implement VideoEncoder
type mfEncoderWrapper struct {
	enc           *encoder.H264Encoder
	forceKeyframe bool
	mu            sync.Mutex
}

// newMFEncoderInternal creates a new Media Foundation encoder wrapper (internal, no timeout)
func newMFEncoderInternal(width, height, bitrate, fps int) (*mfEncoderWrapper, error) {
	log.Printf("[MFEncoder] newMFEncoderInternal starting: %dx%d @ %d bps", width, height, bitrate)

	config := encoder.EncoderConfig{
		Width:      width,
		Height:     height,
		FrameRate:  fps,
		Bitrate:    bitrate,
		Profile:    "main",
		LowLatency: true,
		GOPSize:    fps * 2, // 2 second keyframe interval
	}

	log.Printf("[MFEncoder] Calling encoder.NewH264Encoder...")
	enc, err := encoder.NewH264Encoder(config)
	if err != nil {
		log.Printf("[MFEncoder] encoder.NewH264Encoder failed: %v", err)
		return nil, err
	}

	log.Printf("[MFEncoder] Created Media Foundation encoder (hardware=%v)", enc.IsHardware())

	return &mfEncoderWrapper{
		enc: enc,
	}, nil
}

// newMFEncoderWithTimeout creates a new Media Foundation encoder with timeout protection
func newMFEncoderWithTimeout(width, height, bitrate, fps int, timeout time.Duration) (*mfEncoderWrapper, error) {
	log.Printf("[MFEncoder] newMFEncoderWithTimeout called: %dx%d @ %d bps, timeout=%v", width, height, bitrate, timeout)

	type result struct {
		enc *mfEncoderWrapper
		err error
	}

	resultChan := make(chan result, 1)

	go func() {
		log.Printf("[MFEncoder] Goroutine: starting newMFEncoderInternal...")
		enc, err := newMFEncoderInternal(width, height, bitrate, fps)
		log.Printf("[MFEncoder] Goroutine: newMFEncoderInternal returned, err=%v, sending to channel...", err)
		resultChan <- result{enc: enc, err: err}
		log.Printf("[MFEncoder] Goroutine: sent to channel")
	}()

	log.Printf("[MFEncoder] Waiting on select...")
	select {
	case res := <-resultChan:
		log.Printf("[MFEncoder] Received from channel, returning encoder (err=%v)...", res.err)
		return res.enc, res.err
	case <-time.After(timeout):
		log.Printf("[MFEncoder] TIMEOUT after %v waiting for Media Foundation encoder", timeout)
		return nil, fmt.Errorf("Media Foundation encoder creation timed out after %v", timeout)
	}
}

// newMFEncoder creates a new Media Foundation encoder wrapper with default 10 second timeout
func newMFEncoder(width, height, bitrate, fps int) (*mfEncoderWrapper, error) {
	return newMFEncoderWithTimeout(width, height, bitrate, fps, 10*time.Second)
}

func (w *mfEncoderWrapper) Encode(ycbcr *image.YCbCr) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[MFEncoder] PANIC in Encode: %v", r)
			err = fmt.Errorf("MF encoder panic: %v", r)
		}
	}()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Convert YCbCr (I420) to NV12 for Media Foundation
	nv12 := ycbcrToNV12(ycbcr)

	forceKey := w.forceKeyframe
	w.forceKeyframe = false

	return w.enc.Encode(nv12, forceKey)
}

func (w *mfEncoderWrapper) EncodeBGRA(bgra []byte, width, height, stride int, forceKeyframe bool) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[MFEncoder] PANIC in EncodeBGRA: %v", r)
			err = fmt.Errorf("MF encoder panic: %v", r)
		}
	}()

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.enc.EncodeBGRA(bgra, width, height, stride, forceKeyframe)
}

func (w *mfEncoderWrapper) ForceKeyframe() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.forceKeyframe = true
}

func (w *mfEncoderWrapper) SetBitrate(bps int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.SetBitrate(bps); err != nil {
		return fmt.Errorf("MF SetBitrate failed: %w", err)
	}
	log.Printf("[MFEncoder] Bitrate updated to %d bps", bps)
	return nil
}

func (w *mfEncoderWrapper) GetWidth() int {
	return w.enc.GetConfig().Width
}

func (w *mfEncoderWrapper) GetHeight() int {
	return w.enc.GetConfig().Height
}

func (w *mfEncoderWrapper) IsHardware() bool {
	return w.enc.IsHardware()
}

func (w *mfEncoderWrapper) Close() {
	w.enc.Release()
}

// ycbcrToNV12 converts I420 (planar YCbCr) to NV12 (semi-planar)
func ycbcrToNV12(img *image.YCbCr) []byte {
	width := img.Rect.Dx()
	height := img.Rect.Dy()

	// NV12 format: Y plane followed by interleaved UV plane
	ySize := width * height
	uvSize := (width / 2) * (height / 2) * 2
	nv12 := make([]byte, ySize+uvSize)

	// Copy Y plane (same in both formats)
	copy(nv12[:ySize], img.Y)

	// Interleave U and V planes into UV plane
	uvOffset := ySize
	uvWidth := width / 2
	uvHeight := height / 2

	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			srcIdx := row*img.CStride + col
			dstIdx := uvOffset + row*width + col*2

			nv12[dstIdx] = img.Cb[srcIdx]   // U
			nv12[dstIdx+1] = img.Cr[srcIdx] // V
		}
	}

	return nv12
}
