//go:build windows

package webrtc

import (
	"image"
	"time"
)

// CaptureStrategy makes intelligent decisions about frame encoding
// based on dirty rectangles and activity patterns
type CaptureStrategy struct {
	width, height int

	// Tracking
	lastKeyframe    time.Time
	unchangedFrames int
	totalPixels     int

	// Thresholds
	keyframeInterval   time.Duration
	skipThreshold      float64 // Skip if < X% changed
	qualityBoostThresh float64 // Boost quality if > X% changed
}

// FrameDecision contains the strategy's recommendation for a frame
type FrameDecision struct {
	ShouldEncode  bool
	ForceKeyframe bool
	QualityAdjust int // -2 to +2, adjust encoder quality
	Regions       []image.Rectangle
}

// NewCaptureStrategy creates a new capture strategy for the given dimensions
func NewCaptureStrategy(width, height int) *CaptureStrategy {
	return &CaptureStrategy{
		width:              width,
		height:             height,
		totalPixels:        width * height,
		lastKeyframe:       time.Now(),
		keyframeInterval:   5 * time.Second,
		skipThreshold:      0.001, // 0.1% of screen
		qualityBoostThresh: 0.25,  // 25% of screen
	}
}

// Decide analyzes dirty rectangles and returns a frame encoding decision
func (s *CaptureStrategy) Decide(dirtyRects []image.Rectangle) FrameDecision {
	decision := FrameDecision{
		ShouldEncode: true,
		Regions:      dirtyRects,
	}

	// Calculate changed pixels
	changedPixels := 0
	for _, r := range dirtyRects {
		changedPixels += r.Dx() * r.Dy()
	}

	var changeRatio float64
	if s.totalPixels > 0 {
		changeRatio = float64(changedPixels) / float64(s.totalPixels)
	}

	// Decision 1: Skip frame if almost nothing changed
	if changeRatio < s.skipThreshold {
		s.unchangedFrames++

		// But still send periodic keyframes
		if time.Since(s.lastKeyframe) > s.keyframeInterval {
			decision.ForceKeyframe = true
			s.lastKeyframe = time.Now()
			s.unchangedFrames = 0
		} else if s.unchangedFrames < 30 { // Skip up to 30 frames (1 sec at 30fps)
			decision.ShouldEncode = false
			return decision
		}
	} else {
		s.unchangedFrames = 0
	}

	// Decision 2: Adjust quality based on change amount
	if changeRatio > s.qualityBoostThresh {
		// Lots changing - probably scrolling or video
		// Lower quality for bandwidth, but increase framerate perception
		decision.QualityAdjust = -1
	} else if changeRatio < 0.05 {
		// Small changes - can afford higher quality
		decision.QualityAdjust = 1
	}

	// Decision 3: Force keyframe periodically
	if time.Since(s.lastKeyframe) > s.keyframeInterval {
		decision.ForceKeyframe = true
		s.lastKeyframe = time.Now()
	}

	return decision
}

// Reset resets the strategy state
func (s *CaptureStrategy) Reset() {
	s.lastKeyframe = time.Now()
	s.unchangedFrames = 0
}

// SetKeyframeInterval sets the keyframe interval
func (s *CaptureStrategy) SetKeyframeInterval(d time.Duration) {
	s.keyframeInterval = d
}
