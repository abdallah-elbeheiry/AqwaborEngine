package render

import (
	_ "embed"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// SubcellInstance is the compact per-instance format for the layer that carries
// by far the most instances: loose material drawn as one coloured cell each.
//
// The colour is a palette index rather than an RGBA value. A cell's colour is a
// material identity, not an arbitrary number, so the instance carries a
// four-byte index into a ramp table uploaded once, against the sixteen bytes an
// RGBA colour costs. With the size and rotation a grid does not need either,
// the instance is 16 bytes rather than 64, which is a quarter of the per-frame
// bandwidth on that layer.
//
// It also changes what a moving light costs: a flashlight cone sweeping over
// cells becomes a change of row index per cell rather than a recolour.
type SubcellInstance struct {
	X, Y    float32 // offset 0, world position of the cell centre
	Palette uint32  // offset 8, index into the ramp table, plus one; 0 draws nothing
	_       uint32  // offset 12, padding to a 16-byte stride
}

const subcellInstanceSize = 16

var _ [subcellInstanceSize - unsafe.Sizeof(SubcellInstance{})]byte
var _ [unsafe.Sizeof(SubcellInstance{}) - subcellInstanceSize]byte

// SubcellBufferLayout is the vertex layout for the compact path. The mesh is a
// unit quad and the cell size comes from the pipeline uniform, so nothing here
// is per-instance except position and palette.
var SubcellBufferLayout = gputypes.VertexBufferLayout{
	ArrayStride: subcellInstanceSize,
	StepMode:    gputypes.VertexStepModeInstance,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 2},
		{Format: gputypes.VertexFormatUint32, Offset: 8, ShaderLocation: 3},
	},
}

// RampEntries is how many colours the ramp table holds.
//
// A material with several ramp rows takes one entry per row, so the table is
// materials times rows: 49 materials at 16 rows is 784. The table is a uniform
// buffer at 16 bytes an entry, so this is 16 KB, well inside the 64 KB a
// uniform binding is guaranteed.
const RampEntries = 1024

// RampTable is the palette the compact instances index into.
//
// Materials are numbered from zero and a cell carries PaletteOf(material),
// which is one more than the material. The shift is what reserves a value for
// "draws nothing": a cell carrying 0 is skipped, so it can be cleared without
// being taken out of the buffer. Entry zero of the table is an ordinary colour,
// the one material zero draws with.
//
// Use Set rather than writing Colors directly, and the shift stops being
// something to remember.
//
// How materials and rows map onto a flat material number belongs to the game,
// not here.
type RampTable struct {
	Colors [RampEntries][4]float32
}

// Set gives a material its colour. Materials are numbered from zero.
func (t *RampTable) Set(material int, r, g, b, a float32) {
	if material < 0 || material >= RampEntries {
		log.Error("material is outside the ramp table", "material", material, "entries", RampEntries)
		return
	}
	t.Colors[material] = [4]float32{r, g, b, a}
}

// PaletteOf is the value a cell carries to draw with a material's colour.
// Zero is not a material: a cell carrying it draws nothing.
func PaletteOf(material int) uint32 { return uint32(material) + 1 }

const rampTableSize = RampEntries * 16

// SubcellPipeline draws the compact instances. The camera is the engine's, at
// group 0; what this pipeline owns is the ramp table and the cell size, which
// sit at group 1 because group 0 belongs to the engine.
type SubcellPipeline struct {
	pipe      *wgpu.RenderPipeline
	bgl       *wgpu.BindGroupLayout
	pl        *wgpu.PipelineLayout
	rampBuf   *wgpu.Buffer
	cellBuf   *wgpu.Buffer
	bindGroup *wgpu.BindGroup
	format    gputypes.TextureFormat
	mesh      *Mesh
}

// cellUniform carries the size every cell is drawn at, which a grid shares, so
// it is not repeated per instance.
type cellUniform struct {
	Size [2]float32
	_    [8]byte
}

const cellUniformSize = 16

//go:embed shaders/subcell.wgsl
var subcellVertWGSL string

// NewSubcellPipeline builds the compact instanced pipeline against the engine's
// shared camera layout.
func NewSubcellPipeline(dev *wgpu.Device, queue *wgpu.Queue, format gputypes.TextureFormat, camBGL *wgpu.BindGroupLayout) *SubcellPipeline {
	p := &SubcellPipeline{format: format}

	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "subcell vert", WGSL: subcellVertWGSL})
	if err != nil {
		panic(err)
	}
	defer vertMod.Release()

	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "subcell frag", WGSL: instancedFragWGSL})
	if err != nil {
		panic(err)
	}
	defer fragMod.Release()

	newBuf := func(label string, size uint64, usage gputypes.BufferUsage) *wgpu.Buffer {
		b, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: size, Usage: usage})
		if err != nil {
			panic(err)
		}
		return b
	}
	uniform := gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst
	p.rampBuf = newBuf("ramp table", rampTableSize, uniform)
	p.cellBuf = newBuf("cell size", cellUniformSize, uniform)

	entry := func(binding uint32) gputypes.BindGroupLayoutEntry {
		return gputypes.BindGroupLayoutEntry{
			Binding:    binding,
			Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform},
		}
	}
	p.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "subcell params bgl",
		Entries: []gputypes.BindGroupLayoutEntry{entry(0), entry(1)},
	})
	if err != nil {
		panic(err)
	}

	p.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "subcell pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{camBGL, p.bgl},
	})
	if err != nil {
		panic(err)
	}

	p.bindGroup, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "subcell params bg",
		Layout: p.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: p.rampBuf, Size: rampTableSize},
			{Binding: 1, Buffer: p.cellBuf, Size: cellUniformSize},
		},
	})
	if err != nil {
		panic(err)
	}

	p.pipe, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "subcell pipeline",
		Layout: p.pl,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers:    []gputypes.VertexBufferLayout{MeshVertexLayout, SubcellBufferLayout},
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
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList},
	})
	if err != nil {
		panic(err)
	}

	p.mesh = NewUnitQuad(dev, queue)
	p.SetCellSize(queue, 1, 1)
	return p
}

// SetRamp uploads the palette. It is uploaded once and read by every instance,
// which is what makes the index cheaper than the colour it replaces.
func (p *SubcellPipeline) SetRamp(queue *wgpu.Queue, table *RampTable) {
	src := unsafe.Slice((*byte)(unsafe.Pointer(table)), rampTableSize)
	queue.WriteBuffer(p.rampBuf, 0, src)
}

// SetCellSize sets the size every cell is drawn at, in world units. A grid
// shares it, so it is a uniform rather than sixteen bytes on every instance.
func (p *SubcellPipeline) SetCellSize(queue *wgpu.Queue, w, h float32) {
	u := cellUniform{Size: [2]float32{w, h}}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u)), cellUniformSize)
	queue.WriteBuffer(p.cellBuf, 0, src)
}

func (p *SubcellPipeline) Pipeline() *wgpu.RenderPipeline { return p.pipe }

// ParamsBindGroup is the pipeline's own bindings - the ramp table and the cell
// size - which the draw sets at group 1.
func (p *SubcellPipeline) ParamsBindGroup() *wgpu.BindGroup { return p.bindGroup }

func (p *SubcellPipeline) Mesh() *Mesh { return p.mesh }

// Release frees the pipeline's GPU resources.
func (p *SubcellPipeline) Release() {
	for _, r := range []interface{ Release() }{p.pipe, p.bgl, p.pl, p.rampBuf, p.cellBuf, p.bindGroup} {
		if r != nil {
			r.Release()
		}
	}
	if p.mesh != nil {
		p.mesh.Release()
	}
}
