package render

import (
	_ "embed"
	"unsafe"

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

const cameraUniformSize = 80

// Pipeline manages a single instanced render pipeline with its bind group.
type Pipeline struct {
	pipe      *wgpu.RenderPipeline
	bgl       *wgpu.BindGroupLayout
	pl        *wgpu.PipelineLayout
	cameraBuf *wgpu.Buffer
	bindGroup *wgpu.BindGroup
	format    gputypes.TextureFormat
}

// NewPipeline creates an instanced render pipeline.
func NewPipeline(dev *wgpu.Device, format gputypes.TextureFormat) *Pipeline {
	p := &Pipeline{format: format}
	p.create(dev)
	return p
}

func (p *Pipeline) create(dev *wgpu.Device) {
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

	// Camera uniform buffer
	p.cameraBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "camera uniform",
		Size:  cameraUniformSize,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	// Bind group layout: binding 0 = camera uniform (vertex + fragment)
	p.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "instanced bgl",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
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
		Label:            "instanced pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{p.bgl},
	})
	if err != nil {
		panic(err)
	}

	// Bind group (will be recreated if camera buffer changes)
	p.bindGroup, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "instanced bg",
		Layout: p.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: p.cameraBuf, Size: cameraUniformSize},
		},
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
}

func blendAlpha() *gputypes.BlendState {
	s := gputypes.BlendStateAlpha()
	return &s
}

// UpdateCamera writes the camera view-projection matrix and viewport to the GPU.
func (p *Pipeline) UpdateCamera(queue *wgpu.Queue, viewProj [16]float32, viewportW, viewportH float32) {
	u := CameraUniform{
		ViewProj: viewProj,
		Viewport: [2]float32{viewportW, viewportH},
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u)), cameraUniformSize)
	queue.WriteBuffer(p.cameraBuf, 0, src)
}

// Pipeline returns the underlying render pipeline.
func (p *Pipeline) Pipeline() *wgpu.RenderPipeline {
	return p.pipe
}

// BindGroup returns the camera bind group.
func (p *Pipeline) BindGroup() *wgpu.BindGroup {
	return p.bindGroup
}

// CameraBuffer returns the camera uniform buffer (for compute cull binding).
func (p *Pipeline) CameraBuffer() *wgpu.Buffer {
	return p.cameraBuf
}

// Release releases GPU resources.
func (p *Pipeline) Release() {
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
