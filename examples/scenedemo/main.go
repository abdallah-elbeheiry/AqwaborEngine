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

const (
	side    = 100 // sprites a side
	spacing = 4   // world units between them
	movers  = 64  // how many of them move
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithLevel(logx.InfoLevel))
	frames := flag.Int("frames", 0, "exit after this many frames; 0 runs until the window closes")
	zoom := flag.Float64("zoom", 2, "camera zoom to start at; a high one shows the chunk cull cutting work")
	mode := flag.String("mode", "scene", "scene (chunks + GPU cull), range (chunks, no cull), whole (one draw, no cull)")
	flag.Parse()

	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor - Scene",
		W:         1280,
		H:         720,
		Resizable: true,
	})
	if err != nil {
		logx.Fatalf("window: %v", err)
	}
	defer win.Close()

	w := ecs.NewWorld()
	comps := render.MustRegisterECS(w)
	camComp := camera.MustRegisterECS(w)

	vpW, vpH := float32(1280), float32(720)
	camE := w.Create()
	camComp.Set(camE, camera.Camera{Zoom: float32(*zoom), MinZoom: 0.05, MaxZoom: 50, Active: 1})
	camComp.Wake(camE)
	cam, _ := camComp.Get(camE)
	cam.X, cam.Y = side*spacing/2, side*spacing/2

	mgr := input.NewManager(gogpuinput.NewBackend(win.App()))
	bind := func(name string, key input.Key, fn func(*camera.Camera)) {
		a := mgr.Action(name)
		mgr.BindKey(a, key)
		a.OnPressed(func(input.Context) {
			if c, ok := camComp.Get(camE); ok {
				fn(c)
			}
		})
	}
	bind("zoom_in", input.KeyEqual, func(c *camera.Camera) { c.Zoom *= 1.1; camera.ClampZoom(c) })
	bind("zoom_out", input.KeyMinus, func(c *camera.Camera) { c.Zoom /= 1.1; camera.ClampZoom(c) })
	bind("left", input.KeyA, func(c *camera.Camera) { c.X -= 8 })
	bind("right", input.KeyD, func(c *camera.Camera) { c.X += 8 })
	bind("up", input.KeyW, func(c *camera.Camera) { c.Y -= 8 })
	bind("down", input.KeyS, func(c *camera.Camera) { c.Y += 8 })

	var scene *render.Scene
	var moving []ecs.Entity
	var frame int
	var reported time.Time
	last := time.Now()

	err = win.Run(func(dc *gogpu.Context) {
		now := time.Now()
		mgr.Update(now.Sub(last).Seconds())
		last = now
		frame++

		if scene == nil {
			// The device only exists once the run loop has started, so the
			// scene is built here rather than above.
			gfx := render.New(win.DeviceProvider())
			cfg := render.SceneConfig{ChunkSize: 64}
			switch *mode {
			case "range":
				cfg.CullFrom = 1 << 30
			case "whole":
				cfg.CullFrom = 1 << 30
				cfg.MaxRuns = 1
			}
			logx.Info("drawing", "mode", *mode)
			scene = render.NewScene(gfx, w, comps, cfg)
			defer func() { reported = time.Now() }()

			for y := range side {
				for x := range side {
					shade := float32(x+y) / float32(2*side)
					e := scene.Spawn(
						render.Transform{
							X: float32(x * spacing), Y: float32(y * spacing),
							SX: 3, SY: 3,
						},
						render.Color{R: shade, G: 0.4, B: 1 - shade, A: 1},
						render.Sprite{},
					)
					if len(moving) < movers && (x+y)%37 == 0 {
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
			scene.Touch(e)
		}
		scene.Sync()

		c, _ := camComp.Get(camE)
		if err := gfxBegin(dc, scene, c, vpW, vpH); err != nil {
			return
		}

		if time.Since(reported) > time.Second {
			s := scene.Stats()
			logx.Info("frame",
				"written", s.Written, "rebuilt", s.Rebuilt,
				"submitted", s.Submitted, "of", side*side,
				"draws", s.Draws, "zoom", c.Zoom)
			reported = time.Now()
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
func gfxBegin(dc *gogpu.Context, scene *render.Scene, c *camera.Camera, vpW, vpH float32) error {
	gfx := scene.GPU()
	if err := gfx.Begin(dc, render.Clear{R: 0.04, G: 0.05, B: 0.08, A: 1}); err != nil {
		return err
	}
	gfx.SetCamera(camera.ViewProj(*c, vpW, vpH), vpW, vpH)

	halfW := vpW / (2 * c.Zoom)
	halfH := vpH / (2 * c.Zoom)
	scene.Draw(render.ViewBounds{
		MinX: c.X - halfW, MaxX: c.X + halfW,
		MinY: c.Y - halfH, MaxY: c.Y + halfH,
	})

	gfx.End()
	return nil
}
