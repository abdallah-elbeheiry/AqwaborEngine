package main

import (
	"flag"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
	gogpuinput "github.com/abdallah-elbeheiry/AqwaborEngine/input/backend/gogpu"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/maprender"
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/abdallah-elbeheiry/AqwaborEngine/schedulers"
	"github.com/abdallah-elbeheiry/AqwaborEngine/sound"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ui"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/geometry"
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithTimestamp(true), logx.WithLevel(logx.TraceLevel))

	// Open the audio context once and keep it alive for the whole program so the
	// song keeps playing while the demo runs.
	snd, err := sound.New(sound.WithVolume(0.5))
	if err != nil {
		logx.Errorf("sound init (no audio device?): %v", err)
	} else {
		defer snd.Close()
		clip, err := snd.LoadAudioFile("examples/song-example.mp3")
		clip.SetVolume(1)
		if err != nil {
			logx.Errorf("sound load: %v", err)
		} else if p, err := clip.PlayLoop(); err != nil {
			logx.Errorf("sound play: %v", err)
		} else {
			logx.Info("playing song-example.mp3", "playerVolume", p.Volume(), "master", snd.MasterVolume())
		}
	}

	mode := flag.String("mode", "world", "demo mode: world (vector map ECS), ui (widget shell + image)")
	flag.Parse()

	switch *mode {
	case "ui":
		runUIDemo()
	default:
		runWorldDemo()
	}
}

func runUIDemo() {
	app, err := ui.New(ui.Config{
		Title:     "Aqwabor",
		W:         1280,
		H:         720,
		Resizable: true,
		Theme:     ui.LightPurple,
	})
	if err != nil {
		logx.Fatalf("ui: %v", err)
	}
	defer app.Close()

	s := schedulers.NewScheduler()
	s.Run(func(st schedulers.TickState) {}, 2.0)
	s.Start()
	defer s.Stop()

	// Load the project's bundled fox.png through the app's image manager. Assets
	// are loaded explicitly (no hidden I/O in widgets) and one asset can be
	// reused by many widgets.
	var fox *ui.ImageAsset
	if foxAsset, err := app.Images().Load("examples/fox.png"); err != nil {
		logx.Warnf("image load (examples/fox.png): %v", err)
	} else {
		fox = foxAsset
	}

	themes := []*ui.Theme{
		ui.LightPurple, ui.DarkPurple, ui.Light, ui.Dark, ui.LightBlue, ui.DarkBlue,
	}
	idx := 0

	// build rebuilds the root so captured colors (surface, button) follow the
	// active theme when we switch it at runtime.
	var build func()
	build = func() {
		children := []ui.Widget{
			ui.CenterText(ui.Label("Aqwabor Engine").FontSize(28).Bold()),
			ui.CenterText(ui.Label("UI layer over gogpu/ui")),
			app.Button("Ping", func() { logx.Info("ping") }),
			app.Button("Cycle Theme", func() {
				idx = (idx + 1) % len(themes)
				app.SetTheme(themes[idx])
				build()
			}),
		}
		// A single image button under Cycle Theme. Clickable via ImageButton.
		if fox != nil {
			children = append(children,
				ui.ImageButton(fox, func() { logx.Info("fox image clicked") }),
			)
		}
		app.SetRoot(ui.Align(ui.Column(children...).
			Padding(24).Gap(12).Background(ui.SurfaceColor(app.Theme())), ui.CrossCenter))
	}
	build()

	if err := app.Run(); err != nil {
		logx.Fatalf("ui run: %v", err)
	}

	// After Run returns the widgets are gone, so releasing the asset now
	// succeeds. ForceRelease is intentionally not used here.
	if fox != nil {
		if ok := app.Images().TryRelease(fox); !ok {
			logx.Warn("fox asset still in use at shutdown")
		}
	}
}

// ---------------------------------------------------------------------------
// World vector map demo (loads world_v3.json, draws with camera)
// ---------------------------------------------------------------------------

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
	render.MustRegisterECS(w)
	scenes := maprender.MustRegisterECS(w)
	sceneComp := scenes.Component()

	// Camera entity — source of truth for all view state.
	// Matches old behaviour: Fit centres, then position reset to origin.
	vp := geometry.Sz(1280, 720)
	camE := w.Create()
	camComp.Set(camE, camera.Camera{
		MinZoom: 0.01,
		MaxZoom: 1000,
		Active:  1,
	})
	camComp.Wake(camE)
	c, _ := camComp.Get(camE)
	c.Fit(360, 180, vp.Width, vp.Height)
	c.X = 0
	c.Y = 0

	// Map scene entity — clear colour + world scale.
	mapE := w.Create()
	sceneComp.Set(mapE, maprender.MapScene{
		ClearR:     world.Background.R,
		ClearG:     world.Background.G,
		ClearB:     world.Background.B,
		ClearA:     world.Background.A,
		WorldScale: float32(world.Scale),
		Active:     1,
	})
	sceneComp.Wake(mapE)

	// Renderer — CPU fill triangulation in NewRenderer, GPU mesh built later.
	rend := maprender.NewRenderer(world, 1, nil)
	scenes.Bind(mapE, rend)

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
		c.Pan(float32(dx), float32(dy))
	})

	zoomInAction := mgr.Action("zoom_in")
	mgr.BindKey(zoomInAction, input.KeyEqual)
	zoomInAction.OnPressed(func(_ input.Context) {
		c, ok := camComp.Get(camE)
		if !ok {
			return
		}
		c.Zoom *= 1.1
		camera.ClampZoom(c)
	})

	zoomOutAction := mgr.Action("zoom_out")
	mgr.BindKey(zoomOutAction, input.KeyMinus)
	zoomOutAction.OnPressed(func(_ input.Context) {
		c, ok := camComp.Get(camE)
		if !ok {
			return
		}
		c.Zoom /= 1.1
		camera.ClampZoom(c)
	})

	resetAction := mgr.Action("reset")
	mgr.BindKey(resetAction, input.KeyR)
	resetAction.OnPressed(func(_ input.Context) {
		c, ok := camComp.Get(camE)
		if !ok {
			return
		}
		c.Fit(360, 180, float32(vp.Width), float32(vp.Height))
		c.X = 0
		c.Y = 0
	})

	var lastFrame time.Time
	lastFrame = time.Now()

	logx.Info("world demo running: drag to pan, scroll to zoom, R=reset, =/- zoom")
	var gfx *render.GPU
	if err := win.Run(func(dc *gogpu.Context) {
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		mgr.Update(dt)

		if gfx == nil {
			gfx = render.New(win.DeviceProvider())
			if c, ok := camComp.Get(camE); ok {
				rend.SetRefZoom(c.Zoom)
			}
			rend.SetRenderer(gfx.Renderer())
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
				c.ZoomAt(factor, float32(mx), float32(my), float32(vp.Width), float32(vp.Height))
			}
		}

		// Read components → build view-projection → draw.
		cam, _ := camComp.Get(camE)
		scene, _ := sceneComp.Get(mapE)

		rend.SetViewport(vp)
		rend.SetZoom(cam.Zoom)
		vpMat := render.ViewProjMap(*cam, float32(vp.Width), float32(vp.Height), scene.WorldScale)
		gfx.SetCamera(vpMat, float32(vp.Width), float32(vp.Height))
		gfx.Begin(dc, render.Clear{R: scene.ClearR, G: scene.ClearG, B: scene.ClearB, A: scene.ClearA})
		scenes.Draw(mapE)
		gfx.End()
	}); err != nil {
		logx.Fatalf("window run failed: %v", err)
	}

	world.Unload()
}
