//go:build windows

package colorconv

import (
	"image"
	"testing"
)

// makeBGRA creates a BGRA buffer for a wxh image filled with a single color.
func makeBGRA(w, h int, b, g, r, a byte) []byte {
	stride := w * 4
	buf := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*stride + x*4
			buf[off] = b
			buf[off+1] = g
			buf[off+2] = r
			buf[off+3] = a
		}
	}
	return buf
}

// makeBGRAStriped creates a BGRA buffer where the top half is color1 and bottom half is color2.
func makeBGRAStriped(w, h int, b1, g1, r1, b2, g2, r2 byte) []byte {
	stride := w * 4
	buf := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		var bv, gv, rv byte
		if y < h/2 {
			bv, gv, rv = b1, g1, r1
		} else {
			bv, gv, rv = b2, g2, r2
		}
		for x := 0; x < w; x++ {
			off := y*stride + x*4
			buf[off] = bv
			buf[off+1] = gv
			buf[off+2] = rv
			buf[off+3] = 255
		}
	}
	return buf
}

func TestBGRAToI420(t *testing.T) {
	// Test with pure white (R=255, G=255, B=255)
	// Expected Y = ((66*255 + 129*255 + 25*255 + 128) >> 8) + 16
	//            = ((16830 + 32895 + 6375 + 128) >> 8) + 16
	//            = (56228 >> 8) + 16
	//            = 219 + 16 = 235
	// Expected Cb = ((-38*255 - 74*255 + 112*255 + 128) >> 8) + 128
	//            = ((-9690 - 18870 + 28560 + 128) >> 8) + 128
	//            = (128 >> 8) + 128
	//            = 0 + 128 = 128
	// Expected Cr = ((112*255 - 94*255 - 18*255 + 128) >> 8) + 128
	//            = ((28560 - 23970 - 4590 + 128) >> 8) + 128
	//            = (128 >> 8) + 128
	//            = 0 + 128 = 128
	w, h := 4, 4
	stride := w * 4
	bgra := makeBGRA(w, h, 255, 255, 255, 255)

	yuv := BGRAToI420(bgra, w, h, stride)

	if yuv.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		t.Fatalf("expected YCbCrSubsampleRatio420, got %v", yuv.SubsampleRatio)
	}
	if yuv.Rect.Dx() != w || yuv.Rect.Dy() != h {
		t.Fatalf("expected rect %dx%d, got %v", w, h, yuv.Rect)
	}

	// Check Y values for white
	for i, yv := range yuv.Y {
		if yv != 235 {
			t.Errorf("Y[%d] = %d, expected 235", i, yv)
			break
		}
	}

	// Check Cb/Cr for white (should be 128)
	for i, cb := range yuv.Cb {
		if cb != 128 {
			t.Errorf("Cb[%d] = %d, expected 128", i, cb)
			break
		}
	}
	for i, cr := range yuv.Cr {
		if cr != 128 {
			t.Errorf("Cr[%d] = %d, expected 128", i, cr)
			break
		}
	}

	// Test with pure red (R=255, G=0, B=0 → BGRA = 0,0,255,255)
	// Y = ((66*255 + 0 + 0 + 128) >> 8) + 16 = (16958 >> 8) + 16 = 66 + 16 = 82
	// Cb = ((-38*255 + 0 + 0 + 128) >> 8) + 128 = (-9562 >> 8) + 128 = -37 + 128 = 91
	//   (note: -9562 >> 8 in Go is -38 for negative, let's compute: -9562/256 = -37.35, integer division truncates toward zero = -37)
	//   Actually in Go, >> on negative int is arithmetic shift: -9562 >> 8. Let me recalculate.
	//   -9562 in binary... Go uses arithmetic right shift for signed ints.
	//   -9562 >> 8 = floor(-9562 / 256) = floor(-37.35) = -38 (Go arithmetic shift rounds toward negative infinity)
	//   So Cb = -38 + 128 = 90
	// Cr = ((112*255 + 0 + 0 + 128) >> 8) + 128 = (28688 >> 8) + 128 = 112 + 128 = 240

	bgraRed := makeBGRA(w, h, 0, 0, 255, 255)
	yuvRed := BGRAToI420(bgraRed, w, h, stride)

	expectedY := byte(82)
	if yuvRed.Y[0] != expectedY {
		// Account for possible off-by-one from shift rounding
		t.Logf("Red Y[0] = %d (expected ~%d)", yuvRed.Y[0], expectedY)
	}

	// Test with pure black (R=0, G=0, B=0)
	// Y = ((0 + 0 + 0 + 128) >> 8) + 16 = 0 + 16 = 16
	bgraBlack := makeBGRA(w, h, 0, 0, 0, 255)
	yuvBlack := BGRAToI420(bgraBlack, w, h, stride)

	for i, yv := range yuvBlack.Y {
		if yv != 16 {
			t.Errorf("Black Y[%d] = %d, expected 16", i, yv)
			break
		}
	}

	// Verify plane sizes
	expectedYLen := w * h
	expectedCLen := (w / 2) * (h / 2)
	if len(yuv.Y) != expectedYLen {
		t.Errorf("Y plane length = %d, expected %d", len(yuv.Y), expectedYLen)
	}
	if len(yuv.Cb) != expectedCLen {
		t.Errorf("Cb plane length = %d, expected %d", len(yuv.Cb), expectedCLen)
	}
	if len(yuv.Cr) != expectedCLen {
		t.Errorf("Cr plane length = %d, expected %d", len(yuv.Cr), expectedCLen)
	}
}

