// Package render provides a GPU-driven render submission layer.
package render

import (
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// InstanceData is the per-instance data layout for instanced rendering.
//
// The stride is 64 bytes: the WGSL struct ends with `layer: f32` after a
// `vec2`, so in a `array<InstanceData>` storage buffer the struct alignment
// rounds the size up to a multiple of 16 (60 -> 64). The trailing Pad keeps
// the Go struct, the vertex stride, and the WGSL storage stride identical.
type InstanceData struct {
	Position [2]float32 // offset 0,  8 bytes
	Scale    [2]float32 // offset 8,  8 bytes
	Rotation float32    // offset 16, 4 bytes
	_        [12]byte   // offset 20, 12 bytes padding (vec4 alignment)
	Color    [4]float32 // offset 32, 16 bytes
	UVOffset [2]float32 // offset 48, 8 bytes
	Layer    float32    // offset 56, 4 bytes
	Pad      float32    // offset 60, 4 bytes padding (16-byte struct alignment)
}

const instanceDataSize = 64 // bytes, must match sizeof(InstanceData) and WGSL stride

// Compile-time guard. The old form compared the constant against 64, which a
// struct that drifted alongside a stale constant passed; this compares the
// constant against the struct the GPU actually reads.
var _ [instanceDataSize - unsafe.Sizeof(InstanceData{})]byte
var _ [unsafe.Sizeof(InstanceData{}) - instanceDataSize]byte

// unsafeSizeofInstanceData exposes the size for a test that says what drifted
// rather than only failing to compile.
func unsafeSizeofInstanceData() uintptr { return unsafe.Sizeof(InstanceData{}) }

func init() {
	if unsafe.Sizeof(InstanceData{}) != instanceDataSize {
		panic("render: sizeof(InstanceData) != instanceDataSize")
	}
}

// InstanceBufferLayout describes the vertex buffer layout for instanced attributes.
// Offsets must match the Go struct layout (which matches WGSL alignment).
var InstanceBufferLayout = gputypes.VertexBufferLayout{
	ArrayStride: instanceDataSize,
	StepMode:    gputypes.VertexStepModeInstance,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 2},  // Position  @ offset 0
		{Format: gputypes.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 3},  // Scale     @ offset 8
		{Format: gputypes.VertexFormatFloat32, Offset: 16, ShaderLocation: 4},   // Rotation  @ offset 16
		{Format: gputypes.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 5}, // Color     @ offset 32 (after vec4 padding)
		{Format: gputypes.VertexFormatFloat32x2, Offset: 48, ShaderLocation: 6}, // UVOffset  @ offset 48
		{Format: gputypes.VertexFormatFloat32, Offset: 56, ShaderLocation: 7},   // Layer     @ offset 56
	},
}

// DrawCmd is a single instanced draw command.
type DrawCmd struct {
	Mesh           *Mesh
	InstanceBuffer *InstanceBuffer
	FirstInstance  int
	InstanceCount  int
	Pipeline       *wgpu.RenderPipeline
}
