package render

import (
	_ "embed"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/map.wgsl
var mapWGSL string

//go:embed shaders/fragment.wgsl
var mapFragWGSL string

// MapPipeline renders pre-built map geometry with a camera uniform.
// Positions are int32 world-space coords; the viewProj matrix bakes in
// the scale divisor so the shader just multiplies.
type MapPipeline struct {
	pipe      *wgpu.RenderPipeline
	bgl       *wgpu.BindGroupLayout
	pl        *wgpu.PipelineLayout
	cameraBuf *wgpu.Buffer
	bindGroup *wgpu.BindGroup
	format    gputypes.TextureFormat
}

// NewMapPipeline creates the pipeline for pre-built map geometry.
func NewMapPipeline(dev *wgpu.Device, format gputypes.TextureFormat) *MapPipeline {
	p := &MapPipeline{format: format}

	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "map vert",
		WGSL:  mapWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer vertMod.Release()

	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "map frag",
		WGSL:  mapFragWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer fragMod.Release()

	p.cameraBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "map camera",
		Size:  cameraUniformSize,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	p.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "map bgl",
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

	p.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "map pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{p.bgl},
	})
	if err != nil {
		panic(err)
	}

	p.bindGroup, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "map bg",
		Layout: p.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: p.cameraBuf, Size: cameraUniformSize},
		},
	})
	if err != nil {
		panic(err)
	}

	p.pipe, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "map pipeline",
		Layout: p.pl,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers:    []gputypes.VertexBufferLayout{mapVertexLayout},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    p.format,
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

	return p
}

// UpdateCamera writes the camera view-projection matrix to the GPU.
// viewProj should have the world-scale baked in (i.e. positions are raw int32).
func (p *MapPipeline) UpdateCamera(queue *wgpu.Queue, viewProj [16]float32) {
	src := unsafe.Slice((*byte)(unsafe.Pointer(&viewProj[0])), cameraUniformSize)
	queue.WriteBuffer(p.cameraBuf, 0, src)
}

func (p *MapPipeline) Pipeline() *wgpu.RenderPipeline { return p.pipe }
func (p *MapPipeline) BindGroup() *wgpu.BindGroup     { return p.bindGroup }

func (p *MapPipeline) Release() {
	if p.pipe != nil {
		p.pipe.Release()
	}
	if p.bgl != nil {
		p.bgl.Release()
	}
	if p.pl != nil {
		p.pl.Release()
	}
	if p.cameraBuf != nil {
		p.cameraBuf.Release()
	}
	if p.bindGroup != nil {
		p.bindGroup.Release()
	}
}
