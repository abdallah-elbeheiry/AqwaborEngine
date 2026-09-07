package render

import (
	_ "embed"
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/vertex.wgsl
var legacyVertWGSL string

//go:embed shaders/fragment.wgsl
var legacyFragWGSL string

var log = logx.With("component", "render")

// DeviceProvider gives the renderer access to GPU resources.
type DeviceProvider interface {
	Device() *wgpu.Device
	Queue() *wgpu.Queue
	SurfaceFormat() gputypes.TextureFormat
}

// Renderer owns the per-frame render pass and all draw pipelines.
// It replaces the old Window.Draw per-call buffer allocation pattern.
type Renderer struct {
	dev    *wgpu.Device
	queue  *wgpu.Queue
	format gputypes.TextureFormat

	pass      *wgpu.RenderPassEncoder
	frameOpen bool

	instPipe *Pipeline            // instanced draws
	vertPipe *wgpu.RenderPipeline // legacy vertex draws

	// Reusable vertex buffer for DrawVertices (grown as needed)
	vertBuf    *wgpu.Buffer
	vertBufCap int
	vertCount  uint32

	stats FrameStats
}

// FrameStats tracks per-frame rendering metrics.
type FrameStats struct {
	DrawCalls int
	Instances int
	Triangles int
}

// NewRenderer creates a renderer from a device provider.
func NewRenderer(dp DeviceProvider) *Renderer {
	dev := dp.Device()
	queue := dp.Queue()
	format := dp.SurfaceFormat()

	r := &Renderer{
		dev:    dev,
		queue:  queue,
		format: format,
	}

	// Instanced pipeline
	r.instPipe = NewPipeline(dev, format)

	// Legacy vertex pipeline (for variable-topology content)
	r.vertPipe = r.createVertPipeline(dev, format)

	return r
}

func (r *Renderer) createVertPipeline(dev *wgpu.Device, format gputypes.TextureFormat) *wgpu.RenderPipeline {
	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "vert",
		WGSL:  legacyVertWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer vertMod.Release()

	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "frag",
		WGSL:  legacyFragWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer fragMod.Release()

	layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "vertex pll",
		BindGroupLayouts: nil,
	})
	if err != nil {
		panic(err)
	}

	pipe, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "vertex pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers:    []gputypes.VertexBufferLayout{vertexLayout},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    format,
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
	return pipe
}

// --- Frame lifecycle ---

// ClearAndBeginFrame begins a render pass that clears the screen.
func (r *Renderer) ClearAndBeginFrame(dc *gogpu.Context, cr, cg, cb, ca float32) error {
	enc := dc.CommandEncoder()
	if enc == nil {
		return nil
	}
	view := dc.SurfaceView()
	if view == nil {
		return nil
	}

	r.stats = FrameStats{}

	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gputypes.LoadOpClear,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: gputypes.Color{R: float64(cr), G: float64(cg), B: float64(cb), A: float64(ca)},
		}},
	})
	if err != nil {
		return err
	}

	r.pass = pass
	r.frameOpen = true
	return nil
}

// BeginFrame opens a render pass that loads existing content.
func (r *Renderer) BeginFrame(dc *gogpu.Context) error {
	enc := dc.CommandEncoder()
	if enc == nil {
		return nil
	}
	view := dc.SurfaceView()
	if view == nil {
		return nil
	}

	r.stats = FrameStats{}

	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    view,
			LoadOp:  gputypes.LoadOpLoad,
			StoreOp: gputypes.StoreOpStore,
		}},
	})
	if err != nil {
		return err
	}

	r.pass = pass
	r.frameOpen = true
	return nil
}

// EndFrame closes the render pass.
func (r *Renderer) EndFrame() {
	if r.pass != nil {
		r.pass.End()
		r.pass = nil
	}
	r.frameOpen = false
}

// --- Instanced draws ---

// DrawInstanced submits an instanced draw command.
func (r *Renderer) DrawInstanced(mesh *Mesh, instances *InstanceBuffer) {
	if r.pass == nil || !r.frameOpen {
		return
	}
	if instances.Count() == 0 {
		return
	}

	r.pass.SetPipeline(r.instPipe.Pipeline())
	r.pass.SetBindGroup(0, r.instPipe.BindGroup(), nil)
	r.pass.SetVertexBuffer(0, mesh.VertexBuffer, 0)
	r.pass.SetVertexBuffer(1, instances.Buffer(), 0)
	r.pass.SetIndexBuffer(mesh.IndexBuffer, gputypes.IndexFormatUint32, 0)
	r.pass.DrawIndexed(mesh.IndexCount, uint32(instances.Count()), 0, 0, 0)

	r.stats.DrawCalls++
	r.stats.Instances += instances.Count()
	r.stats.Triangles += int(mesh.IndexCount/3) * instances.Count()
}

