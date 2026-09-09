package render

import (
	"github.com/gogpu/wgpu"
)

// BuildMapStrokes returns the segment buffer and, per rank, how many of its
// segments to draw at that level of detail. Segments are emitted in ascending
// rank, so a level of detail is a prefix.
//
// It operates on generic StrokeGeometry rather than map-specific types.
func BuildMapStrokes(
	dev *wgpu.Device,
	strokeGeoms []StrokeGeometry,
	strokeWidthPx float32,
	minSegmentPx float32,
) (*StrokeBuffer, [MaxRank + 1]int) {
	// Pre-count: worst case one segment per coordinate pair across all strokes.
	total := 0
	for i := range strokeGeoms {
		n := len(strokeGeoms[i].Coords) / 2
		if n >= 2 {
			total += n - 1
		}
	}

	var byRank [MaxRank + 1]int
	if total == 0 {
		return &StrokeBuffer{}, byRank
	}

	allSegs := make([]StrokeSegment, 0, total)

	for _, sg := range strokeGeoms {
		n := len(sg.Coords) / 2
		if n < 2 {
			continue
		}

		// Simplify: skip short segments.
		points := simplifyPolyline(sg.Coords, sg.Closed, minSegmentPx)

		segs := BuildSegments(
			points,
			sg.Color,
			strokeWidthPx,
			WidthModePixels,
			0, // layer 0 for now; draw order comes from pass ordering
		)
		allSegs = append(allSegs, segs...)

		r := clampRank(sg.Rank)
		for i := r; i <= MaxRank; i++ {
			byRank[i] = len(allSegs)
		}
	}

	if len(allSegs) == 0 {
		return &StrokeBuffer{}, byRank
	}
	for i := range byRank {
		if byRank[i] == 0 {
			byRank[i] = len(allSegs)
		}
	}

	buf := NewStrokeBuffer(dev, len(allSegs))
	buf.WriteAll(allSegs)
	return buf, byRank
}

// simplifyPolyline converts int32 coords to float32 points and skips
// segments shorter than minSegPx.
func simplifyPolyline(coords []int32, closed bool, minSegPx float32) [][2]float32 {
	n := len(coords) / 2
	if n < 2 {
		return nil
	}
	minSeg2 := minSegPx * minSegPx

	points := make([][2]float32, 0, n)
	points = append(points, [2]float32{float32(coords[0]), float32(coords[1])})

	lastX, lastY := float32(coords[0]), float32(coords[1])
	for i := 1; i < n; i++ {
		x, y := float32(coords[i*2]), float32(coords[i*2+1])
		dx, dy := x-lastX, y-lastY
		if dx*dx+dy*dy < minSeg2 {
			continue
		}
		points = append(points, [2]float32{x, y})
		lastX, lastY = x, y
	}
	if closed && len(points) >= 2 {
		first := points[0]
		dx := first[0] - lastX
		dy := first[1] - lastY
		if dx*dx+dy*dy >= minSeg2 {
			points = append(points, first)
		}
	}
	return points
}
