package maprender

import (
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/geometry"
)

// metresPerDegree is one degree of latitude at the equator, and is what turns
// camera zoom into a ground resolution the rank filter can be written against.
const metresPerDegree = 110540

// Defaults chosen to match the reference viewer the data ships with.
const (
	defaultStrokeWidthPx = 1.5
	// Segments shorter than this on screen collapse into their predecessor.
	// At a whole-world view the median source segment is a twentieth of a
	// pixel, so without this the coastline costs 20 times the vertices it can
	// possibly show.
	defaultMinSegmentPx = 0.75
	// A geometry smaller than this in both axes cannot show a shape.
	minFeaturePx = 0.6
)

type Renderer struct {
	world    *mapdata.World
	cam      *camera.Camera
	viewport geometry.Size
	vertices []window.Vertex
	win      *window.Window

	// fillTris holds triangle corner indices local to each geometry, three per
	// triangle. Built once, because ear clipping the whole world costs seconds
	// and the result never changes.
	fillTris [][]int32
	// scratch holds one geometry's projected vertices, so a vertex shared by
	// several triangles is projected once.
	scratch []geometry.Point

	strokeWidthPx float32
	minSegmentPx  float32

	stats    Stats
	lastLog  time.Time
	logEvery time.Duration
}

// Stats describes the most recent frame, for tuning and for seeing at a glance
// whether a slow frame is geometry or fill rate.
type Stats struct {
	GeomsVisible int
	Triangles    int
	MaxRank      float32
	MetresPerPx  float64
}

// Stats returns the counters from the last completed frame.
func (r *Renderer) Stats() Stats { return r.stats }

func NewRenderer(world *mapdata.World, cam *camera.Camera, win *window.Window) *Renderer {
	r := &Renderer{
		world:         world,
		cam:           cam,
		win:           win,
		vertices:      make([]window.Vertex, 0, 1<<16),
		strokeWidthPx: defaultStrokeWidthPx,
		minSegmentPx:  defaultMinSegmentPx,
		logEvery:      2 * time.Second,
	}
	r.buildFills()
	return r
}

// buildFills triangulates every ring once, in parallel across rings. Rings are
// independent, so this is the whole of the concurrency: no shared state, one
// slot written per geometry.
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
	maxN := int32(0)
	for wk := 0; wk < workers; wk++ {
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
	for gid := int32(0); gid < w.GeomCount; gid++ {
		if w.GeomN[gid] > maxN {
			maxN = w.GeomN[gid]
		}
	}
	r.scratch = make([]geometry.Point, maxN)
	logx.Info("map fills triangulated",
		"rings", len(ids), "triangles", tris,
		"took", time.Since(start).Round(time.Millisecond), "workers", workers)
}

func (r *Renderer) SetViewport(vp geometry.Size) { r.viewport = vp }
func (r *Renderer) SetCamera(cam *camera.Camera) { r.cam = cam }

// SetStrokeWidth sets the on-screen width of every stroked line, in pixels.
func (r *Renderer) SetStrokeWidth(px float32) { r.strokeWidthPx = px }

func (r *Renderer) Draw(dc *gogpu.Context) error {
	if r.world == nil || r.cam == nil {
		return nil
	}
	if r.viewport.Width <= 0 || r.viewport.Height <= 0 {
		return nil
	}

	r.vertices = r.vertices[:0]

	minLon, minLat, maxLon, maxLat := r.worldViewAABB()
	maxRank := r.maxRank()
	zoom := r.cam.Zoom()
	r.stats = Stats{MaxRank: maxRank, MetresPerPx: metresPerDegree / float64(zoom)}

	for _, pass := range r.world.DrawOrder {
		layer := &r.world.Layers[pass.LayerIndex]

		for _, geomID := range layer.GeomIDs {
			if !r.cullGeom(geomID, minLon, minLat, maxLon, maxLat) {
				continue
			}
			if pass.RankFilter && float32(r.world.GeomRank[geomID]) > maxRank {
				continue
			}
			if r.subPixel(geomID, zoom) {
				continue
			}

			r.stats.GeomsVisible++
			r.project(geomID)

			if pass.FillColor != nil && layer.Kind == mapdata.KindRing {
				r.emitFill(geomID, *pass.FillColor)
			}
			if pass.StrokeColor != nil {
				r.emitStroke(geomID, layer.Kind == mapdata.KindRing, *pass.StrokeColor)
			}
		}

		// One flush per pass keeps the painter's order the data asks for.
		// Every primitive in the buffer is self-contained, so concatenating
		// geometries cannot join them any more.
		if len(r.vertices) > 0 {
			r.stats.Triangles += len(r.vertices) / 3
			if err := r.win.Draw(dc, r.vertices); err != nil {
				return err
			}
			r.vertices = r.vertices[:0]
		}
	}

	if now := time.Now(); now.Sub(r.lastLog) >= r.logEvery {
		r.lastLog = now
		logx.Info("map frame",
			"geoms", r.stats.GeomsVisible, "tris", r.stats.Triangles,
			"m/px", int(r.stats.MetresPerPx), "maxRank", r.stats.MaxRank)
	}

	return nil
}

// maxRank mirrors the zoom-dependent rank filter the data documents: the more
// ground a pixel covers, the fewer minor rivers and lakes are worth drawing.
func (r *Renderer) maxRank() float32 {
	zoom := float64(r.cam.Zoom())
	if zoom <= 0 {
		return 12
	}
	mpx := metresPerDegree / zoom
	v := 16 - 3*math.Log10(math.Max(mpx, 1e-6))
	return float32(math.Max(0, math.Min(12, v)))
}