// DrawInstancedIndirect submits an indirect instanced draw.
func (r *Renderer) DrawInstancedIndirect(mesh *Mesh, indirectBuf *wgpu.Buffer) {
	if r.pass == nil || !r.frameOpen {
		return
	}
	r.pass.SetPipeline(r.instPipe.Pipeline())
	r.pass.SetBindGroup(0, r.instPipe.BindGroup(), nil)
	r.pass.SetVertexBuffer(0, mesh.VertexBuffer, 0)
	r.pass.SetIndexBuffer(mesh.IndexBuffer, gputypes.IndexFormatUint32, 0)
	r.pass.DrawIndexedIndirect(indirectBuf, 0)
	r.stats.DrawCalls++
}

// --- Variable-topology vertex draws ---

// UploadVertices uploads a vertex list to the GPU buffer.
// Must be called before the render pass (before ClearAndBeginFrame/BeginFrame).
func (r *Renderer) UploadVertices(vertices []Vertex) {
	if len(vertices) == 0 {
		return
	}
	needBytes := uint64(len(vertices) * 24)
	if r.vertBuf == nil || r.vertBuf.Size() < needBytes {
		r.growVertBuf(needBytes)
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), int(needBytes))
	r.queue.WriteBuffer(r.vertBuf, 0, src)
	r.vertCount = uint32(len(vertices))
}

// DrawVertices draws the previously uploaded vertex data.
// Must be called inside an active render pass (after ClearAndBeginFrame/BeginFrame).
func (r *Renderer) DrawVertices() {
	if r.pass == nil || !r.frameOpen || r.vertCount == 0 {
		return
	}
	r.pass.SetPipeline(r.vertPipe)
	r.pass.SetVertexBuffer(0, r.vertBuf, 0)
	r.pass.Draw(r.vertCount, 1, 0, 0)
	r.stats.DrawCalls++
	r.stats.Triangles += int(r.vertCount) / 3
	r.vertCount = 0
}

func (r *Renderer) growVertBuf(minBytes uint64) {
	if r.vertBuf != nil {
		r.vertBuf.Release()
	}
	// Double the capacity each time
	cap := r.vertBufCap * 2
	if cap < 1024 {
		cap = 1024
	}
	for uint64(cap)*24 < minBytes {
		cap *= 2
	}
	buf, err := r.dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "vertex stream",
		Size:  uint64(cap) * 24,
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	r.vertBuf = buf
	r.vertBufCap = cap
}

// --- Map mesh draws ---

// DrawMapMesh draws pre-built map geometry with the map pipeline.
// Must be called inside an active render pass.
func (r *Renderer) DrawMapMesh(mesh *MapMesh, pipe *MapPipeline) {
	if r.pass == nil || !r.frameOpen || mesh == nil || mesh.Buffer == nil || mesh.Total == 0 {
		return
	}
	r.pass.SetPipeline(pipe.Pipeline())
	r.pass.SetBindGroup(0, pipe.BindGroup(), nil)
	r.pass.SetVertexBuffer(0, mesh.Buffer, 0)
	for _, pass := range mesh.Passes {
		if pass.Count == 0 {
			continue
		}
		r.pass.Draw(pass.Count, 1, pass.Offset, 0)
		r.stats.DrawCalls++
		r.stats.Triangles += int(pass.Count) / 3
	}
}

// --- Camera ---

// UpdateCamera updates the camera uniform for instanced draws.
func (r *Renderer) UpdateCamera(viewProj [16]float32, viewportW, viewportH float32) {
	r.instPipe.UpdateCamera(r.queue, viewProj, viewportW, viewportH)
}

// --- Accessors ---

func (r *Renderer) Device() *wgpu.Device                  { return r.dev }
func (r *Renderer) Queue() *wgpu.Queue                    { return r.queue }
func (r *Renderer) SurfaceFormat() gputypes.TextureFormat { return r.format }
func (r *Renderer) Stats() FrameStats                     { return r.stats }

// Release releases GPU resources.
func (r *Renderer) Release() {
	r.instPipe.Release()
	if r.vertPipe != nil {
		r.vertPipe.Release()
	}
	if r.vertBuf != nil {
		r.vertBuf.Release()
	}
}
