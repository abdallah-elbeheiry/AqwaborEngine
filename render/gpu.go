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
	"image"
	"math"

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

// ScreenRect turns a world rectangle into the pixels it covers, which is what a
// compositor is told about when only part of a frame changed.
//
// The result is in physical pixels with Y running down, the way a surface is
// addressed, and it is grown outward to whole pixels: a rectangle that covers
// half a pixel still dirties it.
func ScreenRect(r ViewBounds, camX, camY, zoom, vpW, vpH, scale float32) image.Rectangle {
	if zoom <= 0 {
		zoom = 1
	}
	if scale <= 0 {
		scale = 1
	}
	// World Y runs down and so does a surface, so both edges map directly.
	x0 := (r.MinX-camX)*zoom + vpW/2
	x1 := (r.MaxX-camX)*zoom + vpW/2
	y0 := (r.MinY-camY)*zoom + vpH/2
	y1 := (r.MaxY-camY)*zoom + vpH/2

	return image.Rect(
		int(math.Floor(float64(x0*scale))),
		int(math.Floor(float64(y0*scale))),
		int(math.Ceil(float64(x1*scale))),
		int(math.Ceil(float64(y1*scale))),
	)
}

// MergeRects reduces damage to at most max rectangles by tiling the area they
// cover and unioning what falls in each tile.
//
// A compositor wants a handful of rectangles, not one per moving thing, and
// unioning the nearest is cheaper than reporting the whole surface: the pixels
// in a tile's gaps are recomposited, the ones outside it are not. Tiling rather
// than pairwise merging keeps it linear, which matters when the count is
// thousands.
func MergeRects(rects []image.Rectangle, max int) []image.Rectangle {
	if max < 1 {
		max = 1
	}
	if len(rects) <= max {
		return rects
	}

	bounds := rects[0]
	for _, r := range rects[1:] {
		bounds = bounds.Union(r)
	}

	// A square-ish grid of at most max tiles.
	side := 1
	for (side+1)*(side+1) <= max {
		side++
	}
	tileW := max2(bounds.Dx()/side, 1)
	tileH := max2(bounds.Dy()/side, 1)

	tiles := make([]image.Rectangle, side*side)
	for _, r := range rects {
		cx := (r.Min.X + r.Dx()/2 - bounds.Min.X) / tileW
		cy := (r.Min.Y + r.Dy()/2 - bounds.Min.Y) / tileH
		cx = clamp(cx, 0, side-1)
		cy = clamp(cy, 0, side-1)
		i := cy*side + cx
		if tiles[i].Empty() {
			tiles[i] = r
			continue
		}
		tiles[i] = tiles[i].Union(r)
	}

	out := make([]image.Rectangle, 0, side*side)
	for _, t := range tiles {
		if !t.Empty() {
			out = append(out, t)
		}
	}
	return out
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ViewOf is the world rectangle a camera covers, which is what Scene.Draw takes
// and what the cull tests against. It is here because otherwise every game
// writes the same four lines of arithmetic.
//
//	scene.Draw(render.ViewOf(cam.X, cam.Y, cam.Zoom, vpW, vpH))
func ViewOf(camX, camY, zoom, vpW, vpH float32) ViewBounds {
	if zoom <= 0 {
		zoom = 1
	}
	halfW := vpW / (2 * zoom)
	halfH := vpH / (2 * zoom)
	return ViewBounds{
		MinX: camX - halfW, MaxX: camX + halfW,
		MinY: camY - halfH, MaxY: camY + halfH,
	}
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

// SetCamera writes the camera every pipeline draws through, including any built
// outside the engine against CameraLayout. It is one buffer and one write: a
// pipeline added later needs no line here, which is what the four separate
// camera buffers this replaced each cost.
func (g *GPU) SetCamera(viewProj [16]float32, viewportW, viewportH float32) {
	g.r.UpdateCamera(viewProj, viewportW, viewportH)
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

// DrawSpritesRange draws one range of a batch. A layer's buffer is divided into
// chunks of world space, so a view is a few ranges of it rather than all of it.
func (g *GPU) DrawSpritesRange(batch *SpriteBatch, first, count int) {
	g.r.DrawInstancedRange(batch.meshInternal(), batch.bufferInternal(), first, count)
}

// Culled names a cull dispatch that has been encoded and is waiting to be
// drawn. The zero value draws nothing.
//
// It holds the pipeline it was encoded against, not only the slot: a later cull
// in the same frame can need a larger pipeline, and the draw has to read the
// buffers its own dispatch wrote rather than the ones the replacement holds.
type Culled struct {
	mesh *Mesh
	cull *CullPipeline
	slot int
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
// Several culls a frame are allowed, up to the pipeline's slot count. Each
// takes its own region of the output buffer and its own draw command, so they
// do not overwrite one another.
func (g *GPU) CullSprites(batch *SpriteBatch, viewBounds ViewBounds) Culled {
	return g.CullSpritesRange(batch, 0, batch.bufferInternal().Count(), viewBounds)
}

// CullSpritesRange is CullSprites over one range of the batch, which is what a
// scene submits: only the chunks a view covers are worth testing per instance.
func (g *GPU) CullSpritesRange(batch *SpriteBatch, first, count int, viewBounds ViewBounds) Culled {
	instances := batch.bufferInternal()
	if first < 0 {
		first = 0
	}
	if first+count > instances.Capacity() {
		count = instances.Capacity() - first
	}
	if count <= 0 {
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

	// The cull is the first thing to read the instance data this frame, so it
	// is what has to upload it. The draws flush for themselves, but a culled
	// draw reads the cull's output rather than this buffer, so on a frame with
	// nothing but culled draws the writes never reached the GPU at all: the
	// cull tested an empty buffer, every instance had zero scale, and nothing
	// survived to be drawn.
	instances.Flush(g.r.Queue())

	g.ensureCull(count)
	if g.cull == nil {
		return Culled{}
	}
	slot, ok := g.cull.Claim(count)
	if !ok {
		// Out of slots, or out of room in the output buffer. Either way the
		// caller draws the range without culling rather than not at all.
		log.Debug("no cull this draw; the range is drawn as it stands",
			"slots", cullSlots, "instances", count)
		return Culled{}
	}

	mesh := batch.meshInternal()
	if mesh == nil {
		mesh = g.unitQuad()
	}

	g.cull.ResetIndirect(slot, mesh.IndexCount)
	g.cull.EncodeDispatch(
		enc,
		slot,
		instances.Buffer(),
		g.r.CameraBuffer(),
		first, count,
		[2]float32{viewBounds.MinX, viewBounds.MinY},
		[2]float32{viewBounds.MaxX, viewBounds.MaxY},
	)
	return Culled{mesh: mesh, cull: g.cull, slot: slot, ok: true}
}

// DrawSpritesCulled draws the survivors of a cull encoded earlier this frame.
// It opens the render pass, so everything the cull needed is already recorded.
func (g *GPU) DrawSpritesCulled(c Culled) {
	if !c.ok || c.cull == nil {
		return
	}
	g.r.DrawInstancedIndirect(c.mesh, c.cull, c.slot)
}

// ensureCull creates or grows the cull pipeline so this frame's culls fit in
// its output buffer. A pipeline replaced by a larger one is retired rather than
// released, because a frame already submitted may still be reading its buffers.
//
// The buffer holds the frame's total, not its largest cull times the slot
// count, so growing is driven by what the frame has already claimed.
func (g *GPU) ensureCull(count int) {
	if g.cull != nil && g.cull.Fits(count) {
		return
	}
	want := count
	if g.cull != nil {
		want += g.cull.used
	}
	if want > maxCullBytes/instanceDataSize {
		// It cannot be made to fit. Keep whatever pipeline there is; the claim
		// that follows fails and the range is drawn uncalled.
		return
	}

	if g.cull != nil {
		g.retired = append(g.retired, retiredCull{cull: g.cull, frame: g.frame})
	}
	maxN := max(want*2, 1024)
	g.cull = NewCullPipeline(g.r.Device(), g.r.Queue(), maxN)
	if g.cull == nil {
		g.cullMax = 0
		return
	}
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

// --- Cells: the compact palette-indexed path ---

// Subcells creates a buffer of compact 16-byte instances for the dense cell
// layer, where the colour is a palette index rather than an RGBA value.
func (g *GPU) Subcells(capacity int) *SubcellBuffer {
	return NewSubcellBuffer(g.r.Device(), capacity)
}

// SetRamp uploads the palette the compact instances index into. Upload it once;
// every cell reads it, which is what makes the index cheaper than the colour.
func (g *GPU) SetRamp(table *RampTable) {
	g.r.SubcellPipeline().SetRamp(g.r.Queue(), table)
}

// SetCellSize sets the world-space size every cell is drawn at. A grid shares
// one size, so it is a uniform rather than bytes on every instance.
func (g *GPU) SetCellSize(w, h float32) {
	g.r.SubcellPipeline().SetCellSize(g.r.Queue(), w, h)
}

// DrawSubcells draws the cell layer.
func (g *GPU) DrawSubcells(cells *SubcellBuffer) { g.r.DrawSubcells(cells) }

// DrawSubcellsRange draws one range of the cell layer, which is what a grid
// submits: the chunks a view covers rather than the whole world.
func (g *GPU) DrawSubcellsRange(cells *SubcellBuffer, first, count int) {
	g.r.DrawSubcellsRange(cells, first, count)
}

// --- Strokes ---

// DrawStrokes submits GPU-expanded stroke segments.
// Flushes pending dirty ranges automatically.
func (g *GPU) DrawStrokes(segments *StrokeBuffer) {
	g.r.DrawStrokes(segments)
}

// --- Custom pipelines ---

// DrawVertices submits a non-indexed draw with a pipeline created outside the
// Renderer. The camera is bound at group 0 from the engine's buffer, so such a
// pipeline is built against CameraLayout and owns no camera of its own.
func (g *GPU) DrawVertices(pipe *wgpu.RenderPipeline, vertexBuffer *wgpu.Buffer, vertexCount uint32) {
	g.r.DrawVertices(pipe, vertexBuffer, vertexCount)
}

// DrawVerticesRange submits a non-indexed draw for a sub-range of a vertex buffer.
func (g *GPU) DrawVerticesRange(pipe *wgpu.RenderPipeline, vertexBuffer *wgpu.Buffer, vertexCount, firstVertex uint32) {
	g.r.DrawVerticesRange(pipe, vertexBuffer, vertexCount, firstVertex)
}

// CameraLayout is the bind group layout of the engine's shared camera, for a
// pipeline built outside the engine. List it first in the pipeline layout and
// read the camera at group(0) binding(0); the engine binds and updates it.
func (g *GPU) CameraLayout() *wgpu.BindGroupLayout { return g.r.CameraLayout() }

// --- Accessors ---

// Device returns the wgpu device.
func (g *GPU) Device() *wgpu.Device { return g.r.Device() }

// Queue returns the wgpu queue.
func (g *GPU) Queue() *wgpu.Queue { return g.r.Queue() }

// SurfaceFormat returns the surface texture format.
func (g *GPU) SurfaceFormat() gputypes.TextureFormat { return g.r.SurfaceFormat() }

// Stats returns per-frame rendering metrics.
func (g *GPU) Stats() FrameStats { return g.r.Stats() }

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
