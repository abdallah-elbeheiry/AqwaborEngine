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

// std is the package-level logger used by the convenience functions.
// cfg is its current configuration, used to re-apply settings on changes.
var (
	std *Logger
	cfg *config
	mu  sync.RWMutex
)

func init() {
	mu.Lock()
	cfg = defaultConfig()
	applyConfig(cfg)
	mu.Unlock()
	applyEnvLevel()
}

// Logger is a thin context-carrying wrapper around a zerolog.Logger.
// Create children with logx.With or Logger.With; the child carries the extra
// key/value context and prepends it on every emit.
type Logger struct {
	z   zerolog.Logger
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
	level      zerolog.Level
	output     io.Writer
	timeFormat string
	caller     bool
	color      bool
	json       bool
	hasTime    bool
}

// Init configures the package-level logger. Call it exactly once at process
// startup (e.g. the first line of main). Subsequent calls reconfigure std.
func Init(opts ...Option) {
	mu.Lock()
	defer mu.Unlock()

	c := defaultConfig()
	for _, o := range opts {
		o(c)
	}
	cfg = c
	applyConfig(c)
	applyEnvLevel()
}

func applyConfig(c *config) {
	out := c.output
	if out == nil {
		out = os.Stderr
	}

	var w io.Writer
	if c.json {
		// Plain zerolog output is already structured JSON.
		w = out
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
		zerolog.CallerSkipFrameCount = 2
		z = z.With().Caller().Logger()
	}
	z = z.Level(c.level)
	std = &Logger{z: z}
}

func applyEnvLevel() {
	if v := os.Getenv("AQWABOR_LOG"); v != "" {
		if lvl, err := zerolog.ParseLevel(v); err == nil {
			cfg.level = lvl
			std.z = std.z.Level(lvl)
		}
	}
}

// SetLevel changes the global level after Init.
func SetLevel(level zerolog.Level) {
	mu.Lock()
	defer mu.Unlock()
	cfg.level = level
	std.z = std.z.Level(level)
}

// SetOutput redirects the global logger to w (e.g. a file), preserving all
// other settings.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	cfg.output = w
	applyConfig(cfg)
}

// Discard routes all global logs to io.Discard. Handy in tests.
func Discard() {
	SetOutput(io.Discard)
}

// With returns a child logger that carries the given key/value context.
// The fields are prepended to every subsequent log call on the returned
// logger, so logs stay filterable by component, window id, etc.
func With(kvs ...any) *Logger {
	mu.RLock()
	base := std
	mu.RUnlock()
	return &Logger{z: base.z, kvs: append([]any{}, kvs...)}
}

// Z returns the underlying zerolog.Logger. Advanced users only; engine code
// should never need this.
func (l *Logger) Z() zerolog.Logger { return l.z }

// With returns a new child of this logger with additional context.
func (l *Logger) With(kvs ...any) *Logger {
	merged := make([]any, 0, len(l.kvs)+len(kvs))
	merged = append(merged, l.kvs...)
	merged = append(merged, kvs...)
	return &Logger{z: l.z, kvs: merged}
}
