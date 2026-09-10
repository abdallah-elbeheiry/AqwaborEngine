---
title: The widget toolkit
tags: [engine, aqwabor, ui]
---

## UI (`ui/`)

Thin façade over `github.com/gogpu/ui` (+ `desktop.Run`, `gg/gpu`). Hides the
bootstrap (`gogpu.NewApp` → `ui/app.New(WithWindowProvider, WithPlatformProvider,
WithEventSource, WithTheme)` → `SetRoot` → `desktop.Run`) behind one small
surface. Engine code imports only `aqwabor/ui`; it never reaches into
`gogpu/ui/...`, `gogpu/gg`, or `gogpu/desktop`.

```go
type Config struct { Title string; W, H int; Resizable bool }

app, _ := ui.New(ui.Config{Title: "Aqwabor", W: 1280, H: 720, Resizable: true})
defer app.Close()

app.SetRoot(ui.Column(
    ui.Label("Aqwabor Engine").FontSize(28).Bold(),
    ui.Label("UI layer over gogpu/ui"),
    ui.Button("Ping", func() { logx.Info("ping") }),
).Padding(24).Gap(12))

app.Run() // blocks until the window closes

app.Button("Ping", func() {}) // themed button (Primary bg / OnPrimary text)
app.Theme()                   // active *ui.Theme (mutable; read/override fields)
app.SetTheme(ui.DarkBlue)               // runtime swap
```

`Run` owns the window lifecycle via `desktop.Run` (a framework-managed loop).
Do **not** also call `window.Run` for the same window — pick one path per
window. The scheduler is independent: start/stop it around `Run` or from a
button callback; never block the UI thread with heavy work.

### Widget helpers

```go
ui.Label("text").FontSize(24).Bold().Color(widget.RGBA8(r,g,b,a)) // *TextWidget (Widget)
ui.Button("text", func() { ... })                                 // Widget
ui.Column(children ...Widget).Padding(24).Gap(12)                 // *BoxWidget (Widget)
ui.Row(children ...Widget).Padding(8)                             // *BoxWidget (Widget)
ui.Box(children ...Widget).Background(c).Rounded(12)              // *BoxWidget (Widget)
```

`Label`/`Column`/`Row`/`Box` return builders that also satisfy `ui.Widget`, so
they can be nested directly as children. `ui.Widget` is `gogpu/ui/widget.Widget`
re-exported — the only widget type call sites need.

### Alignment

Containers expose cross-axis alignment, and the façade adds short aliases plus
a chooser so you pick the position instead of hardcoding a center:

```go
// cross-axis constants (horizontal for a Column, vertical for a Row)
ui.CrossStart | ui.CrossCenter | ui.CrossEnd | ui.CrossStretch

// choose how children are positioned
ui.Align(ui.Column(...), ui.CrossCenter)  // cross center (no hardcoding)
ui.Align(ui.Column(...), ui.CrossStart)    // left (Column) / top (Row)
ui.Align(ui.Column(...), ui.CrossEnd)      // right / bottom
ui.Column(...).CrossAlign(ui.CrossCenter)  // equivalent raw method
ui.CenterX(ui.Column(...))                 // convenience == Align(..., CrossCenter)

// text alignment for a label
ui.AlignLeft | ui.AlignCenter | ui.AlignRight
ui.CenterText(ui.Label("..."))             // center the label's text
```

Note: the underlying gogpu/ui `BoxWidget` supports only cross-axis alignment;
main-axis (vertical for a Column, horizontal for a Row) alignment is start-only
in this version.

### Themes

A theme is a plain, editable struct (`ui.Theme`) of color roles. Pick one of the
six pre-made themes, or hand-roll your own struct and apply it.

```go
// six pre-made themes — just pick one
app, _ := ui.New(ui.Config{Title: "Aqwabor", W: 1280, H: 720, Theme: ui.LightPurple})

// or build from scratch — only the roles you set matter
custom := &ui.Theme{
    Primary:    ui.Hex(0x6750A4),
    OnPrimary:  ui.Hex(0xFFFFFF),
    Background: ui.Hex(0xFFFFFF),
    Surface:    ui.Hex(0xF2F2F2),
    OnSurface:  ui.Hex(0x101010),
}
app, _ := ui.New(ui.Config{Title: "Aqwabor", W: 1280, H: 720, Theme: custom})
```

Roles on `ui.Theme`: `Primary`, `OnPrimary`, `Secondary`, `OnSecondary`,
`Background`, `Surface`, `OnSurface`, `Error`, `OnError`, plus `Dark bool`.
`ui.Hex(hex)`, `ui.RGB(r,g,b)`, `ui.RGBA(r,g,b,a)` build `widget.Color` values.

Pass a theme through `Config.Theme` at startup, or swap it at runtime with
`SetTheme`. The six pre-made themes:

```go
ui.LightPurple  ui.DarkPurple   // brand purple (light / dark)
ui.Light        ui.Dark         // neutral grays (light / dark)
ui.LightBlue    ui.DarkBlue     // blue (light / dark)
```

```go
app, _ := ui.New(ui.Config{Title: "Aqwabor", W: 1280, H: 720, Theme: ui.LightPurple})
app.SetTheme(ui.DarkBlue) // runtime swap
```

`SetTheme` repaints the window background immediately and rebuilds the root if
one was already set. Widgets that captured theme colors when they were built
(e.g. a `Button` or a surface you painted) keep those colors until you rebuild
the subtree that uses them — so after `SetTheme`, rebuild the parts you want to
follow the new theme (the demo's "Cycle Theme" button does exactly this).

When `Config.Theme` is nil, `ui.New` falls back to the engine brand theme.

#### Making the theme actually visible

The gogpu/ui toolkit only paints the **window backdrop** from the theme
(`Colors.Background`); it does **not** auto-theme containers or buttons. So:

- A light theme's backdrop is near-white by design — that is expected, not a bug.
- `app.Button(...)` (and the package-level `ui.Button`) paint with the active
  theme's `Primary` background and `OnPrimary` text, with hover/press feedback.
  The raw `core/button` ignores the theme (grey/black), which is why the façade
  supplies its own painter.
- Containers (`Box`/`Column`/`Row`) are transparent by default. Paint a surface
  yourself using the theme color accessors:

```go
app.Theme()                       // active *ui.Theme (mutable)
ui.Primary(app.Theme())           // .Primary
ui.OnPrimary(app.Theme())
ui.BackgroundColor(app.Theme())   // .Background (window backdrop)
ui.SurfaceColor(app.Theme())      // .Surface
ui.OnSurfaceColor(app.Theme())    // .OnSurface (default text color)

// themed root surface + themed button
app.SetRoot(ui.Align(
    ui.Column(
        ui.Label("Title").FontSize(28).Bold(),
        app.Button("Ping", func() { logx.Info("ping") }),
    ).Padding(24).Gap(12).Background(ui.SurfaceColor(app.Theme())),
    ui.CrossCenter,
))
```

### Escape hatches

- `app.GogpuApp()` → the underlying `*gogpu.App` (wire the `input` backend via
  `input/backend/gogpu`, or request custom redraws).
- `app.Close()` → requests window close from a button callback.

Logging: all ui logs carry `component=ui`. `Debug` on create/run/exit,
`Error` on failures, no per-frame `Trace`.

## What's next

- Drawing world content rather than widgets: [the render layer](render.md)
- The window the toolkit runs in: [window](window.md)
