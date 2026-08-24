# Aqwabor Engine — Simple API

Three Go files plus shaders, `package main`, no `internal/` hiding. Everything is directly accessible.

```
aqwabor/
├── go.mod                 — module aqwabor, go 1.26, CGO_ENABLED=0
├── .gitignore
├── documentation.md
├── cmd/aqwabor/main.go    — demo entrypoint
├── logx/                  — thin logging wrapper over zerolog (the ONLY package that imports zerolog)
│   ├── logx.go            — Init, package-level helpers, *Logger
│   ├── methods.go         — level methods + key/value field helper
│   ├── options.go         — Init options (level, output, color, caller, json, timestamp)
│   ├── levels.go          — re-exported level constants
│   ├── theme.go           — purple/blue console theme
│   └── logx_test.go
├── schedulers/
│   ├── scheduler.go       — multi-rate scheduler
│   ├── parallel.go        — Future / ParallelFor / AwaitAll
│   ├── scheduler_test.go
│   └── parallel_test.go
├── window/window.go       — goGPU auto window + colored vertices
├── sound/                 — audio: Context → Clip (cached asset) → Player (instance)
│   ├── sound.go           — Context, New, Close, master volume, options
│   ├── clip.go            — LoadAudio/LoadAudioFile, cache, Clip volume
│   ├── player.go          — Play/PlayLoop/Stop/Pause/Resume, effective volume
│   ├── backend.go         — gogpu/audio wiring (WAV + MP3), looping PCM source
│   └── sound_test.go
├── input/                 — high-level input system
│   ├── input.go           — Manager + Backend
│   ├── action.go          — Action + derived events
│   └── backend/           — gogpu + headless backends
└── shaders/
    ├── vertex.wgsl        — vertex shader (vs_main)
    ├── fragment.wgsl       — fragment shader (fs_main)
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

## Sound (`sound/`)

Thin wrapper over the pure-Go `github.com/gogpu/audio` engine. Three-tier model:

- **`Context`** — owns the audio device, the master volume, and the clip cache. One per app. `Close` tears down the device and invalidates every Clip/Player.
- **`Clip`** — a cached, decoded audio asset. `LoadAudio`/`LoadAudioFile` decode on first load (not first play). A Clip is not audible by itself; play it by creating `Player`s.
- **`Player`** — one playback instance. Multiple Players can play the same Clip concurrently (overlapping SFX); each has its own volume and can be stopped/paused/resumed.

```go
ctx, err := sound.New(
    sound.WithSilent(false),  // null (no-output) driver when true; default null on non-Windows
    sound.WithVolume(0.9),    // initial master volume, clamped [0,1]
    // sound.WithSampleRate(44100),
    // sound.WithChannels(2),
)
if err != nil { /* device open failed */ }
defer ctx.Close()

ctx.SetMasterVolume(0.5)      // clamped [0,1]; re-applied to active players
_ = ctx.MasterVolume()        // 0.5

clip, err := ctx.LoadAudioFile("sfx/click.wav") // or ctx.LoadAudio([]byte{...})
if err != nil { /* unsupported format / decode failed */ }
clip.SetVolume(0.8)           // default gain for new players

p, err := clip.Play()         // one-shot
if err != nil { /* play failed */ }
// p, _ := clip.PlayLoop()    // loops until Stop

