package maprender

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
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
) (*render.StrokeBuffer, [MaxRank + 1]int) {
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
		return &render.StrokeBuffer{}, byRank
	}

	allSegs := make([]render.StrokeSegment, 0, total)

	for _, sg := range strokeGeoms {
		n := len(sg.Coords) / 2
		if n < 2 {
			continue
		}

		// Simplify: skip short segments.
		points := render.SimplifyPolyline(sg.Coords, sg.Closed, minSegmentPx)

		segs := render.BuildSegments(
			points,
			sg.Color,
			strokeWidthPx,
			render.WidthModePixels,
			0, // layer 0 for now; draw order comes from pass ordering
		)
		allSegs = append(allSegs, segs...)

		r := clampRank(sg.Rank)
		for i := r; i <= MaxRank; i++ {
			byRank[i] = len(allSegs)
		}
	}

	if len(allSegs) == 0 {
		return &render.StrokeBuffer{}, byRank
	}
	for i := range byRank {
		if byRank[i] == 0 {
			byRank[i] = len(allSegs)
		}
	}

	buf := render.NewStrokeBuffer(dev, len(allSegs))
	buf.WriteAll(allSegs)
	return buf, byRank
}
