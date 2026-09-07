package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
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
	snd, err := sound.New(sound.WithVolume(0))
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

	mode := flag.String("mode", "world", "demo mode: ui (widget shell), window (raw vertices), map (MapView + Camera)")
	flag.Parse()

	switch *mode {
	case "window":
		runWindowDemo()
	case "world":
		runWorldDemo()
	default:
		runUIDemo()
	}
}

// ---------------------------------------------------------------------------
// MapView + Camera test harness
//
// Exercises every feature of the map widget:
//   - initial overview (whole map centered on first layout)
//   - left-drag panning (gesture DragRecognizer)
//   - mouse-wheel zoom, toward the cursor (ZoomAt)
//   - bounds clamping (drag past the edge keeps the map in view)
//   - world<->local coordinate conversion (live cursor HUD)
//   - ZoomRange limits
//   - Row composition with a side panel of live controls
// ---------------------------------------------------------------------------

var (
	demoThemes   = []*ui.Theme{ui.LightPurple, ui.DarkPurple, ui.Light, ui.Dark, ui.LightBlue, ui.DarkBlue}
	demoThemeIdx int
)

func cycleTheme(app *ui.App) {
	demoThemeIdx = (demoThemeIdx + 1) % len(demoThemes)
	app.SetTheme(demoThemes[demoThemeIdx])
}

// genMapPNG draws a 2400x1600 procedural "world" (sea, lat/long grid, a few
// colored land regions, a center cross) and writes it as a PNG so it can be
// loaded through the normal ImageManager path.
func genMapPNG(path string, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	sea := color.RGBA{30, 90, 140, 255}
	for y := range h {
		for x := range w {
			img.Set(x, y, sea)
		}
	}

	grid := color.RGBA{60, 130, 180, 255}
	for x := 0; x <= w; x += 200 {
		for xx := 0; xx < 2 && x+xx < w; xx++ {
			for y := range h {
				img.Set(x+xx, y, grid)
			}
		}
	}
	for y := 0; y <= h; y += 200 {
		for yy := 0; yy < 2 && y+yy < h; yy++ {
			for x := range w {
				img.Set(x, y+yy, grid)
			}
		}
	}

	regions := []struct {
		x, y, w, h int
		c          color.RGBA
	}{
		{200, 200, 500, 400, color.RGBA{90, 160, 80, 255}},
		{800, 150, 600, 500, color.RGBA{200, 180, 90, 255}},
		{1500, 300, 600, 700, color.RGBA{160, 100, 160, 255}},
		{300, 900, 700, 500, color.RGBA{200, 120, 80, 255}},
		{1200, 1000, 800, 400, color.RGBA{80, 160, 160, 255}},
	}
	for _, r := range regions {
		fillRect(img, r.x, r.y, r.w, r.h, r.c)
		strokeRect(img, r.x, r.y, r.w, r.h, color.RGBA{20, 20, 20, 255})
	}

	cx, cy := w/2, h/2
	for x := cx - 40; x <= cx+40; x++ {
		img.Set(x, cy, color.RGBA{255, 255, 255, 255})
	}
	for y := cy - 40; y <= cy+40; y++ {
		img.Set(cx, y, color.RGBA{255, 255, 255, 255})
	}
	strokeRect(img, 0, 0, w, h, color.RGBA{10, 10, 10, 255})

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func fillRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			img.Set(xx, yy, c)
		}
	}
}

func strokeRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for xx := x; xx < x+w; xx++ {
		img.Set(xx, y, c)
		img.Set(xx, y+h-1, c)
	}
	for yy := y; yy < y+h; yy++ {
		img.Set(x, yy, c)
		img.Set(x+w-1, yy, c)
	}
}

// ---------------------------------------------------------------------------
// Original UI shell demo
// ---------------------------------------------------------------------------

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

