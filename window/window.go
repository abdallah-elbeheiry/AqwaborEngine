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

	mu        sync.Mutex
	lastScale float64
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

// Size is the window in logical pixels, which is what a camera and a mouse
// position are in.
//
// A game has to be able to ask: a window moved to a display with a different
// backing scale resizes its surface, and a projection built for the old size
// draws the scene into part of the new one. There was no way to ask before, so
// every example hard-coded the size it was created with.
func (w *Window) Size() (width, height int) {
	if w.app == nil {
		return w.cfg.W, w.cfg.H
	}
	return w.app.Size()
}

// FramebufferSize is the surface in physical pixels, which is what a render
// pass covers: Size times ScaleFactor. During a frame the context's own
// FramebufferSize is the authority, because it is the drawable being written.
func (w *Window) FramebufferSize() (width, height int) {
	lw, lh := w.Size()
	s := w.ScaleFactor()
	if s <= 0 {
		s = 1
	}
	return int(float64(lw) * s), int(float64(lh) * s)
}

// ScaleFactor is physical pixels per logical pixel, and it changes when the
// window moves between displays.
func (w *Window) ScaleFactor() float64 {
	if w.app == nil {
		return 1
	}
	return w.app.ScaleFactor()
}

// WatchScale nudges the surface back into step when the window moves to a
// display with a different backing scale.
//
// gogpu 0.53.0 records EventScaleChanged and asks for a redraw, but does not
// resize the GPU surface the way every other size path does (app.go, the
// EventScaleChanged case). The logical size has not changed, so no resize event
// fires either. The surface keeps its old physical size, the frame is drawn
// into a drawable larger than the one being presented, and what the window
// shows is the top-left corner of it.
//
// Call this once a frame with the frame's own scale factor. It requests the
// size the window already has, which is what makes the surface follow.
//
// Remove it when gogpu resizes the surface on a scale change.
func (w *Window) WatchScale(scale float64) {
	if w.app == nil || scale <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastScale == 0 {
		w.lastScale = scale
		return
	}
	if w.lastScale == scale {
		return
	}
	log.Debug("backing scale changed; asking for the size the window already has",
		"from", w.lastScale, "to", scale)
	w.lastScale = scale
	lw, lh := w.app.Size()
	w.app.RequestSize(lw, lh)
}

// OnResize registers a callback for a change of size, in logical pixels. It
// fires when the window is resized and when it moves to a display with a
// different backing scale.
func (w *Window) OnResize(fn func(width, height int)) {
	if w.app == nil || fn == nil {
		return
	}
	w.app.OnResize(func(width, height int) {
		log.Debug("window resized", "w", width, "h", height)
		fn(width, height)
	})
}

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
