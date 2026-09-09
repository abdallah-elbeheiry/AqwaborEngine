package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

var log = logx.With("component", "render")

// DeviceProvider gives the renderer access to GPU resources.
type DeviceProvider interface {
	Device() *wgpu.Device
	Queue() *wgpu.Queue
	SurfaceFormat() gputypes.TextureFormat
}

// Renderer owns the per-frame render pass and all draw pipelines.
type Renderer struct {
	dev    *wgpu.Device
	queue  *wgpu.Queue
	format gputypes.TextureFormat

	// The frame has two phases. Begin captures the encoder and records what the
	// pass will do; the render pass itself is opened by the first draw. A
	// compute pass cannot be recorded inside a render pass, so compute work has
	// to have somewhere to go, and the previous shape gave it nowhere: the
	// encoder was only reachable once the pass was already open.
	pass      *wgpu.RenderPassEncoder
	enc       *wgpu.CommandEncoder // borrowed from gogpu.Context; valid during frame
	view      *wgpu.TextureView
	frameOpen bool // an encoder is captured
	passOpen  bool // the render pass is recording

	clear      gputypes.Color
	clearFirst bool

	instPipe   *Pipeline       // instanced draws
	strokePipe *StrokePipeline // screen-space-width polylines
	mapPipe    *MapPipeline    // map fill geometry (optional)

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

	// Stroke pipeline (screen-space-width polylines)
	r.strokePipe = NewStrokePipeline(dev, format)

	return r
}

// --- Instanced draws ---

// ClearAndBeginFrame captures the frame's command encoder and records that the
// render pass will clear to the given colour. It does not open the pass: the
// first draw does that, which leaves room between here and there for compute
// work to be encoded.
func (r *Renderer) ClearAndBeginFrame(dc *gogpu.Context, cr, cg, cb, ca float32) error {
	return r.beginFrame(dc, true, gputypes.Color{R: float64(cr), G: float64(cg), B: float64(cb), A: float64(ca)})
}

// BeginFrame captures the encoder for a frame that loads existing content
// rather than clearing.
func (r *Renderer) BeginFrame(dc *gogpu.Context) error {
	return r.beginFrame(dc, false, gputypes.Color{})
}

func (r *Renderer) beginFrame(dc *gogpu.Context, clear bool, c gputypes.Color) error {
	enc := dc.CommandEncoder()
	if enc == nil {
		return nil
	}
	view := dc.SurfaceView()
	if view == nil {
		return nil
	}

	r.stats = FrameStats{}
	r.enc = enc
	r.view = view
	r.clear = c
	r.clearFirst = clear
	r.frameOpen = true
	r.passOpen = false
	return nil
}

// ensurePass opens the render pass if it is not already recording. Every draw
// goes through it, so the pass opens on first use and a frame that draws
// nothing never opens one.
func (r *Renderer) ensurePass() bool {
	if r.passOpen {
		return true
	}
	if !r.frameOpen || r.enc == nil || r.view == nil {
		return false
	}

	loadOp := gputypes.LoadOpLoad
	if r.clearFirst {
		loadOp = gputypes.LoadOpClear
	}
	pass, err := r.enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       r.view,
			LoadOp:     loadOp,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: r.clear,
		}},
	})
	if err != nil {
		log.Error("failed to open the render pass", "err", err)
		return false
	}
	r.pass = pass
	r.passOpen = true
	return true
}

// PassOpen reports whether the render pass is already recording, which is what
// makes it too late to encode compute work for this frame.
func (r *Renderer) PassOpen() bool { return r.passOpen }

// EndFrame closes the render pass if one was opened, and drops the borrowed
// encoder. A frame that drew nothing opened no pass and has nothing to close.
func (r *Renderer) EndFrame() {
	if r.pass != nil {
		r.pass.End()
		r.pass = nil
	}
	r.enc = nil
	r.view = nil
	r.frameOpen = false
	r.passOpen = false
}

// --- Instanced draws ---

