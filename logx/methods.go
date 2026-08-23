package logx

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// --- package-level convenience functions ---

func Trace(msg string, kvs ...any) { stdLogger().emit(zerolog.TraceLevel, msg, kvs) }
func Debug(msg string, kvs ...any) { stdLogger().emit(zerolog.DebugLevel, msg, kvs) }
func Info(msg string, kvs ...any)  { stdLogger().emit(zerolog.InfoLevel, msg, kvs) }
func Warn(msg string, kvs ...any)  { stdLogger().emit(zerolog.WarnLevel, msg, kvs) }
func Error(msg string, kvs ...any) { stdLogger().emit(zerolog.ErrorLevel, msg, kvs) }
func Fatal(msg string, kvs ...any) { stdLogger().emit(zerolog.FatalLevel, msg, kvs) }
func Panic(msg string, kvs ...any) { stdLogger().emit(zerolog.PanicLevel, msg, kvs) }

// Formatted variants avoid formatting work when the level is disabled.
func Tracef(format string, args ...any) {
	z := current()
	if e := z.Trace(); e.Enabled() {
		stdLogger().emit(zerolog.TraceLevel, fmt.Sprintf(format, args...), nil)
	}
}
func Debugf(format string, args ...any) {
	z := current()
	if e := z.Debug(); e.Enabled() {
		stdLogger().emit(zerolog.DebugLevel, fmt.Sprintf(format, args...), nil)
	}
}
func Infof(format string, args ...any) {
	z := current()
	if e := z.Info(); e.Enabled() {
		stdLogger().emit(zerolog.InfoLevel, fmt.Sprintf(format, args...), nil)
	}
}
func Warnf(format string, args ...any) {
	z := current()
	if e := z.Warn(); e.Enabled() {
		stdLogger().emit(zerolog.WarnLevel, fmt.Sprintf(format, args...), nil)
	}
}
func Errorf(format string, args ...any) {
	z := current()
	if e := z.Error(); e.Enabled() {
		stdLogger().emit(zerolog.ErrorLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Fatalf/Panicf always terminate, so they format unconditionally (emit ensures
// exit/panic even when the level is disabled).
func Fatalf(format string, args ...any) {
	stdLogger().emit(zerolog.FatalLevel, fmt.Sprintf(format, args...), nil)
}
func Panicf(format string, args ...any) {
	stdLogger().emit(zerolog.PanicLevel, fmt.Sprintf(format, args...), nil)
}

// --- methods on *Logger ---

func (l *Logger) Trace(msg string, kvs ...any) { l.emit(zerolog.TraceLevel, msg, kvs) }
func (l *Logger) Debug(msg string, kvs ...any) { l.emit(zerolog.DebugLevel, msg, kvs) }
func (l *Logger) Info(msg string, kvs ...any)  { l.emit(zerolog.InfoLevel, msg, kvs) }
func (l *Logger) Warn(msg string, kvs ...any)  { l.emit(zerolog.WarnLevel, msg, kvs) }
func (l *Logger) Error(msg string, kvs ...any) { l.emit(zerolog.ErrorLevel, msg, kvs) }
func (l *Logger) Fatal(msg string, kvs ...any) { l.emit(zerolog.FatalLevel, msg, kvs) }
func (l *Logger) Panic(msg string, kvs ...any) { l.emit(zerolog.PanicLevel, msg, kvs) }

func (l *Logger) Tracef(format string, args ...any) {
	z := current()
	if e := z.Trace(); e.Enabled() {
		l.emit(zerolog.TraceLevel, fmt.Sprintf(format, args...), nil)
	}
}
func (l *Logger) Debugf(format string, args ...any) {
	z := current()
	if e := z.Debug(); e.Enabled() {
		l.emit(zerolog.DebugLevel, fmt.Sprintf(format, args...), nil)
	}
}
func (l *Logger) Infof(format string, args ...any) {
	z := current()
	if e := z.Info(); e.Enabled() {
		l.emit(zerolog.InfoLevel, fmt.Sprintf(format, args...), nil)
	}
}
func (l *Logger) Warnf(format string, args ...any) {
	z := current()
	if e := z.Warn(); e.Enabled() {
		l.emit(zerolog.WarnLevel, fmt.Sprintf(format, args...), nil)
	}
}
func (l *Logger) Errorf(format string, args ...any) {
	z := current()
	if e := z.Error(); e.Enabled() {
		l.emit(zerolog.ErrorLevel, fmt.Sprintf(format, args...), nil)
	}
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
//
// Fatal and Panic must ALWAYS terminate, even when their level is disabled:
// if the event isn't enabled we still exit/panic so the process cannot
// silently continue past a fatal condition.
func (l *Logger) emit(level zerolog.Level, msg string, kvs []any) {
	z := current()
	var e *zerolog.Event
	switch level {
	case zerolog.TraceLevel:
		e = z.Trace()
	case zerolog.DebugLevel:
		e = z.Debug()
	case zerolog.InfoLevel:
		e = z.Info()
	case zerolog.WarnLevel:
		e = z.Warn()
	case zerolog.ErrorLevel:
		e = z.Error()
	case zerolog.FatalLevel:
		e = z.Fatal()
	case zerolog.PanicLevel:
		e = z.Panic()
	default:
		e = z.Log()
	}

	if !e.Enabled() {
		switch level {
		case zerolog.FatalLevel:
			fatalExitFunc(1)
		case zerolog.PanicLevel:
			panic(msg)
		}
		return
	}

	all := kvs
	if len(l.kvs) > 0 {
		all = make([]any, 0, len(l.kvs)+len(kvs))
		all = append(all, l.kvs...)
		all = append(all, kvs...)
	}
	if len(all) > 0 {
		addFields(e, all...)
	}
	e.Msg(msg)
}

// addFields appends alternating key/value pairs to the event. Common types
// are handled without reflection; anything else falls back to Interface.
func addFields(e *zerolog.Event, kvs ...any) {
	for i := 0; i < len(kvs); i += 2 {
		if i+1 >= len(kvs) {
			// trailing key with no value
			e.Str(fmt.Sprintf("%v", kvs[i]), "<missing>")
			continue
		}
		key, ok := kvs[i].(string)
		if !ok {
			key = fmt.Sprintf("%v", kvs[i])
		}
		setField(e, key, kvs[i+1])
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
		} else if key == "error" || key == "err" {
			e.Err(val)
		} else {
			e.Str(key, val.Error())
		}
	case nil:
		e.Str(key, "<nil>")
	default:
		e.Interface(key, val)
	}
}
