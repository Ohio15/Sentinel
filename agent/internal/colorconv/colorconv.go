//go:build windows

package colorconv

import "image"

// clamp restricts v to [0, 255].
func clamp(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// BGRAToI420 converts a BGRA frame to I420 (YCbCr 4:2:0) using integer BT.601
// coefficients in a single pass. The bgra slice is expected in B-G-R-A byte order.
func BGRAToI420(bgra []byte, width, height, stride int) *image.YCbCr {
	yStride := width
	cStride := (width + 1) / 2
	chromaH := (height + 1) / 2

	yLen := yStride * height
	cLen := cStride * chromaH

	yPlane := make([]byte, yLen)
	cbPlane := make([]byte, cLen)
	crPlane := make([]byte, cLen)

	bgraLen := len(bgra)

	for y := 0; y < height; y++ {
		rowOff := y * stride
		for x := 0; x < width; x++ {
			srcIdx := rowOff + x*4
			if srcIdx+3 >= bgraLen {
				continue
			}
			b := int(bgra[srcIdx])
			g := int(bgra[srcIdx+1])
			r := int(bgra[srcIdx+2])

			yVal := ((66*r + 129*g + 25*b + 128) >> 8) + 16
			yPlane[y*yStride+x] = clamp(yVal)
		}
	}

	// Chroma: average 2x2 blocks
	for cy := 0; cy < chromaH; cy++ {
		srcY := cy * 2
		for cx := 0; cx < cStride; cx++ {
			srcX := cx * 2

			var rSum, gSum, bSum, count int

			for dy := 0; dy < 2; dy++ {
				py := srcY + dy
				if py >= height {
					continue
				}
				rowOff := py * stride
				for dx := 0; dx < 2; dx++ {
					px := srcX + dx
					if px >= width {
						continue
					}
					srcIdx := rowOff + px*4
					if srcIdx+3 >= bgraLen {
						continue
					}
					bSum += int(bgra[srcIdx])
					gSum += int(bgra[srcIdx+1])
					rSum += int(bgra[srcIdx+2])
					count++
				}
			}

			if count == 0 {
				cbPlane[cy*cStride+cx] = 128
				crPlane[cy*cStride+cx] = 128
				continue
			}

			rAvg := rSum / count
			gAvg := gSum / count
			bAvg := bSum / count

			cb := ((-38*rAvg - 74*gAvg + 112*bAvg + 128) >> 8) + 128
			cr := ((112*rAvg - 94*gAvg - 18*bAvg + 128) >> 8) + 128

			cbPlane[cy*cStride+cx] = clamp(cb)
			crPlane[cy*cStride+cx] = clamp(cr)
		}
	}

	return &image.YCbCr{
		Y:              yPlane,
		Cb:             cbPlane,
		Cr:             crPlane,
		YStride:        yStride,
		CStride:        cStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, width, height),
	}
}

