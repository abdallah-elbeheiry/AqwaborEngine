package render

import (
	"math"
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// PassRange records the vertex range for one DrawOrder pass in the shared buffer.
type PassRange struct {
	Offset uint32
	Count  uint32
}

// MapMesh holds pre-built GPU geometry for the entire map.
type MapMesh struct {
	Buffer *wgpu.Buffer
	Passes []PassRange
	Total  uint32
}

// MapMeshConfig controls how the map mesh is built.
type MapMeshConfig struct {
	StrokeWidthPx float32
	MinSegmentPx  float32
	RefZoom       float32
}

// BuildMapMesh triangulates fills and optionally stroke quads, uploading
// them to a single GPU vertex buffer.  When fillsOnly is true, only fill
// geometry is emitted (use with BuildMapStrokes for the stroke path).
func BuildMapMesh(
	dev *wgpu.Device,
	_ *wgpu.Queue,
	world *mapdata.World,
	fillTris [][]int32,
	cfg MapMeshConfig,
	fillsOnly bool,
) *MapMesh {
	vertices := make([]MapVertex, 0, 1<<20)

	var passes []PassRange

	for _, pass := range world.DrawOrder {
		layer := &world.Layers[pass.LayerIndex]
		start := uint32(len(vertices))

		for _, geomID := range layer.GeomIDs {
			n := int(world.GeomN[geomID])
			if n < 2 {
				continue
			}

			s := int(world.GeomStart[geomID])
			coords := world.Coords[s : s+n*2]

			if pass.FillColor != nil && layer.Kind == mapdata.KindRing && fillTris[geomID] != nil {
				emitMapFill(&vertices, coords, fillTris[geomID], *pass.FillColor)
			}
			if !fillsOnly && pass.StrokeColor != nil {
				emitMapStroke(&vertices, coords, layer.Kind == mapdata.KindRing,
					*pass.StrokeColor, cfg.StrokeWidthPx, cfg.MinSegmentPx, cfg.RefZoom, float32(world.Scale))
			}
		}

		passes = append(passes, PassRange{
			Offset: start,
			Count:  uint32(len(vertices)) - start,
		})
	}

	total := uint32(len(vertices))
	if total == 0 {
		return &MapMesh{}
	}

	bufBytes := int(total) * 12
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "map mesh",
		Size:             uint64(bufBytes),
		Usage:            gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
		MappedAtCreation: true,
	})
	if err != nil {
		panic(err)
	}
	mr, err := buf.MappedRange(0, uint64(bufBytes))
	if err != nil {
		panic(err)
	}
	copy(mr.Bytes(), unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), bufBytes))
	mr.Release()
	buf.Unmap()

	return &MapMesh{Buffer: buf, Passes: passes, Total: total}
}

func emitMapFill(out *[]MapVertex, coords []int32, tris []int32, c mapdata.Color) {
	cr, cg, cb, ca := quantizeColor(c)
	n := len(coords) / 2
	for _, idx := range tris {
		if int(idx) >= n {
			continue
		}
		*out = append(*out, MapVertex{
			PosX: coords[idx*2],
			PosY: coords[idx*2+1],
			R:    cr, G: cg, B: cb, A: ca,
		})
	}
}

// emitMapStroke works entirely in raw int32 coordinate space.
// coords are raw int32 (= degrees * scale).
// Perpendicular offset in raw units: 1 pixel = scale / zoom raw units.
func emitMapStroke(
	out *[]MapVertex,
	coords []int32,
	closed bool,
	c mapdata.Color,
	widthPx, minSegPx, refZoom, scale float32,
) {
	n := len(coords) / 2
	if n < 2 {
		return
	}
	if refZoom <= 0 {
		refZoom = 1
	}
	if scale <= 0 {
		scale = 1
	}

	cr, cg, cb, ca := quantizeColor(c)

	// Convert pixel measurements to raw int32 units.
	// 1 pixel = scale / refZoom raw units at the reference zoom.
	pixelsToRaw := float64(scale) / float64(refZoom)
	hw := float64(widthPx) / 2 * pixelsToRaw
	if hw < 0.5*pixelsToRaw {
		hw = 0.5 * pixelsToRaw
	}
	minSegRaw := float64(minSegPx) * pixelsToRaw
	minSegRaw2 := minSegRaw * minSegRaw

	lastX, lastY := float64(coords[0]), float64(coords[1])
	emitted := false

	for i := 1; i <= n; i++ {
		var px, py float64
		if i == n {
			if !closed {
				break
			}
			px, py = float64(coords[0]), float64(coords[1])
		} else {
			px, py = float64(coords[i*2]), float64(coords[i*2+1])
		}
		dx, dy := px-lastX, py-lastY
		if dx*dx+dy*dy < minSegRaw2 {
			continue
		}
		emitMapSegmentRaw(out, lastX, lastY, px, py, hw, cr, cg, cb, ca)
		lastX, lastY = px, py
		emitted = true
	}
	if !emitted && n >= 2 {
		bestD := float64(0)
		bestX, bestY := float64(coords[0]), float64(coords[1])
		for i := 1; i < n; i++ {
			dx := float64(coords[i*2]) - float64(coords[0])
			dy := float64(coords[i*2+1]) - float64(coords[1])
			if d := dx*dx + dy*dy; d > bestD {
				bestD = d
				bestX, bestY = float64(coords[i*2]), float64(coords[i*2+1])
			}
		}
		emitMapSegmentRaw(out, float64(coords[0]), float64(coords[1]), bestX, bestY, hw, cr, cg, cb, ca)
	}
}

func emitMapSegmentRaw(out *[]MapVertex, ax, ay, bx, by, hw float64, cr, cg, cb, ca uint8) {
	dx, dy := bx-ax, by-ay
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*hw, dx/l*hw

	*out = append(*out,
		MapVertex{PosX: int32(ax + nx), PosY: int32(ay + ny), R: cr, G: cg, B: cb, A: ca},
		MapVertex{PosX: int32(bx + nx), PosY: int32(by + ny), R: cr, G: cg, B: cb, A: ca},
		MapVertex{PosX: int32(bx - nx), PosY: int32(by - ny), R: cr, G: cg, B: cb, A: ca},

		MapVertex{PosX: int32(ax + nx), PosY: int32(ay + ny), R: cr, G: cg, B: cb, A: ca},
		MapVertex{PosX: int32(bx - nx), PosY: int32(by - ny), R: cr, G: cg, B: cb, A: ca},
		MapVertex{PosX: int32(ax - nx), PosY: int32(ay - ny), R: cr, G: cg, B: cb, A: ca},
	)
}

func quantizeColor(c mapdata.Color) (cr, cg, cb, ca uint8) {
	cr = uint8(clamp255(c.R))
	cg = uint8(clamp255(c.G))
	cb = uint8(clamp255(c.B))
	ca = uint8(clamp255(c.A))
	return
}

func clamp255(v float32) uint32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint32(v * 255)
}
