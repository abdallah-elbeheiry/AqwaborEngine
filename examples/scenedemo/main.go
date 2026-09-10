// Command scenedemo draws a world through the ECS and nothing else.
//
// It spawns a field of sprites, moves a few of them, and lets the camera pan
// and zoom. Nothing here names a buffer, a pipeline or a bind group: the game
// side of an Aqwabor renderer is components and a scene.
//
// Usage:
//
//	go run ./examples/scenedemo
//	go run ./examples/scenedemo -frames 600   # exit after 600 frames
package main

import (
	"flag"
	"fmt"
	"math"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
	gogpuinput "github.com/abdallah-elbeheiry/AqwaborEngine/input/backend/gogpu"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
)

const spacing = 4 // world units between sprites

func main() {
	logx.Init(logx.WithColor(true), logx.WithLevel(logx.InfoLevel))
	defer logx.Info("keys: E/Q or =/- zoom, WASD or arrows pan")
	frames := flag.Int("frames", 0, "exit after this many frames; 0 runs until the window closes")
	zoom := flag.Float64("zoom", 2, "camera zoom to start at; a high one shows the chunk cull cutting work")
	mode := flag.String("mode", "scene", "scene (chunks + GPU cull), range (chunks, no cull), whole (one draw, no cull)")
	side := flag.Int("side", 100, "sprites a side; the field is this squared")
	movers := flag.Int("movers", 64, "how many of them move every frame")
	chunk := flag.Float64("chunk", 64, "chunk side in world units")
	onDemand := flag.Bool("ondemand", false, "draw only when something changes; -movers 0 with this is an idle world")
	spread := flag.Bool("spread", true, "spread the movers over the field; false puts them together")
	flag.Parse()

	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor - Scene",
		W:         1280,
		H:         720,
		Resizable: true,
		OnDemand:  *onDemand,
	})
	if err != nil {
		logx.Fatalf("window: %v", err)
	}
	defer win.Close()

	w := ecs.NewWorld()
	comps := render.MustRegisterECS(w)
	camComp := camera.MustRegisterECS(w)

	// The viewport is read from the frame, not remembered from the window that
	// was asked for. Moving a window to a display with a different backing
	// scale resizes the surface, and a projection built for the old size draws
	// the scene into a corner of the new one.
	vpW, vpH := float32(1280), float32(720)
	camE := w.Create()
	camComp.Set(camE, camera.Camera{Zoom: float32(*zoom), MinZoom: 0.05, MaxZoom: 50, Active: 1})
	camComp.Wake(camE)
	cam, _ := camComp.Get(camE)
	cam.X, cam.Y = float32(*side*spacing)/2, float32(*side*spacing)/2

	mgr := input.NewManager(gogpuinput.NewBackend(win.App()))
	bind := func(name string, key input.Key, fn func(*camera.Camera)) {
		a := mgr.Action(name)
		mgr.BindKey(a, key)
		a.OnPressed(func(input.Context) {
			if c, ok := camComp.Get(camE); ok {
				fn(c)
				win.RequestRedraw()
			}
		})
	}
	zoomIn := func(c *camera.Camera) { c.Zoom *= 1.1; camera.ClampZoom(c) }
	zoomOut := func(c *camera.Camera) { c.Zoom /= 1.1; camera.ClampZoom(c) }

	// Q and E as well as = and -, because = is not a key of its own on a German
	// ISO layout and - is not where the ANSI code expects it. Letters and arrows
	// are in the same place on every layout worth supporting.
	bind("zoom_in", input.KeyE, zoomIn)
	bind("zoom_out", input.KeyQ, zoomOut)
	bind("zoom_in_ansi", input.KeyEqual, zoomIn)
	bind("zoom_out_ansi", input.KeyMinus, zoomOut)

	panBy := func(dx, dy float32) func(*camera.Camera) {
		return func(c *camera.Camera) { c.X += dx; c.Y += dy }
	}
	bind("left", input.KeyA, panBy(-16, 0))
	bind("right", input.KeyD, panBy(16, 0))
	bind("up", input.KeyW, panBy(0, -16))
	bind("down", input.KeyS, panBy(0, 16))
	bind("left_arrow", input.KeyLeft, panBy(-16, 0))
	bind("right_arrow", input.KeyRight, panBy(16, 0))
	bind("up_arrow", input.KeyUp, panBy(0, -16))
	bind("down_arrow", input.KeyDown, panBy(0, 16))

	var scene *render.Scene
	var moving []ecs.Entity
	var frame int
	var lastReportFrame int
	var reported time.Time
	var syncMs, drawMs, presentMs float64
	last := time.Now()

	win.OnResize(func(w, h int) { logx.Info("resized", "w", w, "h", h) })

	err = win.Run(func(dc *gogpu.Context) {
		if w, h := dc.Size(); w > 0 && h > 0 {
			vpW, vpH = float32(w), float32(h)
		}
		win.WatchScale(dc.ScaleFactor())

		now := time.Now()
		mgr.Update(now.Sub(last).Seconds())
		last = now
		frame++

		if scene == nil {
			// The device only exists once the run loop has started, so the
			// scene is built here rather than above.
			gfx := render.New(win.DeviceProvider())
			cfg := render.SceneConfig{ChunkSize: float32(*chunk)}
			switch *mode {
			case "range":
				cfg.CullFrom = 1 << 30
			case "whole":
				cfg.CullFrom = 1 << 30
				cfg.MaxRuns = 1
			}
			logx.Info("drawing", "mode", *mode, "sprites", *side**side, "movers", *movers, "chunk", *chunk)
			scene = render.NewScene(gfx, w, comps, cfg)
			defer func() { reported = time.Now() }()

			// Spread the movers over the field rather than clustering them in
			// the first chunks, so the cost they cause is spread too.
			stride := max(1, *side**side/max(*movers, 1))
			if !*spread {
				stride = 1
			}

			for y := range *side {
				for x := range *side {
					shade := float32(x+y) / float32(2**side)
					e := scene.Spawn(
						render.Transform{
							X: float32(x * spacing), Y: float32(y * spacing),
							SX: 3, SY: 3,
						},
						render.Color{R: shade, G: 0.4, B: 1 - shade, A: 1},
						render.Sprite{},
					)
					if len(moving) < *movers && (y**side+x)%stride == 0 {
						moving = append(moving, e)
					}
				}
			}
		}

		// Move a few sprites and say so. Everything else is never written
		// again, which is what the frame's numbers should show.
		t := float64(frame) / 60
		for i, e := range moving {
			tr, ok := comps.Transform.Get(e)
			if !ok {
				continue
			}
			tr.X += float32(math.Cos(t+float64(i)) * 0.6)
			tr.Y += float32(math.Sin(t+float64(i)) * 0.6)
			comps.Transform.Wake(e)
		}
		syncStart := time.Now()
		scene.Sync()
		syncMs = time.Since(syncStart).Seconds() * 1000

		// A frame that would be identical is not worth drawing. On an OnDemand
		// window this is what makes a still world cost nothing: nothing was
		// written, so nothing asks for the next frame.
		if scene.Stats().Written > 0 {
			win.RequestRedraw()
		}

		c, _ := camComp.Get(camE)
		if err := gfxBegin(dc, scene, c, vpW, vpH, &drawMs, &presentMs); err != nil {
			return
		}

		if since := time.Since(reported); since > time.Second {
			cw, ch := dc.Size()
			fw, fh := dc.FramebufferSize()
			ww, wh := win.Size()
			logx.Info("viewport",
				"ctx", fmt.Sprintf("%dx%d", cw, ch),
				"ctxFramebuffer", fmt.Sprintf("%dx%d", fw, fh),
				"window", fmt.Sprintf("%dx%d", ww, wh),
				"scale", dc.ScaleFactor(),
				"aspect", dc.AspectRatio())

			s := scene.Stats()
			logx.Info("frame",
				"fps", float64(frame-lastReportFrame)/since.Seconds(),
				"syncMs", syncMs, "drawMs", drawMs, "presentMs", presentMs,
				"written", s.Written, "submitted", s.Submitted, "of", *side**side,
				"draws", s.Draws, "rebuilt", s.Rebuilt, "zoom", c.Zoom)
			reported = time.Now()
			lastReportFrame = frame
		}

		if *frames > 0 && frame >= *frames {
			win.Close()
		}
	})
	if err != nil {
		logx.Fatalf("window run failed: %v", err)
	}
}