func (r *Renderer) worldViewAABB() (minLon, minLat, maxLon, maxLat float32) {
	vp := r.viewport
	tl := r.cam.LocalToWorld(geometry.Pt(0, 0), vp)
	br := r.cam.LocalToWorld(geometry.Pt(vp.Width, vp.Height), vp)

	minLon = min(tl.X, br.X)
	maxLon = max(tl.X, br.X)
	// Latitude is negated on the way to the camera, so the view's Y bounds
	// come back inverted.
	minLat = -max(tl.Y, br.Y)
	maxLat = -min(tl.Y, br.Y)

	return
}

func (r *Renderer) cullGeom(geomID int32, minLon, minLat, maxLon, maxLat float32) bool {
	scale := float32(r.world.Scale)
	gMinLon := float32(r.world.MinX[geomID]) / scale
	gMaxLon := float32(r.world.MaxX[geomID]) / scale
	gMinLat := float32(r.world.MinY[geomID]) / scale
	gMaxLat := float32(r.world.MaxY[geomID]) / scale

	return !(gMaxLon < minLon || gMinLon > maxLon || gMaxLat < minLat || gMinLat > maxLat)
}

// subPixel reports whether a geometry is too small on screen to show a shape.
func (r *Renderer) subPixel(geomID int32, zoom float32) bool {
	scale := float32(r.world.Scale)
	w := float32(r.world.MaxX[geomID]-r.world.MinX[geomID]) / scale * zoom
	h := float32(r.world.MaxY[geomID]-r.world.MinY[geomID]) / scale * zoom
	return w < minFeaturePx && h < minFeaturePx
}

// project fills the scratch buffer with the geometry's screen-space points.
func (r *Renderer) project(geomID int32) {
	start := r.world.GeomStart[geomID]
	n := r.world.GeomN[geomID]
	scale := float32(r.world.Scale)
	for i := int32(0); i < n; i++ {
		lon := float32(r.world.Coords[start+i*2]) / scale
		lat := float32(r.world.Coords[start+i*2+1]) / scale
		r.scratch[i] = r.cam.WorldToLocal(geometry.Pt(lon, -lat), r.viewport)
	}
}

func (r *Renderer) emitFill(geomID int32, color mapdata.Color) {
	for _, i := range r.fillTris[geomID] {
		r.vertices = append(r.vertices, r.vertexAt(r.scratch[i], color))
	}
}

// emitStroke turns a polyline into screen-space quads, two triangles per
// segment. The triangle-list pipeline has no line primitive, and building the
// line in screen space is what keeps it one width wide at every zoom.
func (r *Renderer) emitStroke(geomID int32, closed bool, color mapdata.Color) {
	n := int(r.world.GeomN[geomID])
	if n < 2 {
		return
	}
	pts := r.scratch[:n]
	if closed && pts[0] == pts[n-1] {
		// The ring already repeats its first vertex, so it needs no closing
		// segment. Rings that do not repeat it get one below.
		closed = false
	}

	hw := r.strokeWidthPx / 2
	minSeg := r.minSegmentPx * r.minSegmentPx

	last := pts[0]
	emitted := false
	for i := 1; i <= n; i++ {
		var p geometry.Point
		if i == n {
			if !closed {
				break
			}
			p = pts[0]
		} else {
			p = pts[i]
		}
		dx, dy := p.X-last.X, p.Y-last.Y
		if dx*dx+dy*dy < minSeg {
			// Too short to show. Skipping it rather than drawing it keeps the
			// line continuous, because the next segment starts from the last
			// point actually drawn.
			continue
		}
		r.emitSegment(last, p, hw, color)
		last = p
		emitted = true
	}
	// A geometry whose every segment collapsed still deserves a mark, or small
	// islands vanish in the gap between the sub-pixel cull and this one. The
	// far point is used rather than the last, which on a closed ring repeats
	// the first and would give a zero-length segment.
	if !emitted {
		far := pts[0]
		best := float32(0)
		for _, p := range pts[1:] {
			dx, dy := p.X-pts[0].X, p.Y-pts[0].Y
			if d := dx*dx + dy*dy; d > best {
				best, far = d, p
			}
		}
		r.emitSegment(pts[0], far, hw, color)
	}
}

func (r *Renderer) emitSegment(a, b geometry.Point, hw float32, color mapdata.Color) {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	nx, ny := -dy/l*hw, dx/l*hw

	a0 := geometry.Pt(a.X+nx, a.Y+ny)
	a1 := geometry.Pt(a.X-nx, a.Y-ny)
	b0 := geometry.Pt(b.X+nx, b.Y+ny)
	b1 := geometry.Pt(b.X-nx, b.Y-ny)

	r.vertices = append(r.vertices,
		r.vertexAt(a0, color), r.vertexAt(b0, color), r.vertexAt(b1, color),
		r.vertexAt(a0, color), r.vertexAt(b1, color), r.vertexAt(a1, color),
	)
}

// vertexAt converts a screen-space point to the clip space the pipeline wants.
func (r *Renderer) vertexAt(p geometry.Point, c mapdata.Color) window.Vertex {
	return window.Vertex{
		X: p.X/r.viewport.Width*2 - 1,
		Y: 1 - p.Y/r.viewport.Height*2,
		R: c.R, G: c.G, B: c.B, A: c.A,
	}
}
