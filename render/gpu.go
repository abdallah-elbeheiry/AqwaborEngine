// Package render provides the GPU submission layer for the Aqwabor engine.
//
// The high-level entry point is GPU, which wraps all pipeline and buffer
// management behind a small set of methods:
//
//	gfx, _ := render.New(dp)
//	gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
//	gfx.SetCamera(viewProj, vpW, vpH)
//	gfx.DrawInstanced(mesh, instances)
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

// GPU is the public facade over the render submission layer.
// It owns the per-frame render pass, all pipelines, and resource management.
type GPU struct {
	r *Renderer
}

// New creates a GPU facade from a device provider.
func New(dp DeviceProvider) *GPU {
	return &GPU{r: NewRenderer(dp)}
}

// --- Frame lifecycle ---

// Begin starts a render pass that clears the screen.
func (g *GPU) Begin(dc *gogpu.Context, c Clear) error {
	return g.r.ClearAndBeginFrame(dc, c.R, c.G, c.B, c.A)
}

// End closes the current render pass.
func (g *GPU) End() {
	g.r.EndFrame()
}

// --- Camera ---

// SetCamera updates the camera view-projection for instanced and stroke draws.
func (g *GPU) SetCamera(viewProj [16]float32, viewportW, viewportH float32) {
	g.r.UpdateCamera(viewProj, viewportW, viewportH)
	g.r.UpdateStrokeCamera(viewProj, viewportW, viewportH)
}

// --- Instanced draws ---

// DrawInstanced submits an instanced draw command.
// Flushes pending dirty ranges automatically.
func (g *GPU) DrawInstanced(mesh *Mesh, instances *InstanceBuffer) {
	g.r.DrawInstanced(mesh, instances)
}

// DrawInstancedIndirect submits an indirect instanced draw over culled data.
func (g *GPU) DrawInstancedIndirect(mesh *Mesh, cull *CullPipeline) {
	g.r.DrawInstancedIndirect(mesh, cull)
}

// --- Strokes ---

// DrawStrokes submits GPU-expanded stroke segments.
// Flushes pending dirty ranges automatically.
func (g *GPU) DrawStrokes(segments *StrokeBuffer) {
	g.r.DrawStrokes(segments)
}

// --- Static mesh ---

// DrawMapMesh draws pre-built map geometry with a separate map pipeline.
func (g *GPU) DrawMapMesh(mesh *MapMesh, pipe *MapPipeline) {
	g.r.DrawMapMesh(mesh, pipe)
}

// --- Variable-topology vertex draws ---

// UploadVertices uploads vertex data for variable-topology content.
func (g *GPU) UploadVertices(vertices []Vertex) {
	g.r.UploadVertices(vertices)
}

// DrawVertices draws previously uploaded vertex data.
func (g *GPU) DrawVertices() {
	g.r.DrawVertices()
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
func (g *GPU) Release() { g.r.Release() }