// gfxBegin runs one frame: clear, camera, scene, present. The view the scene is
// given is the world rectangle the camera covers, which is what decides the
// chunks worth drawing.
func gfxBegin(dc *gogpu.Context, scene *render.Scene, c *camera.Camera, vpW, vpH float32, drawMs, presentMs *float64) error {
	gfx := scene.GPU()

	// Begin acquires the drawable, so on a vsync display this is the wait for
	// the screen rather than any cost of drawing. Timed apart for that reason.
	start := time.Now()
	if err := gfx.Begin(dc, render.Clear{R: 0.04, G: 0.05, B: 0.08, A: 1}); err != nil {
		return err
	}
	acquire := time.Since(start)
	_ = acquire

	start = time.Now()
	gfx.SetCamera(camera.ViewProj(*c, vpW, vpH), vpW, vpH)

	halfW := vpW / (2 * c.Zoom)
	halfH := vpH / (2 * c.Zoom)
	scene.Draw(render.ViewBounds{
		MinX: c.X - halfW, MaxX: c.X + halfW,
		MinY: c.Y - halfH, MaxY: c.Y + halfH,
	})

	*drawMs = time.Since(start).Seconds() * 1000

	start = time.Now()
	gfx.End()
	*presentMs = time.Since(start).Seconds() * 1000
	return nil
}
