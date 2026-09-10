---
title: Logging
tags: [engine, aqwabor, logx]
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

- **Trace** — noisiest plumbing, *off by default*. Anything that scales with entity count, Hz, FPS or
  input rate: entity creation and destruction, component attach and detach, scheduler tick entry,
  draw submissions, buffer uploads, raw input samples.
- **Debug** — engine diagnostics, safe to leave on during development but not per-frame: lifecycle
  (`Start`, `Stop`, window create and close), setup (registered rates, `SetSpeed`, effective level),
  and rare anomalies (a recoverable draw error, a fallback path, backend selection).
- **Info and above** — process milestones (world created, window ready, clean shutdown), one-off
  registrations, and real problems. Never reclassify these downward.

The structural operations of the entity system sit at Trace for exactly this reason. They used to
sit at Info, which is the default, so every game paid for a log line per entity it created.

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

## Enabled, and why a hot path needs it

Every logging call builds a slice of its key/value pairs and boxes each value into an interface
before the level is consulted, so a call on a per-entity or per-frame path costs allocations even
when its output is discarded. `Enabled` skips that construction:

```go
if log.Enabled(logx.TraceLevel) {
    log.Trace("component attached", "entity", e, "component_id", id)
}
```

It is worth doing only where the call runs per entity or per frame; elsewhere the guard costs more
reading than it saves.

Entity creation plus one component measured 2702 ns and 70 allocations before the engine's structural
log lines were moved to Trace and guarded this way, and 21.8 ns and none after.

## What's next

- How a component is reached, and what a system iterates: [the entity component system](ecs.md)
- What runs when, and what may run at the same time: [scheduling](scheduling.md)
