---
title: Window
tags: [engine, aqwabor, window]
---

A window is a surface, a device and a frame callback. It does not draw.

```go
win, err := window.NewWindow(window.WindowConfig{
    Title: "Aqwabor", W: 1280, H: 720, Resizable: true,
})
defer win.Close()

gfx := render.New(win.DeviceProvider())

err = win.Run(func(dc *gogpu.Context) {
    gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
    gfx.SetCamera(viewProj, w, h)
    // draw
    gfx.End()
})
```

`Run` blocks until the window closes.

The package used to carry a renderer as well: a `Draw` taking clip-space vertices, a pipeline cache,
an ear-clipping triangulator and per-frame vertex buffers. All of it is gone. Drawing goes through
the [render layer](render.md), which owns the pipelines, keeps its buffers across frames, and puts
the camera in a uniform rather than transforming vertices on the CPU.

`win.App()` reaches the underlying `gogpu.App` for anything the façade does not cover, such as wiring
the input backend or requesting a redraw.

## Two run loops, and picking one

`ui.Run` drives the widget toolkit; `window.Run` drives world rendering. Both wrap the same graphics
stack. Never drive one window with both.

The scheduler is independent of either: start and stop it around the run loop, or from a button
callback. Do not block the loop with simulation work.

## What's next

- Drawing world content: [the render layer](render.md)
- Widgets, menus and panels: [the widget toolkit](ui.md)
- Keyboard, mouse and actions: [input](input.md)