p.SetVolume(1.0)              // instance gain, clamped [0,1]
p.Pause()                     // err == nil (supported)
p.Resume()
p.Stop()                      // ends this instance only
```

### Volume rule

`effective = masterVolume * clipVolume * playerVolume`, each factor clamped to `[0,1]`. A master of `0` ⇒ silence regardless of the others. Because `gogpu/audio`'s mixer has no public master control, this package folds the formula into every backend `Player`'s own volume and re-applies it whenever master/clip/player volume changes.

### Formats (v1)

- **WAV** — fully supported (PCM 8/16/24/32-bit and 32-bit float) via `gogpu/audio`.
- **MP3** — supported via the pure-Go `github.com/hajimehoshi/go-mp3` decoder.
- Anything else → `ErrUnsupportedFormat` (detected by RIFF/WAVE or ID3/MPEG-sync header, with an extension fallback for files).

### Cache

- `LoadAudioFile(path)` — normalised path is the key; same path ⇒ same `*Clip`.
- `LoadAudio(data)` — SHA-256 of the bytes is the key; identical bytes ⇒ same `*Clip`.
- Decode happens on first load so format/codec errors surface early.

### Lifecycle / errors

- After `Context.Close`, every Clip/Player method fails with `ErrClosed`.
- `Close` is idempotent (safe to call twice).
- `Pause`/`Resume` are supported by the backend and return `nil`; `ErrNotSupported` is defined for forward compatibility.

### Logging policy (`component=sound`)

All sound logs carry `component=sound`. They are detailed by design (enable with
`logx.WithLevel(logx.DebugLevel)`; the engine default is `Info`, which hides them).

- **Debug** — context open/close (sample rate, channels, master, silent flag); clip loaded (id, format, sample rate, channels, duration, bytes, cache hit/miss); master-volume and clip-volume changes (with affected player counts); and **each** Play/Stop/Pause/Resume (player id, clip id, loop flag, effective volume). No per-buffer / per-mix noise.
- **Error** — device open failed, decode failed, play failed, read-file failed.

---

## Logging (`logx`)

All logging flows through the thin `logx` package. **The engine never imports
`zerolog` directly — only `logx` does.** The API is deliberately simpler than
raw zerolog chaining: the message is always first, fields are plain
alternating `(string, any)` key/value pairs (no `.Str().Int().Msg()` chains),
and there are one-line `Fatal`/`Panic` helpers.

### Initialize once at startup

```go
func main() {
    logx.Init(
        logx.WithColor(true),                       // purple/blue console theme
        logx.WithLevel(logx.DebugLevel),            // Trace/Debug/Info/Warn/Error/Fatal/Panic
        logx.WithTimestamp(true),                  // RFC-ish timestamp
        // logx.WithJSON(true),                     // structured JSON instead of console
        // logx.WithCaller(true),                   // file:line (off by default, faster)
        // logx.WithOutput(os.Stdout),              // redirect destination
    )
    runGame()
}
```

Level policy: an explicit `logx.WithLevel(...)` passed to `Init` **always
wins**. The `AQWABOR_LOG` environment variable (`trace|debug|info|warn|error|
fatal|panic`) is only a *default* applied when no level option was provided;
if you pass `WithLevel`, the env var is ignored. `SetLevel` overrides at
runtime. Both the per-logger and the zerolog global floor are kept in sync, so
`SetLevel`/`Init` affect every logger (including ones created via `With`).

```go
logx.Level() // effective level (zerolog.Level), handy for diagnostics
```

### Trace vs Debug (engine only)

Engine-internal logging follows a strict volume policy so games built on the
engine stay quiet by default:

- **Trace** — noisiest plumbing, *off by default*. Per-tick / per-frame / per-poll bodies: scheduler tick entry, every `Draw`/`DrawPolygon` submission, buffer uploads, pipeline binds, raw input samples, `ParallelFor` chunks. Anything that scales with Hz, FPS, or input rate.
- **Debug** — engine diagnostics, safe to leave on during development but still *not* per-frame: lifecycle (`Start`/`Stop`/window create/close), setup config (registered Hz, `SetSpeed`, effective level), and rare anomalies (recoverable draw error, fallback pipeline path, backend selection).
- **Info+** — process milestones (`window ready`, clean shutdown) and real problems (`WARN`/`ERROR`/`FATAL`). Never reclassify these into Trace/Debug.

```go
logx.Trace("draw submitted", "vertices", n)     // scales with FPS -> Trace
logx.Debug("scheduler started", "rates", rates) // one-shot setup -> Debug
logx.Info("window ready", "title", t, "w", w)  // milestone -> Info
```

### Call sites are short

```go
logx.Info("window created", "title", cfg.Title, "w", cfg.W, "h", cfg.H)
logx.Errorf("draw failed: %v", err)
logx.Warn("low memory", "mb", 12)
logx.Fatal("cannot continue")      // logs then os.Exit(1)
```

### Components: child loggers with context

`logx.With` returns a `*logx.Logger` that prepends context to every line, so
logs stay filterable by subsystem:

```go
var log = logx.With("component", "window")

log.Debug("draw submitted", "vertices", n, "first_clear", isFirst)
log.Error("failed to create render pipeline", "err", err)
```

Child loggers nest: `log.With("window", "main").Warn("slow frame", "ms", 22)`.

### Available surface

```go
// package-level
logx.Trace/Debug/Info/Warn/Error/Fatal/Panic(msg, kvs ...any)
logx.Tracef/Debugf/Infof/Warnf/Errorf/Fatalf/Panicf(format, args ...any)
logx.With(kvs ...any) *Logger

// per-logger (same set)
l.Trace/Debug/Info/Warn/Error/Fatal/Panic(msg, kvs ...any)
l.With(kvs ...any) *Logger

// configuration
logx.Init(opts ...Option)
logx.Level() zerolog.Level       // effective level
logx.SetLevel(logx.DebugLevel)   // re-exported from zerolog: Trace/Debug/Info/Warn/Error/Fatal/Panic/NoLevel/Disabled (constants)
logx.SetOutput(io.Writer)        // preserves level/color/caller/json
logx.Discard()                   // tests

// advanced escape hatch (engine code should not use it)
l.Z() zerolog.Logger
```

Errors are special-cased: a field whose value is an `error` and whose key is
`"error"` or `"err"` is emitted with zerolog's `Err()` so the console renders
it in the error style (e.g. `logx.Error("boom", "err", err)`). `Fatal`/
`Fatalf` always terminate the process (`os.Exit(1)`) and `Panic`/`Panicf`
always panic — even when the level is `Disabled`, so a fatal condition can
never be silently swallowed.

Disabled levels short-circuit with near-zero cost (the `*f` helpers also skip
`fmt.Sprintf` until the level is enabled), and structured fields are
type-switched (string/int/bool/float/error/time.Duration/…) to avoid
reflection on the hot path. The console theme leans purple/blue while keeping
severity readable: blue `DEBUG` → light-blue `INFO` → purple `WARN` → red
`ERROR`/`FATAL`/`PANIC`, with blue field names and a lavender message.

---

## Notes

- No `internal/` — `Scheduler`, `Vertex`, `Window`, `logx` are exported directly. `logx` is the only package that depends on `zerolog`; every other package imports `logx`, never `zerolog`.
- Single file per concern: `schedulers/scheduler.go` (≈170 lines), `schedulers/parallel.go` (≈70 lines), `window/window.go` (≈360 lines with embedded shaders), `input/input.go` + `input/action.go`, `logx/*`, `cmd/aqwabor/main.go`, `shaders/*.wgsl` independent.
- `.gitignore` covers binaries (`AqwaborEngine`, `*.test`, `*.out`), `vendor/`, `.idea/`, `.vscode/`, `.DS_Store`, `/tmp/`.
- Pure Go, `CGO_ENABLED=0`, `go 1.26`.