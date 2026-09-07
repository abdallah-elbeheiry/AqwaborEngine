package render

import "github.com/gogpu/gputypes"

// Vertex is clip-space position + color.
// Used for variable-topology content (map geometry, debug lines, etc.)
// where a shared instanced mesh doesn't fit.
type Vertex struct {
	X, Y       float32
	R, G, B, A float32
}

// vertexLayout describes the vertex buffer layout for Vertex (locations 0-1).
var vertexLayout = gputypes.VertexBufferLayout{
	ArrayStride: 24,
	StepMode:    gputypes.VertexStepModeVertex,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 8, ShaderLocation: 1},
	},
}
