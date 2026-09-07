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

	pass      *wgpu.RenderPassEncoder
	enc       *wgpu.CommandEncoder // borrowed from gogpu.Context; valid during frame
	frameOpen bool

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
	r.enc = enc

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
	r.enc = enc

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
	r.enc = nil
	r.frameOpen = false
}

// --- Instanced draws ---

// DrawInstanced submits an instanced draw command.
// It flushes pending dirty ranges first, so callers do not need a separate
// Flush call (an explicit Flush beforehand is still fine and not duplicated:
// Flush is a no-op when no dirty ranges remain).
func (r *Renderer) DrawInstanced(mesh *Mesh, instances *InstanceBuffer) {
	if r.pass == nil || !r.frameOpen {
		return
	}
	if instances.Count() == 0 {
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
	if r.pass == nil || !r.frameOpen {
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
	if r.pass == nil || !r.frameOpen {
		return
	}
	if segments.Count() == 0 {
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
