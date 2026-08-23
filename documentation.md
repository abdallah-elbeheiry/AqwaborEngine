# Aqwabor Engine — Simple API

Three Go files plus shaders, `package main`, no `internal/` hiding. Everything is directly accessible.

```
aqwabor/
├── go.mod                 — module aqwabor, go 1.26, CGO_ENABLED=0
├── .gitignore
├── main.go                — demo
├── schedulers/
│   ├── scheduler.go       — multi-rate scheduler (NEW)
│   ├── parallel.go        — Future / ParallelFor / AwaitAll
│   ├── scheduler_test.go
│   └── parallel_test.go
├── window.go              — goGPU auto window + colored vertices
└── shaders/
    ├── vertex.wgsl        — vertex shader (vs_main)
    ├── fragment.wgsl      — fragment shader (fs_main)
    └── colored.wgsl       — combined fallback
```

Build: `CGO_ENABLED=0 go run .`

---

## Scheduler (`schedulers/scheduler.go`)

**Multi-rate, fixed-timestep scheduler.** Register systems at different Hz; a single background goroutine drives all rates. Speed multiplier scales how often all registered functions run per wall-clock second.

```go
s := schedulers.NewScheduler()

// Register systems at their desired tick rates
s.Run(UpdateSupply, 2.0)       // 2 Hz
s.Run(UpdateCombat, 2.0)       
s.Run(UpdateAI, 1.0)           
s.Run(UpdateProduction, 0.5)   // 0.5 Hz = every 2 seconds

// Lifecycle
s.Start()
s.Pause()
s.Resume()
s.Stop()

// Global speed (applies to ALL registered functions)
s.SetSpeed(1.0)  // normal
s.SetSpeed(3.0)  // 3× as often
s.SetSpeed(0.0)  // paused (same as Pause())
```

### Key Behavior

| Property           | Behavior                                                                             |
|--------------------|--------------------------------------------------------------------------------------|
| **Multiple rates** | Each distinct Hz gets its own internal accumulator; functions grouped by rate        |
| **Fixed Δt**       | `TickState.DeltaTime` = `1/hz` — constant regardless of speed                        |
| **Speed**          | Multiplies simulation time; at 3× speed, 60 Hz functions execute ~180 times/sec      |
| **Pause/Stop**     | `Pause()` / `SetSpeed(0)` = no functions called; `Stop()` = goroutine exits entirely |

### ⚠️ Stop() stops the WHOLE scheduler

`Stop()` terminates the background goroutine. **All registered functions stop.** If you need independent lifecycles (e.g., pause combat but keep AI running), use **multiple schedulers**:

```go
combat := schedulers.NewScheduler()
combat.Run(UpdateCombat, 60.0)

ai := schedulers.NewScheduler()  
ai.Run(UpdateAI, 10.0)

combat.Start()
ai.Start()

// later: stop only combat
combat.Stop()  // AI keeps running
```

### TickState

Passed by value to every registered function:

```go
type TickState struct {
    Tick      uint64   // monotonically increasing per-rate tick count
    DeltaTime float64  // fixed = 1/hz (seconds)
}
```

---

## Parallel Utilities (`schedulers/parallel.go`)

Run work concurrently inside your tick functions. Fully awaited before function returns.

```go
// Future — async value
f := Go(func() int { return heavy() })
val := f.Get() // blocks until ready

// ParallelFor — data-parallel loop
ParallelFor(len(items), func(i int) {
    process(items[i])
})

// AwaitAll — wait on multiple futures
AwaitAll(f1, f2, f3)
```

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

## Notes

- No `internal/` — `Scheduler`, `Vertex`, `Window` are exported directly.
- Single file per concern: `schedulers/scheduler.go` (≈160 lines), `schedulers/parallel.go` (≈70 lines), `window.go` (≈320 lines with embedded shaders), `main.go` (≈40 lines), `shaders/*.wgsl` independent.
- `.gitignore` covers binaries (`AqwaborEngine`, `*.test`, `*.out`), `vendor/`, `.idea/`, `.vscode/`, `.DS_Store`, `/tmp/`.
- Pure Go, `CGO_ENABLED=0`, `go 1.26`.