// BGRAToI420Padded converts a BGRA frame to I420 with padding to targetW x targetH.
// This is useful for H.264 encoders that require 16-pixel-aligned dimensions.
// Padding areas are filled with Y=16 (black) and Cb/Cr=128 (neutral chroma).
func BGRAToI420Padded(bgra []byte, width, height, stride, targetW, targetH int) *image.YCbCr {
	yStride := targetW
	cStride := (targetW + 1) / 2
	chromaH := (targetH + 1) / 2

	yLen := yStride * targetH
	cLen := cStride * chromaH

	// Pre-fill with black (Y=16) and neutral chroma (128)
	yPlane := make([]byte, yLen)
	for i := range yPlane {
		yPlane[i] = 16
	}
	cbPlane := make([]byte, cLen)
	for i := range cbPlane {
		cbPlane[i] = 128
	}
	crPlane := make([]byte, cLen)
	for i := range crPlane {
		crPlane[i] = 128
	}

	bgraLen := len(bgra)

	// Convert source pixels into the target buffer
	srcH := height
	if srcH > targetH {
		srcH = targetH
	}
	srcW := width
	if srcW > targetW {
		srcW = targetW
	}

	for y := 0; y < srcH; y++ {
		rowOff := y * stride
		for x := 0; x < srcW; x++ {
			srcIdx := rowOff + x*4
			if srcIdx+3 >= bgraLen {
				continue
			}
			b := int(bgra[srcIdx])
			g := int(bgra[srcIdx+1])
			r := int(bgra[srcIdx+2])

			yVal := ((66*r + 129*g + 25*b + 128) >> 8) + 16
			yPlane[y*yStride+x] = clamp(yVal)
		}
	}

	// Chroma: average 2x2 blocks from source pixels only
	chromaSrcH := (srcH + 1) / 2
	chromaSrcW := (srcW + 1) / 2

	for cy := 0; cy < chromaSrcH; cy++ {
		srcY := cy * 2
		for cx := 0; cx < chromaSrcW; cx++ {
			srcX := cx * 2

			var rSum, gSum, bSum, count int

			for dy := 0; dy < 2; dy++ {
				py := srcY + dy
				if py >= srcH {
					continue
				}
				rowOff := py * stride
				for dx := 0; dx < 2; dx++ {
					px := srcX + dx
					if px >= srcW {
						continue
					}
					srcIdx := rowOff + px*4
					if srcIdx+3 >= bgraLen {
						continue
					}
					bSum += int(bgra[srcIdx])
					gSum += int(bgra[srcIdx+1])
					rSum += int(bgra[srcIdx+2])
					count++
				}
			}

			if count == 0 {
				continue // already filled with 128
			}

			rAvg := rSum / count
			gAvg := gSum / count
			bAvg := bSum / count

			cb := ((-38*rAvg - 74*gAvg + 112*bAvg + 128) >> 8) + 128
			cr := ((112*rAvg - 94*gAvg - 18*bAvg + 128) >> 8) + 128

			cbPlane[cy*cStride+cx] = clamp(cb)
			crPlane[cy*cStride+cx] = clamp(cr)
		}
	}

	return &image.YCbCr{
		Y:              yPlane,
		Cb:             cbPlane,
		Cr:             crPlane,
		YStride:        yStride,
		CStride:        cStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, targetW, targetH),
	}
}

// BGRAToNV12 converts a BGRA frame to NV12 format (Y plane followed by interleaved
// UV plane). This is the preferred input format for Media Foundation encoders.
// Uses integer BT.601 coefficients with 2x2 chroma averaging.
func BGRAToNV12(bgra []byte, width, height, stride int) []byte {
	chromaH := (height + 1) / 2

	ySize := width * height
	uvSize := width * chromaH // width must be even for NV12; interleaved U,V pairs = (width/2)*2 = width
	buf := make([]byte, ySize+uvSize)

	bgraLen := len(bgra)

	// Y plane
	for y := 0; y < height; y++ {
		rowOff := y * stride
		for x := 0; x < width; x++ {
			srcIdx := rowOff + x*4
			if srcIdx+3 >= bgraLen {
				continue
			}
			b := int(bgra[srcIdx])
			g := int(bgra[srcIdx+1])
			r := int(bgra[srcIdx+2])

			yVal := ((66*r + 129*g + 25*b + 128) >> 8) + 16
			buf[y*width+x] = clamp(yVal)
		}
	}

	// Interleaved UV plane
	chromaW := (width + 1) / 2
	uvOff := ySize

	for cy := 0; cy < chromaH; cy++ {
		srcY := cy * 2
		for cx := 0; cx < chromaW; cx++ {
			srcX := cx * 2

			var rSum, gSum, bSum, count int

			for dy := 0; dy < 2; dy++ {
				py := srcY + dy
				if py >= height {
					continue
				}
				rowOff := py * stride
				for dx := 0; dx < 2; dx++ {
					px := srcX + dx
					if px >= width {
						continue
					}
					srcIdx := rowOff + px*4
					if srcIdx+3 >= bgraLen {
						continue
					}
					bSum += int(bgra[srcIdx])
					gSum += int(bgra[srcIdx+1])
					rSum += int(bgra[srcIdx+2])
					count++
				}
			}

			var cbVal, crVal uint8
			if count == 0 {
				cbVal = 128
				crVal = 128
			} else {
				rAvg := rSum / count
				gAvg := gSum / count
				bAvg := bSum / count

				cb := ((-38*rAvg - 74*gAvg + 112*bAvg + 128) >> 8) + 128
				cr := ((112*rAvg - 94*gAvg - 18*bAvg + 128) >> 8) + 128

				cbVal = clamp(cb)
				crVal = clamp(cr)
			}

			// NV12: interleaved U (Cb) then V (Cr)
			uvIdx := uvOff + cy*width + cx*2
			if uvIdx+1 < len(buf) {
				buf[uvIdx] = cbVal
				buf[uvIdx+1] = crVal
			}
		}
	}

	return buf
}
