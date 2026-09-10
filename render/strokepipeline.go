package render

import (
	_ "embed"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/stroke.wgsl
var strokeVertWGSL string

//go:embed shaders/fragment.wgsl
var strokeFragWGSL string

// StrokePipeline renders screen-space-width polylines via vertex-shader
// expansion of segment instances.  Each segment is one instance; the
// vertex shader expands it into a quad using adjacency for miter joins.
//
// Bindings:
//
//	group(0) binding(0) = the engine's shared camera uniform (viewProj + viewport)
//	slot(0)            = quad vertex buffer (4 corners, 8 bytes each)
//	slot(1)            = segment instance buffer (StrokeSegment, 48 bytes)
//	index buffer       = quad indices (6 x uint16)
type StrokePipeline struct {
	pipe   *wgpu.RenderPipeline
	pl     *wgpu.PipelineLayout
	format gputypes.TextureFormat

	// Quad mesh (unit quad, index-buffered as 2 triangles).
	quadVerts *wgpu.Buffer
	quadIdx   *wgpu.Buffer
}

// NewStrokePipeline creates the stroke render pipeline against the engine's
// shared camera layout.
func NewStrokePipeline(dev *wgpu.Device, format gputypes.TextureFormat, camBGL *wgpu.BindGroupLayout) *StrokePipeline {
	sp := &StrokePipeline{format: format}

	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "stroke vert",
		WGSL:  strokeVertWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer vertMod.Release()

	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "stroke frag",
		WGSL:  strokeFragWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer fragMod.Release()

	sp.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "stroke pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{camBGL},
	})
	if err != nil {
		panic(err)
	}

	// Quad vertex buffer (4 corners)
	sp.quadVerts, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "stroke quad verts",
		Size:             uint64(len(strokeQuadVerts)) * 8,
		Usage:            gputypes.BufferUsageVertex,
		MappedAtCreation: true,
	})
	if err != nil {
		panic(err)
	}
	mr, err := sp.quadVerts.MappedRange(0, uint64(len(strokeQuadVerts))*8)
	if err != nil {
		panic(err)
	}
	copy(mr.Bytes(), unsafe.Slice((*byte)(unsafe.Pointer(&strokeQuadVerts[0])), len(strokeQuadVerts)*8))
	mr.Release()
	sp.quadVerts.Unmap()

	// Quad index buffer (6 x uint16)
	sp.quadIdx, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "stroke quad idx",
		Size:             uint64(len(strokeQuadIndices)) * 2,
		Usage:            gputypes.BufferUsageIndex,
		MappedAtCreation: true,
	})
	if err != nil {
		panic(err)
	}
	mr, err = sp.quadIdx.MappedRange(0, uint64(len(strokeQuadIndices))*2)
	if err != nil {
		panic(err)
	}
	copy(mr.Bytes(), unsafe.Slice((*byte)(unsafe.Pointer(&strokeQuadIndices[0])), len(strokeQuadIndices)*2))
	mr.Release()
	sp.quadIdx.Unmap()

	sp.pipe, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "stroke pipeline",
		Layout: sp.pl,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{
				strokeQuadVertexLayout, // slot 0: quad corners
				StrokeSegmentLayout,    // slot 1: segment instances
			},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    sp.format,
				Blend:     BlendAlpha(),
				WriteMask: gputypes.ColorWriteMaskAll,
			}},
		},
		Primitive: gputypes.PrimitiveState{
			Topology: gputypes.PrimitiveTopologyTriangleList,
		},
	})
	if err != nil {
		panic(err)
	}

	return sp
}

// Pipeline returns the underlying render pipeline.
func (sp *StrokePipeline) Pipeline() *wgpu.RenderPipeline { return sp.pipe }

// QuadVertexBuffer returns the unit quad vertex buffer (4 corners, 8 bytes each).
func (sp *StrokePipeline) QuadVertexBuffer() *wgpu.Buffer { return sp.quadVerts }

// QuadIndexBuffer returns the unit quad index buffer (6 x uint16).
func (sp *StrokePipeline) QuadIndexBuffer() *wgpu.Buffer { return sp.quadIdx }

// Release releases GPU resources.
func (sp *StrokePipeline) Release() {
	if sp.pipe != nil {
		sp.pipe.Release()
	}
	if sp.pl != nil {
		sp.pl.Release()
	}
	if sp.quadVerts != nil {
		sp.quadVerts.Release()
	}
	if sp.quadIdx != nil {
		sp.quadIdx.Release()
	}
}
