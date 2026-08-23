// Package logx is a very thin logging wrapper around zerolog.
//
// It exists so the rest of the engine never imports zerolog directly and so
// that the common logging call sites are simpler than raw zerolog chaining:
//
//	logx.Info("window created", "title", cfg.Title, "w", cfg.W, "h", cfg.H)
//	logx.Errorf("draw failed: %v", err)
//
// Key-value pairs are plain alternating (string, any) values, like slog/zap
// sugar. The message is always the first argument. Structured fields are
// passed through to zerolog's Event API under the hood, preserving a
// near-zero allocation path for disabled levels.
//
// Configure once at startup with logx.Init(...) and use the package-level
// helpers everywhere else.
package logx

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// std is the single active package-level logger. All logging (package-level
// helpers and child loggers created via With) emits through std's zerolog
// logger, so SetLevel/Init affect every caller. std is replaced wholesale by
// Init/SetOutput under mu; readers take a copy under RLock.
var (
	base zerolog.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.InfoLevel)
	mu   sync.RWMutex

	// lastCfg stores the most recently applied configuration so that
	// SetOutput/SetLevel can preserve the other settings.
	lastCfg *config = defaultConfig()

	// std is the package-level context carrier (no kvs); emits via current().
	std = &Logger{}

	// fatalExitFunc is the terminal action for Fatal; overridable in tests.
	fatalExitFunc = os.Exit
)

func init() {
	// Route zerolog's built-in fatal exit through our (testable) hook so that
	// Fatal terminates consistently and tests can intercept it.
	zerolog.FatalExitFunc = func() { fatalExitFunc(1) }

	mu.Lock()
	c := defaultConfig()
	applyEnvLevel(c)
	applyConfig(c)
	mu.Unlock()
}

// Logger carries key/value context that is prepended on every emit. The actual
// zerolog.Logger is shared process-wide (see current), so level changes apply
// to every Logger.
type Logger struct {
	kvs []any
}

func defaultConfig() *config {
	return &config{
		level:      zerolog.InfoLevel,
		output:     os.Stderr,
		timeFormat: time.DateTime,
		color:      true,
		hasTime:    true,
	}
}

type config struct {
	level         zerolog.Level
	explicitLevel bool // set when WithLevel was passed to this Init
	output        io.Writer
	timeFormat    string
	caller        bool
	color         bool
	json          bool
	hasTime       bool
}

// Init configures the package-level logger. Call it at process start (e.g. the
// first line of main). Subsequent calls reconfigure the global logger.
//
// Level policy: an explicit WithLevel always wins. AQWABOR_LOG is only a
// default applied when no WithLevel was provided; otherwise it is ignored.
func Init(opts ...Option) {
	mu.Lock()
	defer mu.Unlock()

	c := defaultConfig()
	for _, o := range opts {
		o(c)
	}
	applyEnvLevel(c)
	applyConfig(c)
}

func applyConfig(c *config) {
	out := c.output
	if out == nil {
		out = os.Stderr
	}

	var w io.Writer
	if c.json {
		w = out // plain zerolog output is already structured JSON
	} else {
		cw := themedConsoleWriter(c.color, c.timeFormat)
		cw.Out = out
		w = cw
	}

	z := zerolog.New(w)
	if c.hasTime && c.timeFormat != "" {
		z = z.With().Timestamp().Logger()
	}
	if c.caller {
		// zerolog's default skip (2) assumes one wrapper frame (direct
		// usage). Our thin API adds two (logx.Info -> emit), so skip 4 to
		// land on the user's call site.
		zerolog.CallerSkipFrameCount = 4
		z = z.With().Caller().Logger()
	}
	z = z.Level(c.level)

	// Keep the process-global floor in sync so copies/defaults agree.
	zerolog.SetGlobalLevel(c.level)

	base = z
	lastCfg = c
}

// current returns the active base zerolog.Logger, copied safely.
func current() zerolog.Logger {
	mu.RLock()
	z := base
	mu.RUnlock()
	return z
}

// stdLogger returns the active package logger under lock.
func stdLogger() *Logger {
	mu.RLock()
	l := std
	mu.RUnlock()
	return l
}

// applyEnvLevel applies AQWABOR_LOG only when no explicit level was set.
func applyEnvLevel(c *config) {
	if c.explicitLevel {
		return
	}
	if v := os.Getenv("AQWABOR_LOG"); v != "" {
		if lvl, err := zerolog.ParseLevel(v); err == nil {
			c.level = lvl
		}
	}
}

// Level returns the effective global level.
func Level() zerolog.Level { return current().GetLevel() }

// SetLevel changes the global level after Init (and the zerolog global floor).
func SetLevel(level zerolog.Level) {
	mu.Lock()
	defer mu.Unlock()
	zerolog.SetGlobalLevel(level)
	base = base.Level(level)
	lastCfg.level = level
	lastCfg.explicitLevel = true
}

// SetOutput redirects the global logger to w (e.g. a file), preserving all
// other settings (level, color, caller, json, timestamp).
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	c := *lastCfg
	c.output = w
	applyConfig(&c)
}

// Discard routes all global logs to io.Discard. Handy in tests.
func Discard() { SetOutput(io.Discard) }

// With returns a child logger that carries the given key/value context. The
// fields are prepended to every subsequent log call on the returned logger, so
// logs stay filterable by component, window id, etc. The child always uses the
// current global level.
func With(kvs ...any) *Logger {
	merged := make([]any, 0, len(kvs))
	merged = append(merged, kvs...)
	return &Logger{kvs: merged}
}

// Z returns the underlying global zerolog.Logger. Advanced users only; engine
// code should never need this.
func (l *Logger) Z() zerolog.Logger { return current() }

// With returns a new child of this logger with additional context.
func (l *Logger) With(kvs ...any) *Logger {
	merged := make([]any, 0, len(l.kvs)+len(kvs))
	merged = append(merged, l.kvs...)
	merged = append(merged, kvs...)
	return &Logger{kvs: merged}
}
