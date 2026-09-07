package render

import (
	"unsafe"

	"github.com/gogpu/gputypes"
)

const (
	WidthModePixels = 0 // width in screen pixels (constant under zoom)
	WidthModeWorld  = 1 // width in world units (scales with zoom)
)

// StrokeSegment is one line segment instance: two endpoints with
// neighbor context for miter joins.  The vertex shader expands each
// segment into a quad (4 vertices, index-buffered as 2 triangles).
//
// Layout (72 bytes, 16-byte aligned for WGSL storage):
//
//	Offset  Size  Field
//	 0       8    p0       (vec2<f32>)
//	 8       8    p1       (vec2<f32>)
//	16       8    prev     (vec2<f32>)
//	24       8    next     (vec2<f32>)
//	32      16    color0   (vec4<f32>)
//	48      16    color1   (vec4<f32>)
//	64       4    width    (f32)
//	68       1    flags    (u8: bit 0 = width mode)
//	69       1    layer    (u8)
//	70       2    pad
type StrokeSegment struct {
	P0     [2]float32 // endpoint 0
	P1     [2]float32 // endpoint 1
	Prev   [2]float32 // neighbor before P0 (for miter)
	Next   [2]float32 // neighbor after P1  (for miter)
	Color0 [4]float32 // color at P0
	Color1 [4]float32 // color at P1
	Width  float32    // stroke width
	Flags  uint8      // bit 0: 0=pixels, 1=world
	Layer  uint8      // draw order layer
	_pad   [2]byte
}

const strokeSegmentSize = 72 // bytes, must match sizeof(StrokeSegment) and WGSL

// Compile-time stride guard.
var _ [strokeSegmentSize - 72]byte
var _ [72 - strokeSegmentSize]byte

func init() {
	if unsafe.Sizeof(StrokeSegment{}) != strokeSegmentSize {
		panic("render: sizeof(StrokeSegment) != strokeSegmentSize")
	}
}

// StrokeSegmentLayout describes the vertex buffer layout for instanced stroke segments.
var StrokeSegmentLayout = gputypes.VertexBufferLayout{
	ArrayStride: strokeSegmentSize,
	StepMode:    gputypes.VertexStepModeInstance,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},  // p0
		{Format: gputypes.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 1},  // p1
		{Format: gputypes.VertexFormatFloat32x2, Offset: 16, ShaderLocation: 2}, // prev
		{Format: gputypes.VertexFormatFloat32x2, Offset: 24, ShaderLocation: 3}, // next
		{Format: gputypes.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 4}, // color0
		{Format: gputypes.VertexFormatFloat32x4, Offset: 48, ShaderLocation: 5}, // color1
		{Format: gputypes.VertexFormatFloat32, Offset: 64, ShaderLocation: 6},   // width
		{Format: gputypes.VertexFormatUint8x2, Offset: 68, ShaderLocation: 7},   // flags, layer
	},
}

// strokeQuadVerts are the 4 corner offsets for a unit quad.
// The vertex shader scales these by the computed offset vector.
var strokeQuadVerts = []StrokeQuadVertex{
	{X: -1, Y: -1}, // corner 0: left  side, start end
	{X: 1, Y: -1},  // corner 1: right side, start end
	{X: 1, Y: 1},   // corner 2: right side, end end
	{X: -1, Y: 1},  // corner 3: left  side, end end
}

// StrokeQuadVertex is a minimal vertex for the stroke quad mesh.
type StrokeQuadVertex struct {
	X, Y float32
}

var strokeQuadVertexLayout = gputypes.VertexBufferLayout{
	ArrayStride: 8,
	StepMode:    gputypes.VertexStepModeVertex,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 8},
	},
}

// strokeQuadIndices indexes the 4 corner vertices into 2 triangles.
var strokeQuadIndices = []uint16{0, 1, 2, 0, 2, 3}

// BuildSegments converts a polyline (ordered points) into StrokeSegments
// with adjacency for miter joins.  End segments duplicate the endpoint as
// the missing neighbor.
//
// width is the stroke width; flags encodes WidthModePixels/WidthModeWorld.
// color applies uniformly to all segments; per-vertex colors in the output
// are all set to this value.
func BuildSegments(points [][2]float32, color [4]float32, width float32, flags uint8, layer uint8) []StrokeSegment {
	n := len(points)
	if n < 2 {
		return nil
	}
	segs := make([]StrokeSegment, 0, n-1)
	for i := 0; i < n-1; i++ {
		prev := points[i]
		if i > 0 {
			prev = points[i-1]
		}
		next := points[i+1]
		if i+2 < n {
			next = points[i+2]
		}
		segs = append(segs, StrokeSegment{
			P0:     points[i],
			P1:     points[i+1],
			Prev:   prev,
			Next:   next,
			Color0: color,
			Color1: color,
			Width:  width,
			Flags:  flags,
			Layer:  layer,
		})
	}
	return segs
}