// DrawInstanced submits an instanced draw command.
// It flushes pending dirty ranges first, so callers do not need a separate
// Flush call (an explicit Flush beforehand is still fine and not duplicated:
// Flush is a no-op when no dirty ranges remain).
func (r *Renderer) DrawInstanced(mesh *Mesh, instances *InstanceBuffer) {
	if instances.Count() == 0 {
		return
	}
	if !r.ensurePass() {
		return
	}
	instances.Flush(r.queue)

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

// DrawInstancedIndirect submits an indirect instanced draw over culled data.
// The cull compute pass compacts survivors into cull.OutputBuffer() starting
// at slot 0 (FirstInstance = 0), so that buffer is bound as the instance
// vertex input and indirectBuf supplies the GPU-written instance count.
// Encode the cull dispatch before the render pass that calls this.
func (r *Renderer) DrawInstancedIndirect(mesh *Mesh, cull *CullPipeline) {
	if !r.ensurePass() {
		return
	}
	r.pass.SetPipeline(r.instPipe.Pipeline())
	r.pass.SetBindGroup(0, r.instPipe.BindGroup(), nil)
	r.pass.SetVertexBuffer(0, mesh.VertexBuffer, 0)
	r.pass.SetVertexBuffer(1, cull.OutputBuffer(), 0)
	r.pass.SetIndexBuffer(mesh.IndexBuffer, gputypes.IndexFormatUint32, 0)
	r.pass.DrawIndexedIndirect(cull.IndirectBuffer(), 0)
	r.stats.DrawCalls++
}

// --- Camera ---

// DrawMapMesh draws pre-built map geometry with the map pipeline.
// Must be called inside an active render pass.
func (r *Renderer) DrawMapMesh(mesh *MapMesh, pipe *MapPipeline) {
	if mesh == nil || mesh.Buffer == nil || mesh.Total == 0 {
		return
	}
	if !r.ensurePass() {
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

// UpdateStrokeCamera updates the camera uniform for stroke draws.
// The stroke pipeline needs the viewport size for screen-space width
// calculations, so it maintains a separate camera buffer.
func (r *Renderer) UpdateStrokeCamera(viewProj [16]float32, viewportW, viewportH float32) {
	r.strokePipe.UpdateCamera(r.queue, viewProj, viewportW, viewportH)
}

// --- Stroke draws ---

// DrawStrokes submits an instanced draw for stroke segments.
// Each segment is expanded into a screen-space quad by the vertex shader.
// Flushes pending dirty ranges automatically.
func (r *Renderer) DrawStrokes(segments *StrokeBuffer) {
	if segments.Count() == 0 {
		return
	}
	if !r.ensurePass() {
		return
	}
	segments.Flush(r.queue)

	r.pass.SetPipeline(r.strokePipe.Pipeline())
	r.pass.SetBindGroup(0, r.strokePipe.BindGroup(), nil)
	r.pass.SetVertexBuffer(0, r.strokePipe.QuadVertexBuffer(), 0)
	r.pass.SetVertexBuffer(1, segments.Buffer(), 0)
	r.pass.SetIndexBuffer(r.strokePipe.QuadIndexBuffer(), gputypes.IndexFormatUint16, 0)
	r.pass.DrawIndexed(6, uint32(segments.Count()), 0, 0, 0)

	r.stats.DrawCalls++
	r.stats.Instances += segments.Count()
	r.stats.Triangles += 2 * segments.Count() // 2 triangles per segment quad
}

// --- Accessors ---

func (r *Renderer) Device() *wgpu.Device                  { return r.dev }
func (r *Renderer) Queue() *wgpu.Queue                    { return r.queue }
func (r *Renderer) SurfaceFormat() gputypes.TextureFormat { return r.format }
func (r *Renderer) Stats() FrameStats                     { return r.stats }

// CommandEncoder returns the frame's command encoder, valid only during the
// current draw callback. Used by the GPU facade for compute cull dispatch.
func (r *Renderer) CommandEncoder() *wgpu.CommandEncoder { return r.enc }

// CameraBuffer returns the instanced pipeline's camera uniform buffer.
// Used by the GPU facade for compute cull binding.
func (r *Renderer) CameraBuffer() *wgpu.Buffer { return r.instPipe.CameraBuffer() }

// SetMapPipeline registers a map pipeline so SetCamera can update its camera
// uniform alongside the sprite and stroke pipelines.
func (r *Renderer) SetMapPipeline(p *MapPipeline) { r.mapPipe = p }

// Release releases GPU resources.
func (r *Renderer) Release() {
	r.instPipe.Release()
	r.strokePipe.Release()
}