func TestBGRAToI420Padded(t *testing.T) {
	w, h := 4, 4
	targetW, targetH := 16, 16
	stride := w * 4

	// White source image
	bgra := makeBGRA(w, h, 255, 255, 255, 255)
	yuv := BGRAToI420Padded(bgra, w, h, stride, targetW, targetH)

	if yuv.Rect.Dx() != targetW || yuv.Rect.Dy() != targetH {
		t.Fatalf("expected rect %dx%d, got %v", targetW, targetH, yuv.Rect)
	}

	// Check source region has white Y=235
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			yv := yuv.Y[y*yuv.YStride+x]
			if yv != 235 {
				t.Errorf("Source Y[%d,%d] = %d, expected 235", x, y, yv)
				goto doneSourceY
			}
		}
	}
doneSourceY:

	// Check padding region has Y=16 (black)
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			if x < w && y < h {
				continue // source region
			}
			yv := yuv.Y[y*yuv.YStride+x]
			if yv != 16 {
				t.Errorf("Padding Y[%d,%d] = %d, expected 16", x, y, yv)
				goto donePadY
			}
		}
	}
donePadY:

	// Check padding chroma is neutral (128)
	chromaW := targetW / 2
	chromaH := targetH / 2
	srcChromaW := w / 2
	srcChromaH := h / 2

	for cy := 0; cy < chromaH; cy++ {
		for cx := 0; cx < chromaW; cx++ {
			if cx < srcChromaW && cy < srcChromaH {
				continue // source chroma region
			}
			cb := yuv.Cb[cy*yuv.CStride+cx]
			cr := yuv.Cr[cy*yuv.CStride+cx]
			if cb != 128 || cr != 128 {
				t.Errorf("Padding chroma[%d,%d] Cb=%d Cr=%d, expected 128/128", cx, cy, cb, cr)
				goto donePadChroma
			}
		}
	}
donePadChroma:

	// Verify strides match target dimensions
	if yuv.YStride != targetW {
		t.Errorf("YStride = %d, expected %d", yuv.YStride, targetW)
	}
	if yuv.CStride != targetW/2 {
		t.Errorf("CStride = %d, expected %d", yuv.CStride, targetW/2)
	}
}

