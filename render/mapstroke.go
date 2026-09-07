package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/gogpu/wgpu"
)

// BuildMapStrokes extracts polylines from all stroke passes and builds
// StrokeSegments for GPU expansion.  Fills are not included — use
// BuildMapMesh (with fillsOnly=true) for those.
//
// Each DrawOrder pass with a StrokeColor contributes segments for every
// geometry in its layer.  Closed rings are closed polylines.
func BuildMapStrokes(
	dev *wgpu.Device,
	world *mapdata.World,
	strokeWidthPx float32,
	minSegmentPx float32,
) *StrokeBuffer {
	// Pre-count: worst case one segment per coordinate pair across all strokes.
	total := 0
	for _, pass := range world.DrawOrder {
		if pass.StrokeColor == nil {
			continue
		}
		layer := &world.Layers[pass.LayerIndex]
		for _, gid := range layer.GeomIDs {
			n := int(world.GeomN[gid])
			if n >= 2 {
				total += n - 1
			}
		}
	}

	if total == 0 {
		return &StrokeBuffer{}
	}

	allSegs := make([]StrokeSegment, 0, total)

	for _, pass := range world.DrawOrder {
		if pass.StrokeColor == nil {
			continue
		}
		c := pass.StrokeColor
		color := [4]float32{
			float32(clamp255(c.R)) / 255,
			float32(clamp255(c.G)) / 255,
			float32(clamp255(c.B)) / 255,
			float32(clamp255(c.A)) / 255,
		}
		layer := &world.Layers[pass.LayerIndex]
		closed := layer.Kind == mapdata.KindRing

		for _, gid := range layer.GeomIDs {
			n := int(world.GeomN[gid])
			if n < 2 {
				continue
			}
			s := int(world.GeomStart[gid])
			coords := world.Coords[s : s+n*2]

			// Simplify: skip short segments (same logic as old emitMapStroke).
			points := simplifyPolyline(coords, closed, minSegmentPx, float32(world.Scale))

			segs := BuildSegments(
				points,
				color,
				strokeWidthPx,
				WidthModePixels,
				0, // layer 0 for now; draw order comes from pass ordering
			)
			allSegs = append(allSegs, segs...)
		}
	}

	if len(allSegs) == 0 {
		return &StrokeBuffer{}
	}

	buf := NewStrokeBuffer(dev, len(allSegs))
	buf.WriteAll(allSegs)
	return buf
}

// simplifyPolyline converts int32 coords to float32 points and skips
// segments shorter than minSegPx (in pixels at the reference zoom).
// This replicates the old CPU min-segment filter.
func simplifyPolyline(coords []int32, closed bool, minSegPx, scale float32) [][2]float32 {
	n := len(coords) / 2
	if n < 2 {
		return nil
	}
	if scale <= 0 {
		scale = 1
	}
	// minSegPx in world units: 1 pixel = 1 raw unit (positions are raw int32).
	minSeg2 := float32(minSegPx * minSegPx)

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
		// Close the polyline: ensure last point connects back to first.
		first := points[0]
		dx := first[0] - lastX
		dy := first[1] - lastY
		if dx*dx+dy*dy >= minSeg2 {
			points = append(points, first)
		}
	}
	return points
}
