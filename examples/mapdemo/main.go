// Command mapdemo loads a vector world map and renders it with camera pan/zoom.
//
// Usage:
//
//	go run ./examples/mapdemo
package main

import (
	"flag"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/abdallah-elbeheiry/AqwaborEngine/examples/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/examples/maprender"
	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
	gogpuinput "github.com/abdallah-elbeheiry/AqwaborEngine/input/backend/gogpu"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithTimestamp(true), logx.WithLevel(logx.DebugLevel))

	mode := flag.String("mode", "world", "demo mode: world (vector map ECS)")
	flag.Parse()

	switch *mode {
	default:
		runWorldDemo()
	}
}

func runWorldDemo() {
	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor — World Map",
		W:         1280,
		H:         720,
		Resizable: true,
	})
	if err != nil {
		logx.Fatalf("window: %v", err)
	}
	defer win.Close()

	worldPath := "examples/world_v3.json"
	world, err := mapdata.LoadJSON(worldPath)
	if err != nil {
		logx.Fatalf("load world: %v", err)
	}
	logx.Info("world loaded", "geoms", world.GeomCount, "verts", len(world.Coords)/2, "layers", len(world.Layers))

	// --- ECS setup ---
	w := ecs.NewWorld()
	camComp := camera.MustRegisterECS(w)
	_ = render.MustRegisterECS(w)

	// The viewport is read from the frame rather than remembered from the size
	// the window was asked for: moving to a display with a different backing
	// scale resizes the surface, and a projection built for the old size draws
	// the map into a corner of the new one.
	vpW, vpH := float32(1280), float32(720)
	camE := w.Create()
	camComp.Set(camE, camera.Camera{
		MinZoom: 0.01,
		MaxZoom: 1000,
		Active:  1,
	})
	camComp.Wake(camE)
	c, _ := camComp.Get(camE)
	c.Fit(360, 180, vpW, vpH)
	c.X = 0
	c.Y = 0

	// --- Input ---
	app := win.App()
	mgr := input.NewManager(gogpuinput.NewBackend(app))

	var scrollMu sync.Mutex
	var scrollDy float32
	app.EventSource().OnScroll(func(_, dy float64) {
		scrollMu.Lock()
		scrollDy -= float32(dy)
		scrollMu.Unlock()
	})

	panAction := mgr.Action("pan")
	mgr.BindMouseButton(panAction, input.MouseButtonLeft)
	panAction.OnDrag(func(dx, dy float64, _ input.Context) {
		c, ok := camComp.Get(camE)
		if !ok {
			return
		}
		// Pan is written for a world whose Y runs down the screen. This one runs
		// up, so the drag is handed the opposite sign and the map follows the
		// cursor rather than fighting it.
		c.Pan(float32(dx), -float32(dy))
	})

	// E and Q as well as = and -, because = is not a key of its own on a German
	// ISO layout and - is not where the ANSI code expects it.
	zoom := func(name string, key input.Key, factor float32) {
		a := mgr.Action(name)
		mgr.BindKey(a, key)
		a.OnPressed(func(_ input.Context) {
			c, ok := camComp.Get(camE)
			if !ok {
				return
			}
			c.Zoom *= factor
			camera.ClampZoom(c)
		})
	}
	zoom("zoom_in", input.KeyE, 1.1)
	zoom("zoom_out", input.KeyQ, 1/1.1)
	zoom("zoom_in_ansi", input.KeyEqual, 1.1)
	zoom("zoom_out_ansi", input.KeyMinus, 1/1.1)

	resetAction := mgr.Action("reset")
	mgr.BindKey(resetAction, input.KeyR)
	resetAction.OnPressed(func(_ input.Context) {
		c, ok := camComp.Get(camE)
		if !ok {
			return
		}
		c.Fit(360, 180, vpW, vpH)
		c.X = 0
		c.Y = 0
	})

	var lastFrame time.Time
	lastFrame = time.Now()

	logx.Info("mapdemo running: drag to pan, scroll to zoom, R=reset, E/Q or =/- zoom")

	var gfx *render.GPU
	var mapMesh *maprender.MapMesh
	var mapPipe *maprender.MapPipeline
	var strokeBuf *render.StrokeBuffer
	var worldScale float32

	win.OnResize(func(w, h int) { logx.Info("resized", "w", w, "h", h) })

	if err := win.Run(func(dc *gogpu.Context) {
		if w, h := dc.Size(); w > 0 && h > 0 {
			vpW, vpH = float32(w), float32(h)
		}
		win.WatchScale(dc.ScaleFactor())

		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		mgr.Update(dt)

		if gfx == nil {
			gfx = render.New(win.DeviceProvider())

			worldScale = float32(world.Scale)

			// Build fill geometries.
			fillGeoms := make([]maprender.FillGeometry, world.GeomCount)
			for gid := int32(0); gid < world.GeomCount; gid++ {
				s, n := world.GeomStart[gid], world.GeomN[gid]
				if n < 2 {
					continue
				}
				fillGeoms[gid] = maprender.FillGeometry{
					Coords: world.Coords[s : s+n*2],
					Rank:   int(world.GeomRank[gid]),
				}
			}

			// Build stroke geometries.
			var strokeGeoms []maprender.StrokeGeometry
			for _, pass := range world.DrawOrder {
				if pass.StrokeColor == nil {
					continue
				}
				layer := &world.Layers[pass.LayerIndex]
				closed := layer.Kind == mapdata.KindRing
				c := pass.StrokeColor
				color := [4]float32{
					float32(render.Clamp255(c.R)) / 255,
					float32(render.Clamp255(c.G)) / 255,
					float32(render.Clamp255(c.B)) / 255,
					float32(render.Clamp255(c.A)) / 255,
				}
				for _, gid := range layer.GeomIDs {
					n := world.GeomN[gid]
					if n < 2 {
						continue
					}
					s := world.GeomStart[gid]
					strokeGeoms = append(strokeGeoms, maprender.StrokeGeometry{
						Coords: world.Coords[s : s+n*2],
						Closed: closed,
						Rank:   int(world.GeomRank[gid]),
						Color:  color,
					})
				}
			}

			// Build draw pass specs.
			passes := make([]maprender.DrawPassSpec, 0, len(world.DrawOrder))
			for _, pass := range world.DrawOrder {
				layer := &world.Layers[pass.LayerIndex]
				dp := maprender.DrawPassSpec{
					GeomIndices: layer.GeomIDs,
					RankFilter:  pass.RankFilter,
				}
				if pass.FillColor != nil {
					c := pass.FillColor
					dp.FillColor = &render.Color{R: c.R, G: c.G, B: c.B, A: c.A}
				}
				if pass.StrokeColor != nil {
					c := pass.StrokeColor
					dp.StrokeColor = &render.Color{R: c.R, G: c.G, B: c.B, A: c.A}
				}
				passes = append(passes, dp)
			}

			// Build GPU resources.
			mapPipe = maprender.NewMapPipeline(gfx.Device(), gfx.SurfaceFormat(), gfx.CameraLayout())
			mapMesh = maprender.BuildMapMesh(gfx.Device(), fillGeoms, passes, maprender.MapMeshConfig{})
			strokeBuf, _ = maprender.BuildMapStrokes(gfx.Device(), strokeGeoms, 1.5, 0.75)
		}

		// Drain accumulated scroll-wheel zoom toward cursor.
		scrollMu.Lock()
		sd := scrollDy
		scrollDy = 0
		scrollMu.Unlock()
		if sd != 0 {
			c, ok := camComp.Get(camE)
			if ok {
				factor := float32(1.1)
				if sd < 0 {
					factor = 1 / 1.1
				}
				mx, my := app.Input().Mouse().Position()
				c.ZoomAt(factor, float32(mx), float32(my), vpW, vpH)
			}
		}

		cam, _ := camComp.Get(camE)
		vpMat := viewProjMap(*cam, vpW, vpH, worldScale)

		if err := gfx.Begin(dc, render.Clear{
			R: world.Background.R, G: world.Background.G,
			B: world.Background.B, A: world.Background.A,
		}); err != nil {
			return
		}
		// One SetCamera covers the map pipeline too: it was built against the
		// engine's camera layout, so it reads the same buffer the sprite and
		// stroke pipelines do.
		gfx.SetCamera(vpMat, vpW, vpH)

		// Draw fills via the generic vertex draw.
		maxRank := maprender.RankForZoom(cam.Zoom)
		if mapMesh != nil && mapMesh.Buffer != nil && mapMesh.Total > 0 {
			for _, pass := range mapMesh.Passes {
				count := pass.CountFor(maxRank)
				if count == 0 {
					continue
				}
				gfx.DrawVerticesRange(
					mapPipe.Pipeline(),
					mapMesh.Buffer,
					count,
					pass.Offset,
				)
			}
		}

		// Draw strokes.
		if strokeBuf != nil && strokeBuf.Count() > 0 {
			gfx.DrawStrokes(strokeBuf)
		}

		gfx.End()
	}); err != nil {
		logx.Fatalf("window run failed: %v", err)
	}

	world.Unload()
}

// viewProjMap builds a column-major 4x4 orthographic view-projection matrix
// from a Camera component, viewport size, and world scale.
//
// It exists instead of camera.ViewProj for two reasons. The map's coordinates
// are degrees times a scale, so the matrix folds that divisor in; and latitude
// runs up, unlike the engine's default of world Y running down the screen. Y up
// means the scale stays positive and the translation is negated, which is the
// mirror of what camera.ViewProj does. Either way the camera's own position has
// to land at clip 0.
func viewProjMap(c camera.Camera, viewW, viewH, worldScale float32) [16]float32 {
	if worldScale == 0 {
		worldScale = 1
	}
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	vpW := viewW
	vpH := viewH
	if vpW == 0 {
		vpW = 1
	}
	if vpH == 0 {
		vpH = 1
	}

	sx := zoom * 2 / vpW / worldScale
	sy := zoom * 2 / vpH / worldScale
	tx := -c.X * zoom * 2 / vpW
	ty := -c.Y * zoom * 2 / vpH

	return [16]float32{
		sx, 0, 0, 0,
		0, sy, 0, 0,
		0, 0, 1, 0,
		tx, ty, 0, 1,
	}
}