func runWindowDemo() {
	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor Engine - goGPU Auto",
		W:         1280,
		H:         720,
		Resizable: true,
	})
	if err != nil {
		logx.Fatalf("failed to create window: %v", err)
	}
	defer win.Close()
	logx.Info("window ready", "title", "Aqwabor Engine - goGPU Auto", "w", 1280, "h", 720)

	s := schedulers.NewScheduler()
	s.Run(func(st schedulers.TickState) {}, 2.0)
	s.Start()
	defer s.Stop()

	var gfx *render.GPU
	var quad *render.Mesh
	var instances *render.InstanceBuffer

	if err := win.Run(func(dc *gogpu.Context) {
		if gfx == nil {
			gfx = render.New(win.DeviceProvider())
			quad = render.NewUnitQuad(gfx.Device(), gfx.Queue())
			instances = render.NewInstanceBuffer(gfx.Device(), 1)
			instances.WriteAll([]render.InstanceData{{
				Position: [2]float32{0, 0},
				Scale:    [2]float32{600, 600},
				Color:    [4]float32{1, 1, 1, 1},
			}})
			// Ortho: world [-300,300] → NDC [-1,1], Y flipped for screen.
			s := float32(1.0 / 300)
			vp := [16]float32{
				s, 0, 0, 0,
				0, -s, 0, 0,
				0, 0, 1, 0,
				0, 0, 0, 1,
			}
			gfx.SetCamera(vp, 1280, 720)
		}
		gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
		gfx.DrawInstanced(quad, instances)
		gfx.End()
	}); err != nil {
		logx.Fatalf("window run failed: %v", err)
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

	cam := camera.NewCamera()
	cam.Fit(geometry.Sz(360, 180), geometry.Sz(1280, 720))
	cam.SetPosition(geometry.Pt(0, 0))
	cam.SetZoomLimits(0.01, 1000)

	rend := maprender.NewRenderer(world, cam, nil)

	app := win.App()
	mgr := input.NewManager(gogpuinput.NewBackend(app))

	// Scroll-wheel zoom is captured on the main thread (gogpu's OnScroll
	// callback) into a small accumulator. Reading gogpu's transient frame-scroll
	// state from inside OnDraw races with its per-frame reset on the main
	// thread, so it is always observed as 0 there. The accumulator survives the
	// thread boundary and is drained once per frame below.
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
		cam.Pan(geometry.Pt(float32(dx), float32(dy)))
	})

	zoomInAction := mgr.Action("zoom_in")
	mgr.BindKey(zoomInAction, input.KeyEqual)
	zoomInAction.OnPressed(func(_ input.Context) {
		cam.SetZoom(cam.Zoom() * 1.1)
	})

	zoomOutAction := mgr.Action("zoom_out")
	mgr.BindKey(zoomOutAction, input.KeyMinus)
	zoomOutAction.OnPressed(func(_ input.Context) {
		cam.SetZoom(cam.Zoom() / 1.1)
	})

	resetAction := mgr.Action("reset")
	mgr.BindKey(resetAction, input.KeyR)
	resetAction.OnPressed(func(_ input.Context) {
		cam.Fit(geometry.Sz(360, 180), geometry.Sz(1280, 720))
		cam.SetPosition(geometry.Pt(0, 0))
	})

	var lastFrame time.Time
	lastFrame = time.Now()

	logx.Info("world demo running: drag to pan, scroll to zoom, R=reset, =/- zoom")
	var gfx *render.GPU
	if err := win.Run(func(dc *gogpu.Context) {
		now := time.Now()
		dt := float64(now.Sub(lastFrame).Seconds())
		lastFrame = now

		mgr.Update(dt)

		vp := geometry.Sz(1280, 720)

		if gfx == nil {
			gfx = render.New(win.DeviceProvider())
			rend.SetRenderer(gfx.Renderer())
		}

		// Apply accumulated scroll-wheel zoom toward the cursor.
		scrollMu.Lock()
		sd := scrollDy
		scrollDy = 0
		scrollMu.Unlock()
		if sd != 0 {
			// gogpu reports scroll-up as negative dy, so scroll up = zoom in.
			factor := float32(1.1)
			if sd < 0 {
				factor = 1 / 1.1
			}
			mx, my := app.Input().Mouse().Position()
			cam.ZoomAt(factor, geometry.Pt(mx, my), vp)
		}

		rend.SetViewport(vp)

		_ = rend.Draw(dc)
	}); err != nil {
		logx.Fatalf("window run failed: %v", err)
	}

	world.Unload()
}
