package window

import (
	"sync"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/gogpu/gogpu"
)

var log = logx.With("component", "window")

type WindowConfig struct {
	Title     string
	W, H      int
	Resizable bool
}

// Window wraps gogpu App on auto mode (GraphicsAPIAuto, RenderModeAuto).
// It owns the OS surface and app lifecycle. All drawing goes through
// the render package — Window does not draw.
type Window struct {
	app *gogpu.App
	cfg WindowConfig

	mu sync.Mutex
}

func NewWindow(cfg WindowConfig) (*Window, error) {
	if cfg.W <= 0 || cfg.H <= 0 {
		log.Debug("invalid size, falling back to default", "w", cfg.W, "h", cfg.H)
		cfg.W = 1280
		cfg.H = 720
	}
	if cfg.Title == "" {
		cfg.Title = "AqwaborEngine"
	}
	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(cfg.Title).
		WithSize(cfg.W, cfg.H).
		WithResizable(cfg.Resizable).
		WithContinuousRender(true))
	log.Debug("window created", "title", cfg.Title, "w", cfg.W, "h", cfg.H, "resizable", cfg.Resizable)
	return &Window{app: app, cfg: cfg}, nil
}

func (w *Window) App() *gogpu.App { return w.app }

// DeviceProvider returns the GPU device provider for the render package.
func (w *Window) DeviceProvider() gogpu.DeviceProvider { return w.app.DeviceProvider() }

// Run starts the app draw loop. The callback receives the per-frame
// gogpu.Context and should call render.GPU methods to draw.
func (w *Window) Run(onDraw func(dc *gogpu.Context)) error {
	if w.app == nil {
		return nil
	}
	wrapped := func(dc *gogpu.Context) {
		w.mu.Lock()
		w.mu.Unlock()
		log.Trace("frame begin")
		onDraw(dc)
		log.Trace("frame end")
	}
	w.app.OnDraw(wrapped)
	log.Debug("window run loop starting")
	err := w.app.Run()
	if err != nil {
		log.Errorf("window run loop exited with error: %v", err)
	} else {
		log.Debug("window run loop exited cleanly")
	}
	return err
}

func (w *Window) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	log.Debug("window closed")
}
