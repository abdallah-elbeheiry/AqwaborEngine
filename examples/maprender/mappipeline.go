package maprender

import (
	_ "embed"

	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/map.wgsl
var mapWGSL string

//go:embed shaders/fragment.wgsl
var mapFragWGSL string

// MapPipeline renders pre-built map geometry through the engine's camera.
// Positions are int32 world-space coords; the viewProj matrix bakes in
// the scale divisor so the shader just multiplies.
//
// It is an example of a pipeline built outside the engine: it lists the
// engine's camera layout as group 0, reads the camera at group(0) binding(0),
// and owns no camera buffer. render.GPU binds and updates that for it, so
// nothing here has to be told when the view moves.
type MapPipeline struct {
	pipe   *wgpu.RenderPipeline
	pl     *wgpu.PipelineLayout
	format gputypes.TextureFormat
}

// NewMapPipeline creates the pipeline for pre-built map geometry. camBGL is
// render.GPU.CameraLayout().
func NewMapPipeline(dev *wgpu.Device, format gputypes.TextureFormat, camBGL *wgpu.BindGroupLayout) *MapPipeline {
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

	p.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "map pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{camBGL},
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
			Buffers:    []gputypes.VertexBufferLayout{MapVertexLayout},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    p.format,
				Blend:     render.BlendAlpha(),
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

func (p *MapPipeline) Pipeline() *wgpu.RenderPipeline { return p.pipe }

func (p *MapPipeline) Release() {
	if p.pipe != nil {
		p.pipe.Release()
	}
	if p.pl != nil {
		p.pl.Release()
	}
}
