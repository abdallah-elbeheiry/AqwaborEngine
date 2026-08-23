package logx

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// --- package-level convenience functions ---

func Debug(msg string, kvs ...any) { std.emit(zerolog.DebugLevel, msg, kvs) }
func Info(msg string, kvs ...any)  { std.emit(zerolog.InfoLevel, msg, kvs) }
func Warn(msg string, kvs ...any)  { std.emit(zerolog.WarnLevel, msg, kvs) }
func Error(msg string, kvs ...any) { std.emit(zerolog.ErrorLevel, msg, kvs) }
func Fatal(msg string, kvs ...any) { std.emit(zerolog.FatalLevel, msg, kvs) }
func Panic(msg string, kvs ...any) { std.emit(zerolog.PanicLevel, msg, kvs) }

func Debugf(format string, args ...any) {
	std.emit(zerolog.DebugLevel, fmt.Sprintf(format, args...), nil)
}
func Infof(format string, args ...any) {
	std.emit(zerolog.InfoLevel, fmt.Sprintf(format, args...), nil)
}
func Warnf(format string, args ...any) {
	std.emit(zerolog.WarnLevel, fmt.Sprintf(format, args...), nil)
}
func Errorf(format string, args ...any) {
	std.emit(zerolog.ErrorLevel, fmt.Sprintf(format, args...), nil)
}
func Fatalf(format string, args ...any) {
	std.emit(zerolog.FatalLevel, fmt.Sprintf(format, args...), nil)
}
func Panicf(format string, args ...any) {
	std.emit(zerolog.PanicLevel, fmt.Sprintf(format, args...), nil)
}

// --- methods on *Logger ---

func (l *Logger) Debug(msg string, kvs ...any) { l.emit(zerolog.DebugLevel, msg, kvs) }
func (l *Logger) Info(msg string, kvs ...any)  { l.emit(zerolog.InfoLevel, msg, kvs) }
func (l *Logger) Warn(msg string, kvs ...any)  { l.emit(zerolog.WarnLevel, msg, kvs) }
func (l *Logger) Error(msg string, kvs ...any) { l.emit(zerolog.ErrorLevel, msg, kvs) }
func (l *Logger) Fatal(msg string, kvs ...any) { l.emit(zerolog.FatalLevel, msg, kvs) }
func (l *Logger) Panic(msg string, kvs ...any) { l.emit(zerolog.PanicLevel, msg, kvs) }

func (l *Logger) Debugf(format string, args ...any) {
	l.emit(zerolog.DebugLevel, fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Infof(format string, args ...any) {
	l.emit(zerolog.InfoLevel, fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Warnf(format string, args ...any) {
	l.emit(zerolog.WarnLevel, fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Errorf(format string, args ...any) {
	l.emit(zerolog.ErrorLevel, fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Fatalf(format string, args ...any) {
	l.emit(zerolog.FatalLevel, fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Panicf(format string, args ...any) {
	l.emit(zerolog.PanicLevel, fmt.Sprintf(format, args...), nil)
}

// emit builds a zerolog event at the given level, short-circuiting with
// near-zero cost when the level is disabled, then appends the key/value
// fields and the message. The context kvs (from With) are prepended.
func (l *Logger) emit(level zerolog.Level, msg string, kvs []any) {
	var e *zerolog.Event
	switch level {
	case zerolog.DebugLevel:
		e = l.z.Debug()
	case zerolog.InfoLevel:
		e = l.z.Info()
	case zerolog.WarnLevel:
		e = l.z.Warn()
	case zerolog.ErrorLevel:
		e = l.z.Error()
	case zerolog.FatalLevel:
		e = l.z.Fatal()
	case zerolog.PanicLevel:
		e = l.z.Panic()
	default:
		e = l.z.Log()
	}
	if !e.Enabled() {
		return
	}
	if len(l.kvs) > 0 || len(kvs) > 0 {
		all := kvs
		if len(l.kvs) > 0 {
			all = append(append([]any{}, l.kvs...), kvs...)
		}
		addFields(e, all...)
	}
	e.Msg(msg)
}

// addFields appends alternating key/value pairs to the event. Common types
// are handled without reflection; anything else falls back to Interface.
func addFields(e *zerolog.Event, kvs ...any) {
	for i := 0; i < len(kvs); i += 2 {
		var key string
		if i+1 < len(kvs) {
			switch k := kvs[i].(type) {
			case string:
				key = k
			default:
				key = fmt.Sprintf("%v", kvs[i])
			}
			setField(e, key, kvs[i+1])
		} else {
			// trailing key with no value: record it as a string.
			e.Str(fmt.Sprintf("%v", kvs[i]), "<missing>")
		}
	}
}

func setField(e *zerolog.Event, key string, v any) {
	switch val := v.(type) {
	case string:
		e.Str(key, val)
	case bool:
		e.Bool(key, val)
	case int:
		e.Int(key, val)
	case int8:
		e.Int8(key, val)
	case int16:
		e.Int16(key, val)
	case int32:
		e.Int32(key, val)
	case int64:
		e.Int64(key, val)
	case uint:
		e.Uint(key, val)
	case uint8:
		e.Uint8(key, val)
	case uint16:
		e.Uint16(key, val)
	case uint32:
		e.Uint32(key, val)
	case uint64:
		e.Uint64(key, val)
	case float32:
		e.Float32(key, val)
	case float64:
		e.Float64(key, val)
	case time.Duration:
		e.Dur(key, val)
	case time.Time:
		e.Time(key, val)
	case error:
		if val == nil {
			e.Str(key, "<nil>")
		} else {
			e.Str(key, val.Error())
		}
	case nil:
		e.Str(key, "<nil>")
	default:
		e.Interface(key, val)
	}
}
