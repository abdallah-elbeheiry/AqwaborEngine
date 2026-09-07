package render

import (
	_ "embed"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/stroke.wgsl
var strokeVertWGSL string

//go:embed shaders/stroke_frag.wgsl
var strokeFragWGSL string

// StrokePipeline renders screen-space-width polylines via vertex-shader
// expansion of segment instances.  Each segment is one instance; the
// vertex shader expands it into a quad using adjacency for miter joins.
//
// Bindings:
//
//	group(0) binding(0) = Camera uniform (viewProj + viewport)
//	slot(0)            = quad vertex buffer (4 corners, 8 bytes each)
//	slot(1)            = segment instance buffer (StrokeSegment, 48 bytes)
//	index buffer       = quad indices (6 x uint16)
type StrokePipeline struct {
	pipe      *wgpu.RenderPipeline
	bgl       *wgpu.BindGroupLayout
	pl        *wgpu.PipelineLayout
	cameraBuf *wgpu.Buffer
	bindGroup *wgpu.BindGroup
	format    gputypes.TextureFormat

	// Quad mesh (unit quad, index-buffered as 2 triangles).
	quadVerts *wgpu.Buffer
	quadIdx   *wgpu.Buffer
}

// NewStrokePipeline creates the stroke render pipeline.
func NewStrokePipeline(dev *wgpu.Device, format gputypes.TextureFormat) *StrokePipeline {
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

	// Camera uniform buffer
	sp.cameraBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "stroke camera",
		Size:  cameraUniformSize,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	// Bind group layout: binding 0 = camera uniform (vertex stage)
	sp.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "stroke bgl",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: gputypes.ShaderStageVertex,
				Buffer: &gputypes.BufferBindingLayout{
					Type: gputypes.BufferBindingTypeUniform,
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	sp.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "stroke pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{sp.bgl},
	})
	if err != nil {
		panic(err)
	}

	sp.bindGroup, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "stroke bg",
		Layout: sp.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: sp.cameraBuf, Size: cameraUniformSize},
		},
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
				Blend:     blendAlpha(),
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

// UpdateCamera writes the camera view-projection matrix and viewport to the GPU.
func (sp *StrokePipeline) UpdateCamera(queue *wgpu.Queue, viewProj [16]float32, viewportW, viewportH float32) {
	u := CameraUniform{
		ViewProj: viewProj,
		Viewport: [2]float32{viewportW, viewportH},
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u)), cameraUniformSize)
	queue.WriteBuffer(sp.cameraBuf, 0, src)
}

// Pipeline returns the underlying render pipeline.
func (sp *StrokePipeline) Pipeline() *wgpu.RenderPipeline { return sp.pipe }

// BindGroup returns the camera bind group.
func (sp *StrokePipeline) BindGroup() *wgpu.BindGroup { return sp.bindGroup }

// CameraBuffer returns the camera uniform buffer (for sharing with compute cull).
func (sp *StrokePipeline) CameraBuffer() *wgpu.Buffer { return sp.cameraBuf }

// QuadVertexBuffer returns the unit quad vertex buffer (4 corners, 8 bytes each).
func (sp *StrokePipeline) QuadVertexBuffer() *wgpu.Buffer { return sp.quadVerts }

// QuadIndexBuffer returns the unit quad index buffer (6 x uint16).
func (sp *StrokePipeline) QuadIndexBuffer() *wgpu.Buffer { return sp.quadIdx }

// Release releases GPU resources.
func (sp *StrokePipeline) Release() {
	if sp.pipe != nil {
		sp.pipe.Release()
	}
	if sp.bgl != nil {
		sp.bgl.Release()
	}
	if sp.pl != nil {
		sp.pl.Release()
	}
	if sp.cameraBuf != nil {
		sp.cameraBuf.Release()
	}
	if sp.bindGroup != nil {
		sp.bindGroup.Release()
	}
	if sp.quadVerts != nil {
		sp.quadVerts.Release()
	}
	if sp.quadIdx != nil {
		sp.quadIdx.Release()
	}
}
