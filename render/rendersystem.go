package render

import (
	"runtime"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/abdallah-elbeheiry/AqwaborEngine/examples/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/triangulate"
)

const (
	defaultStrokeWidthPx = 1.5
	defaultMinSegmentPx  = 0.75
)

// SceneConfig is the ECS component carrying per-scene render settings.
type SceneConfig struct {
	ClearR, ClearG, ClearB, ClearA float32
	WorldScale                     float32
	Active                         uint8
}

// RenderSystem bridges ECS entities to the GPU. It owns the pre-built GPU
// buffers and handles per-frame LOD selection and draw calls.
//
// The system is generic: it does not know whether the entities came from a
// map JSON, a procedural generator, or any other source.
type RenderSystem struct {
	sceneComp ecs.Comp[SceneConfig]
	ren       *Renderer

	fillGeoms   []FillGeometry
	strokeGeoms []StrokeGeometry
	passes      []DrawPassSpec

	mapMesh       *MapMesh
	mapPipe       *MapPipeline
	strokeBuf     *StrokeBuffer
	strokesByRank [MaxRank + 1]int

	strokeWidthPx float32
	minSegmentPx  float32

	stats    FrameStats
	lastLog  time.Time
	logEvery time.Duration
}

// NewRenderSystem creates a render system and registers the SceneConfig
// component with the ECS world.
func NewRenderSystem(w *ecs.World, ren *Renderer) (*RenderSystem, error) {
	comp, err := ecs.Register[SceneConfig](w)
	if err != nil {
		return nil, err
	}
	return &RenderSystem{
		sceneComp:     comp,
		ren:           ren,
		strokeWidthPx: defaultStrokeWidthPx,
		minSegmentPx:  defaultMinSegmentPx,
		logEvery:      2 * time.Second,
	}, nil
}

// MustRegisterRenderSystem is NewRenderSystem, panicking on error.
func MustRegisterRenderSystem(w *ecs.World, ren *Renderer) *RenderSystem {
	rs, err := NewRenderSystem(w, ren)
	if err != nil {
		panic("render.MustRegisterECS: " + err.Error())
	}
	return rs
}

// SceneComponent returns the handle for the SceneConfig component.
func (rs *RenderSystem) SceneComponent() ecs.Comp[SceneConfig] { return rs.sceneComp }

// SetRenderer sets the underlying GPU renderer. Call after GPU init.
func (rs *RenderSystem) SetRenderer(ren *Renderer) {
	rs.ren = ren
	if ren == nil {
		return
	}
	rs.mapPipe = NewMapPipeline(ren.Device(), ren.SurfaceFormat())
	ren.SetFillPipeline(rs.mapPipe)
	rs.buildGPUMesh()
}

// SetStrokeWidth sets the stroke width in pixels for all polylines.
func (rs *RenderSystem) SetStrokeWidth(px float32) { rs.strokeWidthPx = px }

// Stats returns the last frame's rendering metrics.
func (rs *RenderSystem) Stats() FrameStats { return rs.stats }

// --- Loading from mapdata ---

