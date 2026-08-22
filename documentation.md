# Aqwabor Engine — Simple API

Three Go files plus shaders, `package main`, no `internal/` hiding. Everything is directly accessible.

```
aqwabor/
├── go.mod                 — module aqwabor, go 1.26, CGO_ENABLED=0
├── .gitignore
├── main.go                — demo
├── loop.go                — single-threaded fixed-tick loop
├── window.go              — goGPU auto window + colored vertices
└── shaders/
    ├── vertex.wgsl        — vertex shader (vs_main)
    ├── fragment.wgsl      — fragment shader (fs_main)
    └── colored.wgsl       — combined fallback
```

Build: `CGO_ENABLED=0 go run .`

---

## Loop (`loop.go`)

Single-threaded, deterministic. One goroutine ticks, tasks run in order every tick. No steps are skipped — if overloaded it lags but stays correct.

```go
loop := NewLoop(60) // Hz
loop.Start()
defer loop.Stop()

loop.Do(func(){})                // every tick
loop.Once(func(){})              // once
loop.Every(100*time.Millisecond, fn)
loop.Times(10, fn)
loop.After(time.Second, fn)
loop.AfterTicks(5, fn)

// advanced options via CreateTask:
t := CreateTask(fn, Once())
t = CreateTask(fn, Every(d), Until(func()bool{return done}), FinishAfter(d))
t.Cancel()
t.IsDone()

g := loop.Serial() // ordered pipeline, runs before Do tasks each tick
g.Do(fn); g.Once(fn); g.Every(d, fn); g.Times(n, fn); g.After(d, fn); g.AfterTicks(n, fn)

loop.Alpha()      // [0,1] interpolation to next tick
loop.TickCount()  // ticks executed
loop.Delta()      // 1/Hz
loop.Lag()        // behind real time
loop.AchievedHz() // avg Hz since Start
```

`Add` and all schedulers are safe from any goroutine; tasks added inside a task run next tick. `Serial()` groups run in creation order each tick.

---

## Window (`window.go`)

Thin wrapper over `github.com/gogpu/gogpu` on **auto** (`DefaultConfig` → `GraphicsAPIAuto`, `RenderModeAuto`, event-driven 0% CPU idle). No SDL/GL.

```go
type Vertex struct { X, Y float32; R, G, B, A float32 } // clip-space + color
type WindowConfig struct { Title string; W, H int; Resizable bool }

win, _ := NewWindow(WindowConfig{Title: "Aqwabor", W: 1280, H: 720, Resizable: true})
defer win.Close()

// drive rendering:
win.Run(func(dc *gogpu.Context) {
    // dc.Clear is optional — Window clears to 0.05,0.05,0.1 on first Draw per frame
    win.DrawPolygon(dc, quad) // concave ok, keeps per-corner color
    win.Draw(dc, triList)    // triangles: 3 verts each
})
// Run blocks until window closed. Use win.App() for advanced goGPU:
win.App().OnUpdate(func(dt float64){})
win.App().Input()              // Ebiten-style polling
win.App().DeviceProvider()     // *wgpu.Device, *wgpu.Queue
win.App().RequestRedraw()
```

`Draw`/`DrawPolygon` must be called inside `Run`'s `OnDraw`. `DrawPolygon` triangulates via ear clipping (`triangulate` inside `window.go`). `Window` retains vertex buffers for ~8 frames then releases (Vulkan submit safety).

Shaders are in independent files, embedded at compile time:

```go
//go:embed shaders/vertex.wgsl
var vertexWGSL string
//go:embed shaders/fragment.wgsl
var fragmentWGSL string
//go:embed shaders/colored.wgsl
var coloredWGSL string // combined fallback
```

Pipeline: vertex `pos@0 vec2` + `color@1 vec4`, stride 24, `TriangleList`, `vs_main`/`fs_main` from `shaders/vertex.wgsl` + `shaders/fragment.wgsl`, created lazily per `TextureFormat`.

---

## Demo (`main.go`)

1. `measure()` — 240Hz loop, 64 tasks, 5s, prints `achieved Hz` + `lag`.
2. `runWindowDemo()` — 60Hz loop + goGPU window drawing a colored quad.

## Notes

- No `internal/` — `Loop`, `Task`, `Vertex`, `Window` are in `package main` directly.
- Single file per concern: `loop.go` (≈490 lines), `window.go` (≈320 lines with embedded shaders), `main.go` (≈90 lines), `shaders/*.wgsl` independent.
- `.gitignore` covers binaries (`AqwaborEngine`, `*.test`, `*.out`), `vendor/`, `.idea/`, `.vscode/`, `.DS_Store`, `/tmp/`.
- Pure Go, `CGO_ENABLED=0`, `go 1.26`.
