package render

import (
	_ "embed"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/instanced.wgsl
var instancedVertWGSL string

//go:embed shaders/fragment.wgsl
var instancedFragWGSL string

// CameraUniform is the GPU-side camera data. Must be 80 bytes to match WGSL
// struct alignment (mat4x4 + vec2 = 72 bytes, rounded up to next multiple of 16).
type CameraUniform struct {
	ViewProj [16]float32 // 64 bytes
	Viewport [2]float32  // 8 bytes
	_        [8]byte     // padding to 80 bytes
}

// CameraUniformSize is the byte size of the CameraUniform struct (80 bytes).
const CameraUniformSize = 80

// Pipeline manages a single instanced render pipeline. The camera it draws
// through is the renderer's, bound at group 0, so the pipeline owns no uniform
// of its own.
type Pipeline struct {
	pipe   *wgpu.RenderPipeline
	pl     *wgpu.PipelineLayout
	format gputypes.TextureFormat
}

// NewPipeline creates an instanced render pipeline against the engine's shared
// camera layout.
func NewPipeline(dev *wgpu.Device, format gputypes.TextureFormat, camBGL *wgpu.BindGroupLayout) *Pipeline {
	p := &Pipeline{format: format}
	p.create(dev, camBGL)
	return p
}

func (p *Pipeline) create(dev *wgpu.Device, camBGL *wgpu.BindGroupLayout) {
	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "instanced vert",
		WGSL:  instancedVertWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer vertMod.Release()

	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "instanced frag",
		WGSL:  instancedFragWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer fragMod.Release()

	p.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "instanced pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{camBGL},
	})
	if err != nil {
		panic(err)
	}

	p.pipe, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "instanced pipeline",
		Layout: p.pl,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{
				MeshVertexLayout,     // slot 0: mesh vertices (step = Vertex)
				InstanceBufferLayout, // slot 1: instance data (step = Instance)
			},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    p.format,
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
}

// BlendAlpha returns the standard alpha-blend state used by most pipelines.
func BlendAlpha() *gputypes.BlendState {
	s := gputypes.BlendStateAlpha()
	return &s
}

// Pipeline returns the underlying render pipeline.
func (p *Pipeline) Pipeline() *wgpu.RenderPipeline {
	return p.pipe
}

// Release releases GPU resources.
func (p *Pipeline) Release() {
	if p.pipe != nil {
		p.pipe.Release()
	}
	if p.pl != nil {
		p.pl.Release()
	}
}
