// Package render provides the GPU submission layer for the Aqwabor engine.
//
// The high-level entry point is GPU, which wraps all pipeline and buffer
// management behind a small set of methods:
//
//	gfx := render.New(dp)
//	gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
//	gfx.SetCamera(viewProj, vpW, vpH)
//	batch := gfx.Sprites(cap)
//	batch.SetAll(sprites)
//	gfx.DrawSprites(batch)
//	gfx.DrawSpritesCulled(batch, viewBounds)
//	gfx.DrawStrokes(segments)
//	gfx.End()
//
// Game code never touches wgpu.Buffer, bind groups, or WGSL directly.
package render

import (
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// Clear describes the clear colour for Begin.
type Clear struct {
	R, G, B, A float32
}

// ViewBounds defines an axis-aligned bounding box for GPU culling.
type ViewBounds struct {
	MinX, MinY float32
	MaxX, MaxY float32
}

// GPU is the public facade over the render submission layer. It owns the
// per-frame render pass, the pipelines and the resources.
//
// A frame has two phases. Begin captures the frame's command encoder without
// opening a render pass, so compute work has somewhere to go; the first draw
// opens the pass. A compute pass cannot be recorded inside a render pass, and
// the previous shape made the encoder reachable only once the pass was already
// open, so the cull dispatch was dropped and the indirect draw drew nothing.
//
//	gfx.Begin(dc, clear)
//	gfx.SetCamera(vp, w, h)
//	h := gfx.CullSprites(batch, bounds)   // compute, before any draw
//	gfx.DrawSpritesCulled(h)              // opens the pass
//	gfx.End()
type GPU struct {
	r *Renderer

	// Compute cull pipeline, created lazily on the first cull.
	cull     *CullPipeline
	cullMax  int
	cullMesh *Mesh // shared unit quad for culled draws

	// retired holds cull pipelines replaced by a larger one, released after
	// enough frames that no submitted command buffer still references them.
	retired []retiredCull
	frame   uint64
}

// retiredCull is a pipeline waiting out the frames a submitted command buffer
// might still be using it for.
type retiredCull struct {
	cull  *CullPipeline
	frame uint64
}

// retireAfter is how many frames a replaced pipeline is held before release.
// Releasing it at the moment of replacement freed buffers a frame already
// submitted could still read.
const retireAfter = 3

// New creates a GPU facade from a device provider.
func New(dp DeviceProvider) *GPU {
	return &GPU{r: NewRenderer(dp)}
}

// --- Frame lifecycle ---

// Begin captures the frame's command encoder and records that the pass will
// clear to c. It does not open the render pass; the first draw does, which is
// what leaves room for CullSprites in between.
func (g *GPU) Begin(dc *gogpu.Context, c Clear) error {
	g.frame++
	g.releaseRetired()
	if g.cull != nil {
		g.cull.BeginFrame()
	}
	return g.r.ClearAndBeginFrame(dc, c.R, c.G, c.B, c.A)
}

// BeginLoad is Begin for a frame that keeps what is already on the surface.
func (g *GPU) BeginLoad(dc *gogpu.Context) error {
	g.frame++
	g.releaseRetired()
	if g.cull != nil {
		g.cull.BeginFrame()
	}
	return g.r.BeginFrame(dc)
}

func (g *GPU) releaseRetired() {
	kept := g.retired[:0]
	for _, r := range g.retired {
		if g.frame-r.frame >= retireAfter {
			r.cull.Release()
			continue
		}
		kept = append(kept, r)
	}
	g.retired = kept
}

// End closes the current render pass.
func (g *GPU) End() {
	g.r.EndFrame()
}

// --- Camera ---

// SetCamera updates the camera view-projection for all registered pipelines
// (sprite, stroke, and map if set via Renderer.SetMapPipeline).
func (g *GPU) SetCamera(viewProj [16]float32, viewportW, viewportH float32) {
	g.r.UpdateCamera(viewProj, viewportW, viewportH)
	g.r.UpdateStrokeCamera(viewProj, viewportW, viewportH)
	if g.r.mapPipe != nil {
		g.r.mapPipe.UpdateCamera(g.r.queue, viewProj, viewportW, viewportH)
	}
}

// --- Instanced draws ---

// Sprites creates a sprite batch with the given capacity.
// The batch wraps a shared unit quad mesh and an InstanceBuffer.
func (g *GPU) Sprites(capacity int) *SpriteBatch {
	return NewSpriteBatch(g.r.Device(), g.r.Queue(), nil, capacity)
}

// DrawSprites draws all sprites in the batch (no culling).
func (g *GPU) DrawSprites(batch *SpriteBatch) {
	g.r.DrawInstanced(batch.meshInternal(), batch.bufferInternal())
}

// Culled names a cull dispatch that has been encoded and is waiting to be
// drawn. The zero value draws nothing.
type Culled struct {
	mesh *Mesh
	ok   bool
}

// CullSprites encodes a compute pass that tests every instance in the batch
// against viewBounds and the camera frustum, compacting the survivors and
// writing the draw count the GPU will use.
//
// It must be called after Begin and before the first draw of the frame, because
// a compute pass cannot be recorded inside a render pass. Calling it once the
// pass is open is refused rather than silently dropped, which is what used to
// happen.
//
// One cull a frame: the output and indirect buffers are single, so a second
// would overwrite the first.
func (g *GPU) CullSprites(batch *SpriteBatch, viewBounds ViewBounds) Culled {
	instances := batch.bufferInternal()
	if instances.Count() == 0 {
		return Culled{}
	}
	if g.r.PassOpen() {
		log.Error("CullSprites called after the render pass opened; a compute pass cannot be recorded inside one. Call it between Begin and the first draw.")
		return Culled{}
	}
	enc := g.r.CommandEncoder()
	if enc == nil {
		log.Error("CullSprites called outside a frame; call Begin first")
		return Culled{}
	}

	g.ensureCull(instances.Count())
	if !g.cull.Claim() {
		log.Error("CullSprites called twice in one frame; only one culled draw is supported")
		return Culled{}
	}

	mesh := batch.meshInternal()
	if mesh == nil {
		mesh = g.unitQuad()
	}

	g.cull.ResetIndirect(mesh.IndexCount)
	g.cull.EncodeDispatch(
		enc,
		instances.Buffer(),
		g.r.CameraBuffer(),
		instances.Count(),
		[2]float32{viewBounds.MinX, viewBounds.MinY},
		[2]float32{viewBounds.MaxX, viewBounds.MaxY},
	)
	return Culled{mesh: mesh, ok: true}
}

// DrawSpritesCulled draws the survivors of a cull encoded earlier this frame.
// It opens the render pass, so everything the cull needed is already recorded.
func (g *GPU) DrawSpritesCulled(c Culled) {
	if !c.ok {
		return
	}
	g.r.DrawInstancedIndirect(c.mesh, g.cull)
}

// ensureCull creates or grows the cull pipeline. A pipeline replaced by a
// larger one is retired rather than released, because a frame already submitted
// may still be reading its buffers.
func (g *GPU) ensureCull(count int) {
	if g.cull != nil && g.cullMax >= count {
		return
	}
	if g.cull != nil {
		g.retired = append(g.retired, retiredCull{cull: g.cull, frame: g.frame})
	}
	maxN := max(count, 1024)
	g.cull = NewCullPipeline(g.r.Device(), g.r.Queue(), maxN)
	g.cull.BeginFrame()
	g.cullMax = maxN
}

// unitQuad returns a lazily-created shared unit quad mesh.
func (g *GPU) unitQuad() *Mesh {
	if g.cullMesh == nil {
		g.cullMesh = NewUnitQuad(g.r.Device(), g.r.Queue())
	}
	return g.cullMesh
}

// --- Strokes ---

// DrawStrokes submits GPU-expanded stroke segments.
// Flushes pending dirty ranges automatically.
func (g *GPU) DrawStrokes(segments *StrokeBuffer) {
	g.r.DrawStrokes(segments)
}

// --- Accessors ---

// Device returns the wgpu device.
func (g *GPU) Device() *wgpu.Device { return g.r.Device() }

// Queue returns the wgpu queue.
func (g *GPU) Queue() *wgpu.Queue { return g.r.Queue() }

// SurfaceFormat returns the surface texture format.
func (g *GPU) SurfaceFormat() gputypes.TextureFormat { return g.r.SurfaceFormat() }

// Stats returns per-frame rendering metrics.
func (g *GPU) Stats() FrameStats { return g.r.Stats() }

// Renderer returns the underlying Renderer for cases that need direct access
// (e.g. maprender which builds its own pipelines).
func (g *GPU) Renderer() *Renderer { return g.r }

// Release releases all GPU resources.
func (g *GPU) Release() {
	for _, r := range g.retired {
		r.cull.Release()
	}
	g.retired = nil
	if g.cull != nil {
		g.cull.Release()
		g.cull = nil
	}
	if g.cullMesh != nil {
		g.cullMesh.Release()
		g.cullMesh = nil
	}
	g.r.Release()
}
