// Package ui is a thin façade over github.com/gogpu/ui (+ desktop, gg) that
// hides the bootstrap boilerplate (NewApp → ui/app.New → SetRoot →
// desktop.Run, plus the blank gg/gpu import) behind one small public surface.
//
// It follows the same philosophy as the engine's logx, window and input
// packages: engine code imports only aqwabor/ui and never reaches into
// gogpu/ui/... internals.
//
// Two run loops exist in the engine and must not be mixed for the same window:
//
//   - window.Run: raw goGPU vertex drawing (low-level engine render path).
//   - ui.Run:    widget toolkit driven by desktop.Run (UI shell / HUD path).
//
// See documentation.md for the split between the UI shell and the engine
// render path.
package ui

import (
	_ "github.com/gogpu/gg/gpu" // enable GPU SDF text/glyph acceleration

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/gogpu/gogpu"
	uiapp "github.com/gogpu/ui/app"
	uidesktop "github.com/gogpu/ui/desktop"
)

var log = logx.With("component", "ui")

// Config configures a UI application window.
type Config struct {
	Title     string
	W, H      int
	Resizable bool
	// Theme, if set, replaces the default theme (LightPurple). Use one of the
	// pre-made themes (ui.DarkBlue, ...) or build your own with &ui.Theme{...}.
	Theme *Theme
}

// App owns a goGPU application and its gogpu/ui App.
type App struct {
	cfg      Config
	gogpuApp *gogpu.App
	uiApp    *uiapp.App
	theme    *Theme
	content  Widget
}

// New creates a UI application. It builds a goGPU app window and the matching
// gogpu/ui App (window/event/platform providers wired from the goGPU app).
func New(cfg Config) (*App, error) {
	if cfg.W == 0 {
		cfg.W = 800
	}
	if cfg.H == 0 {
		cfg.H = 600
	}
	title := cfg.Title
	if title == "" {
		title = "Aqwabor"
	}

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(cfg.W, cfg.H).
		WithResizable(cfg.Resizable))

	th := cfg.Theme
	if th == nil {
		th = defaultM3Theme()
	}

	uiApp := uiapp.New(
		uiapp.WithWindowProvider(gogpuApp),
		uiapp.WithPlatformProvider(gogpuApp),
		uiapp.WithEventSource(gogpuApp.EventSource()),
		uiapp.WithTheme(th.toGogpu()),
	)

	a := &App{cfg: cfg, gogpuApp: gogpuApp, uiApp: uiApp, theme: th}
	log.Debug("ui app created", "title", title, "w", cfg.W, "h", cfg.H, "resizable", cfg.Resizable)
	return a, nil
}

// SetRoot sets the root widget of the UI tree. The theme's Background is
// painted as a full-window surface behind the content so changing the theme
// actually repaints the window background (relying solely on the toolkit's
// backdrop is fragile and easy to cover with a child widget).
func (a *App) SetRoot(root Widget) {
	a.content = root
	a.applyRoot()
}

func (a *App) applyRoot() {
	root := Box(a.content).
		Background(a.theme.Background).
		CrossAlign(CrossCenter)
	a.uiApp.SetRoot(root)
}

// Run blocks until the window is closed, driving the widget toolkit.
func (a *App) Run() error {
	log.Info("ui run starting", "title", a.cfg.Title)
	if err := uidesktop.Run(a.gogpuApp, a.uiApp); err != nil {
		log.Error("ui run failed", "err", err)
		return err
	}
	log.Info("ui run exited")
	return nil
}

// Close requests the UI window to close. Safe to call from a button callback.
func (a *App) Close() {
	a.gogpuApp.Quit()
}

// GogpuApp returns the underlying goGPU app as an escape hatch (e.g. to wire
// the input backend or request custom redraws).
func (a *App) GogpuApp() *gogpu.App {
	return a.gogpuApp
}

// SetTheme swaps the active theme at runtime. The window background is
// repainted immediately; if the root was already set, it is rebuilt so the new
// background applies. Rebuild your own widgets too if you want their captured
// colors (e.g. surfaces, buttons) to follow.
func (a *App) SetTheme(t *Theme) {
	a.theme = t
	a.uiApp.SetTheme(t.toGogpu())
	if a.content != nil {
		a.applyRoot()
	}
}

// Theme returns the active theme (a mutable *Theme). Mutate its fields then call
// SetTheme to apply changes.
func (a *App) Theme() *Theme {
	return a.theme
}

// Button builds a themed button: its background uses the active theme's primary
// and its text uses on-primary, with hover/press feedback. Unlike the raw
// core/button (which hardcodes grey/black), this reflects the chosen theme.
func (a *App) Button(text string, onClick func()) Widget {
	return newThemedButton(text, onClick, a.theme)
}
