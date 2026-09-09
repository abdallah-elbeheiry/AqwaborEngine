package maprender

import (
	"runtime"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/gogpu/ui/geometry"
)

const metresPerDegree = 110540

const (
	defaultStrokeWidthPx = 1.5
	defaultMinSegmentPx  = 0.75
)

type Renderer struct {
	world    *mapdata.World
	refZoom  float32
	viewport geometry.Size
	ren      *render.Renderer

	fillTris [][]int32

	mapMesh *render.MapMesh
	mapPipe *render.MapPipeline

	strokeBuf *render.StrokeBuffer // GPU-expanded strokes (new path)

	// strokesByRank is how many stroke segments to draw at each level of
	// detail, which the build works out by emitting them in rank order.
	strokesByRank [render.MaxRank + 1]int

	// zoom is the camera zoom the level of detail is chosen from. refZoom is
	// what the mesh was built against and no longer selects detail.
	zoom float32

	strokeWidthPx float32
	minSegmentPx  float32

	stats    Stats
	lastLog  time.Time
	logEvery time.Duration
}

type Stats struct {
	Triangles   int
	MetresPerPx float64
	// MaxRank is the level of detail the last frame drew at, which is the
	// number to look at when features appear or vanish unexpectedly.
	MaxRank int
}

func (r *Renderer) Stats() Stats { return r.stats }

func NewRenderer(world *mapdata.World, refZoom float32, ren *render.Renderer) *Renderer {
	r := &Renderer{
		world:         world,
		refZoom:       refZoom,
		ren:           ren,
		strokeWidthPx: defaultStrokeWidthPx,
		minSegmentPx:  defaultMinSegmentPx,
		logEvery:      2 * time.Second,
	}
	r.buildFills()
	return r
}

// SetRenderer sets the render.Renderer and builds the GPU mesh (called after GPU init).
func (r *Renderer) SetRenderer(ren *render.Renderer) {
	r.ren = ren
	if ren == nil {
		return
	}

	r.mapPipe = render.NewMapPipeline(ren.Device(), ren.SurfaceFormat())
	ren.SetMapPipeline(r.mapPipe)
	r.buildGPUMesh()
}

func (r *Renderer) buildGPUMesh() {
	if r.ren == nil || r.world == nil {
		return
	}
	start := time.Now()
	r.mapMesh = render.BuildMapMesh(
		r.ren.Device(), r.ren.Queue(),
		r.world, r.fillTris,
		render.MapMeshConfig{
			StrokeWidthPx: r.strokeWidthPx,
			MinSegmentPx:  r.minSegmentPx,
			RefZoom:       r.refZoom,
		},
		true, // fillsOnly: strokes go through BuildMapStrokes + DrawStrokes
	)

	// Build GPU-expanded stroke segments (replaces baked quad soup).
	r.strokeBuf, r.strokesByRank = render.BuildMapStrokes(
		r.ren.Device(),
		r.world,
		r.strokeWidthPx,
		r.minSegmentPx,
	)

	logx.Info("map GPU mesh built",
		"fill_verts", r.mapMesh.Total,
		"fill_passes", len(r.mapMesh.Passes),
		"stroke_segs", r.strokeBuf.Count(),
		"took", time.Since(start).Round(time.Millisecond))
}

func (r *Renderer) buildFills() {
	w := r.world
	r.fillTris = make([][]int32, w.GeomCount)

	ids := make([]int32, 0, w.GeomCount)
	for li := range w.Layers {
		if w.Layers[li].Kind != mapdata.KindRing {
			continue
		}
		ids = append(ids, w.Layers[li].GeomIDs...)
	}

	start := time.Now()
	scale := float64(w.Scale)
	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	var next struct {
		sync.Mutex
		i int
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				next.Lock()
				i := next.i
				next.i++
				next.Unlock()
				if i >= len(ids) {
					return
				}
				gid := ids[i]
				s, n := w.GeomStart[gid], w.GeomN[gid]
				r.fillTris[gid] = triangulateRing(w.Coords[s:s+n*2], scale)
			}
		}()
	}
	wg.Wait()

	tris := 0
	for _, t := range r.fillTris {
		tris += len(t) / 3
	}
	logx.Info("map fills triangulated",
		"rings", len(ids), "triangles", tris,
		"took", time.Since(start).Round(time.Millisecond), "workers", workers)
}

func (r *Renderer) SetViewport(vp geometry.Size) { r.viewport = vp }
func (r *Renderer) SetRefZoom(z float32)         { r.refZoom = z }

// SetZoom tells the renderer the camera's current zoom, which is what selects
// the level of detail. Without it the map draws every rank at every zoom, which
// is every minor river at a whole-world view.
func (r *Renderer) SetZoom(z float32)         { r.zoom = z }
func (r *Renderer) SetStrokeWidth(px float32) { r.strokeWidthPx = px }

func (r *Renderer) Draw() {
	if r.world == nil || r.ren == nil || r.mapMesh == nil || r.mapPipe == nil {
		return
	}
	if r.viewport.Width <= 0 || r.viewport.Height <= 0 {
		return
	}

	zoom := r.zoom
	if zoom <= 0 {
		zoom = r.refZoom
	}
	maxRank := render.RankForZoom(zoom)
	r.stats = Stats{MetresPerPx: metresPerDegree / float64(zoom), MaxRank: maxRank}

	// Fills, at the level of detail this zoom is worth. The geometry was
	// ordered by rank when the mesh was built, so this is a smaller vertex
	// count rather than any per-frame work.
	r.ren.DrawMapMeshAtRank(r.mapMesh, r.mapPipe, maxRank)

	// Strokes, the same way.
	if r.strokeBuf != nil && r.strokeBuf.Count() > 0 {
		r.ren.DrawStrokesN(r.strokeBuf, r.strokesByRank[maxRank])
	}

	r.stats.Triangles = r.ren.Stats().Triangles

	if now := time.Now(); now.Sub(r.lastLog) >= r.logEvery {
		r.lastLog = now
		logx.Info("map frame",
			"tris", r.stats.Triangles,
			"m/px", int(r.stats.MetresPerPx),
			"max_rank", r.stats.MaxRank)
	}
}
