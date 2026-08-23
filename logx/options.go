package logx

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// Option configures the logger created by Init.
type Option func(*config)

// WithLevel sets the minimum level that will be emitted.
func WithLevel(level zerolog.Level) Option {
	return func(c *config) { c.level = level }
}

// WithOutput sets the destination writer (defaults to os.Stderr).
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.output = w }
}

// WithColor enables/disables ANSI color in the console writer.
func WithColor(on bool) Option {
	return func(c *config) { c.color = on }
}

// WithTimestamp toggles the timestamp field (on by default).
func WithTimestamp(on bool) Option {
	return func(c *config) { c.hasTime = on }
}

// WithTimeFormat sets the timestamp layout. Empty string disables timestamps.
func WithTimeFormat(layout string) Option {
	return func(c *config) {
		c.timeFormat = layout
		c.hasTime = layout != ""
	}
}

// WithCaller toggles file:line caller reporting (off by default, faster).
func WithCaller(on bool) Option {
	return func(c *config) { c.caller = on }
}

// WithJSON switches the output to structured JSON instead of console text.
func WithJSON(on bool) Option {
	return func(c *config) {
		c.json = on
		if on && c.timeFormat == "" {
			c.timeFormat = time.RFC3339
			c.hasTime = true
		}
	}
}