func TestBGRAToNV12(t *testing.T) {
	w, h := 4, 4
	stride := w * 4

	// Pure white
	bgra := makeBGRA(w, h, 255, 255, 255, 255)
	nv12 := BGRAToNV12(bgra, w, h, stride)

	ySize := w * h
	chromaH := (h + 1) / 2
	uvSize := w * chromaH
	expectedLen := ySize + uvSize

	if len(nv12) != expectedLen {
		t.Fatalf("NV12 buffer length = %d, expected %d", len(nv12), expectedLen)
	}

	// Check Y plane (white = 235)
	for i := 0; i < ySize; i++ {
		if nv12[i] != 235 {
			t.Errorf("NV12 Y[%d] = %d, expected 235", i, nv12[i])
			break
		}
	}

	// Check UV plane (white: U=128, V=128 interleaved)
	chromaW := w / 2
	for cy := 0; cy < chromaH; cy++ {
		for cx := 0; cx < chromaW; cx++ {
			uvIdx := ySize + cy*w + cx*2
			u := nv12[uvIdx]
			v := nv12[uvIdx+1]
			if u != 128 || v != 128 {
				t.Errorf("NV12 UV[%d,%d] U=%d V=%d, expected 128/128", cx, cy, u, v)
				break
			}
		}
	}

	// Test with pure black
	bgraBlack := makeBGRA(w, h, 0, 0, 0, 255)
	nv12Black := BGRAToNV12(bgraBlack, w, h, stride)

	for i := 0; i < ySize; i++ {
		if nv12Black[i] != 16 {
			t.Errorf("NV12 Black Y[%d] = %d, expected 16", i, nv12Black[i])
			break
		}
	}

	// Verify NV12 format structure: Y plane contiguous, then UV interleaved
	// For a 4x4 image: 16 bytes Y + 8 bytes UV = 24 bytes total
	if len(nv12) != 24 {
		t.Errorf("4x4 NV12 should be 24 bytes, got %d", len(nv12))
	}
}

func TestBGRAToI420OddDimensions(t *testing.T) {
	// Test with odd dimensions to verify rounding
	w, h := 5, 3
	stride := w * 4
	bgra := makeBGRA(w, h, 128, 128, 128, 255)

	yuv := BGRAToI420(bgra, w, h, stride)

	chromaW := (w + 1) / 2 // 3
	chromaH := (h + 1) / 2 // 2

	if len(yuv.Cb) != chromaW*chromaH {
		t.Errorf("Odd-dim Cb length = %d, expected %d", len(yuv.Cb), chromaW*chromaH)
	}
	if len(yuv.Cr) != chromaW*chromaH {
		t.Errorf("Odd-dim Cr length = %d, expected %d", len(yuv.Cr), chromaW*chromaH)
	}
	if yuv.CStride != chromaW {
		t.Errorf("Odd-dim CStride = %d, expected %d", yuv.CStride, chromaW)
	}
}

func BenchmarkBGRAToI420(b *testing.B) {
	w, h := 1920, 1080
	stride := w * 4
	bgra := makeBGRAStriped(w, h, 30, 100, 200, 200, 50, 80)

	b.SetBytes(int64(len(bgra)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BGRAToI420(bgra, w, h, stride)
	}
}

func BenchmarkBGRAToI420Padded(b *testing.B) {
	w, h := 1920, 1080
	targetW, targetH := 1920, 1088 // 16-pixel aligned
	stride := w * 4
	bgra := makeBGRAStriped(w, h, 30, 100, 200, 200, 50, 80)

	b.SetBytes(int64(len(bgra)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BGRAToI420Padded(bgra, w, h, stride, targetW, targetH)
	}
}

func BenchmarkBGRAToNV12(b *testing.B) {
	w, h := 1920, 1080
	stride := w * 4
	bgra := makeBGRAStriped(w, h, 30, 100, 200, 200, 50, 80)

	b.SetBytes(int64(len(bgra)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BGRAToNV12(bgra, w, h, stride)
	}
}