// LoadMapScene converts a mapdata.World into ECS entities and pre-built
// geometry. It returns the scene entity and the geometry slices ready for
// GPU upload.
func (rs *RenderSystem) LoadMapScene(w *ecs.World, world *mapdata.World) ecs.Entity {
	scale := float64(world.Scale)

	// Create scene entity.
	sceneE := w.Create()
	rs.sceneComp.Set(sceneE, SceneConfig{
		ClearR:     world.Background.R,
		ClearG:     world.Background.G,
		ClearB:     world.Background.B,
		ClearA:     world.Background.A,
		WorldScale: float32(world.Scale),
		Active:     1,
	})
	rs.sceneComp.Wake(sceneE)

	// Triangulate ring polygons in parallel.
	rs.fillGeoms = make([]FillGeometry, world.GeomCount)
	type ringJob struct {
		geomIdx int32
		start   int32
		n       int32
	}
	var ringJobs []ringJob
	for li := range world.Layers {
		if world.Layers[li].Kind != mapdata.KindRing {
			continue
		}
		for _, gid := range world.Layers[li].GeomIDs {
			ringJobs = append(ringJobs, ringJob{
				geomIdx: gid,
				start:   world.GeomStart[gid],
				n:       world.GeomN[gid],
			})
		}
	}

	triResults := make([][]int32, world.GeomCount)
	start := time.Now()
	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var nextJob int
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := nextJob
				nextJob++
				mu.Unlock()
				if i >= len(ringJobs) {
					return
				}
				job := ringJobs[i]
				coords := world.Coords[job.start : job.start+job.n*2]
				triResults[job.geomIdx] = triangulate.Ring(coords, scale)
			}
		}()
	}
	wg.Wait()

	tris := 0
	for _, t := range triResults {
		tris += len(t) / 3
	}
	logx.Info("fills triangulated",
		"rings", len(ringJobs), "triangles", tris,
		"took", time.Since(start).Round(time.Millisecond), "workers", workers)

	// Build fill geometry structs.
	for gid := int32(0); gid < world.GeomCount; gid++ {
		s, n := world.GeomStart[gid], world.GeomN[gid]
		if n < 2 || triResults[gid] == nil {
			continue
		}
		rs.fillGeoms[gid] = FillGeometry{
			Coords: world.Coords[s : s+n*2],
			Tris:   triResults[gid],
			Rank:   int(world.GeomRank[gid]),
		}
	}

	// Build stroke geometries.
	rs.strokeGeoms = nil
	for _, pass := range world.DrawOrder {
		if pass.StrokeColor == nil {
			continue
		}
		layer := &world.Layers[pass.LayerIndex]
		closed := layer.Kind == mapdata.KindRing
		c := pass.StrokeColor
		color := [4]float32{
			float32(Clamp255(c.R)) / 255,
			float32(Clamp255(c.G)) / 255,
			float32(Clamp255(c.B)) / 255,
			float32(Clamp255(c.A)) / 255,
		}
		for _, gid := range layer.GeomIDs {
			n := world.GeomN[gid]
			if n < 2 {
				continue
			}
			s := world.GeomStart[gid]
			rs.strokeGeoms = append(rs.strokeGeoms, StrokeGeometry{
				Coords: world.Coords[s : s+n*2],
				Closed: closed,
				Rank:   int(world.GeomRank[gid]),
				Color:  color,
			})
		}
	}

	// Build draw pass specs.
	rs.passes = make([]DrawPassSpec, 0, len(world.DrawOrder))
	for _, pass := range world.DrawOrder {
		layer := &world.Layers[pass.LayerIndex]
		dp := DrawPassSpec{
			GeomIndices: layer.GeomIDs,
			RankFilter:  pass.RankFilter,
		}
		if pass.FillColor != nil {
			c := pass.FillColor
			dp.FillColor = &Color{R: c.R, G: c.G, B: c.B, A: c.A}
		}
		if pass.StrokeColor != nil {
			c := pass.StrokeColor
			dp.StrokeColor = &Color{R: c.R, G: c.G, B: c.B, A: c.A}
		}
		rs.passes = append(rs.passes, dp)
	}

	// Build GPU resources if the renderer is already available.
	if rs.ren != nil {
		if rs.mapPipe == nil {
			rs.mapPipe = NewMapPipeline(rs.ren.Device(), rs.ren.SurfaceFormat())
			rs.ren.SetFillPipeline(rs.mapPipe)
		}
		rs.buildGPUMesh()
	}

	return sceneE
}

// --- GPU mesh building ---

func (rs *RenderSystem) buildGPUMesh() {
	if rs.ren == nil {
		return
	}
	start := time.Now()
	rs.mapMesh = BuildMapMesh(
		rs.ren.Device(),
		rs.fillGeoms,
		rs.passes,
		MapMeshConfig{
			StrokeWidthPx: rs.strokeWidthPx,
			MinSegmentPx:  rs.minSegmentPx,
			RefZoom:       1,
		},
	)

	rs.strokeBuf, rs.strokesByRank = BuildMapStrokes(
		rs.ren.Device(),
		rs.strokeGeoms,
		rs.strokeWidthPx,
		rs.minSegmentPx,
	)

	logx.Info("GPU mesh built",
		"fill_verts", rs.mapMesh.Total,
		"fill_passes", len(rs.mapMesh.Passes),
		"stroke_segs", rs.strokeBuf.Count(),
		"took", time.Since(start).Round(time.Millisecond))
}

// --- Per-frame draw ---

// Draw renders the scene attached to the given entity. The caller owns the
// render pass (Begin/End).
func (rs *RenderSystem) Draw(sceneE ecs.Entity, cam camera.Camera, vpW, vpH float32) {
	if rs.ren == nil || rs.mapMesh == nil || rs.mapPipe == nil {
		return
	}
	scene, ok := rs.sceneComp.Get(sceneE)
	if !ok || scene.Active == 0 {
		return
	}
	if vpW <= 0 || vpH <= 0 {
		return
	}

	zoom := cam.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	maxRank := RankForZoom(zoom)

	// Fills.
	rs.ren.DrawMapMeshAtRank(rs.mapMesh, rs.mapPipe, maxRank)

	// Strokes.
	if rs.strokeBuf != nil && rs.strokeBuf.Count() > 0 {
		rs.ren.DrawStrokesN(rs.strokeBuf, rs.strokesByRank[maxRank])
	}

	rs.stats = rs.ren.Stats()

	if now := time.Now(); now.Sub(rs.lastLog) >= rs.logEvery {
		rs.lastLog = now
		logx.Info("render frame",
			"tris", rs.stats.Triangles,
			"max_rank", maxRank)
	}
}
