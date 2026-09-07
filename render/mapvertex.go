package render

import "github.com/gogpu/gputypes"

// MapVertex is a compact 12-byte vertex for pre-built map geometry.
// Positions are raw int32 world-space coords (divided by Scale in the shader).
// Colors are quantized to uint8 (2x compression vs float32 Vertex).
type MapVertex struct {
	PosX, PosY int32
	R, G, B, A uint8
}

// mapVertexLayout describes the buffer layout for MapVertex.
var mapVertexLayout = gputypes.VertexBufferLayout{
	ArrayStride: 12,
	StepMode:    gputypes.VertexStepModeVertex,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatSint32x2, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatUnorm8x4, Offset: 8, ShaderLocation: 1},
	},
}
