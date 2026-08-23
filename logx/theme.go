package logx

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// 256-color ANSI codes leaning purple/blue, while keeping severity order
// readable: blue (debug) -> light blue (info) -> purple (warn) -> red
// (error/fatal/panic). Field names are blue, values off-white, timestamps a
// soft purple, and the message a lavender so the whole stream reads cool.
const (
	cReset    = "\x1b[0m"
	cDebug    = "38;5;111" // blue
	cInfo     = "38;5;117" // light blue / cyan
	cWarn     = "38;5;141" // purple
	cError    = "38;5;203" // red
	cFatal    = "38;5;196" // bright red
	cPanic    = "38;5;201" // magenta
	cTime     = "38;5;147" // soft purple
	cFieldKey = "38;5;111" // blue
	cFieldVal = "38;5;253" // off-white
	cMessage  = "38;5;183" // lavender
	cDim      = "38;5;240" // muted grey for separators
)

func wrap(code, s string) string {
	if code == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + cReset
}

func levelColor(lvl string) string {
	switch strings.ToLower(lvl) {
	case "debug":
		return cDebug
	case "info":
		return cInfo
	case "warn", "warning":
		return cWarn
	case "error":
		return cError
	case "fatal":
		return cFatal
	case "panic":
		return cPanic
	default:
		return cFieldVal
	}
}

func padLevel(s string) string {
	if len(s) >= 5 {
		return s
	}
	return s + strings.Repeat(" ", 5-len(s))
}

// themedConsoleWriter returns a ConsoleWriter painted with the purple/blue
// palette. When color is false the same layout is used but without ANSI codes.
func themedConsoleWriter(color bool, timeFormat string) zerolog.ConsoleWriter {
	cc := ""
	if color {
		cc = cMessage
	}
	w := zerolog.ConsoleWriter{
		NoColor:    true, // we apply our own colors below
		TimeFormat: timeFormat,
		FormatTimestamp: func(i any) string {
			s, ok := i.(string)
			if !ok {
				if p, ok := i.(*string); ok && p != nil {
					s = *p
				}
			}
			return wrap(ternary(color, cTime), s)
		},
		FormatLevel: func(i any) string {
			var raw string
			switch v := i.(type) {
			case string:
				raw = v
			case *string:
				if v != nil {
					raw = *v
				}
			}
			up := strings.ToUpper(raw)
			return wrap(ternary(color, levelColor(raw)), padLevel(up))
		},
		FormatMessage: func(i any) string {
			return wrap(cc, toString(i))
		},
		FormatFieldName: func(i any) string {
			return wrap(ternary(color, cFieldKey), toString(i)) + wrap(ternary(color, cDim), "=")
		},
		FormatFieldValue: func(i any) string {
			return wrap(ternary(color, cFieldVal), toString(i))
		},
		FormatErrFieldName: func(i any) string {
			return wrap(ternary(color, cError), toString(i)) + wrap(ternary(color, cDim), "=")
		},
		FormatErrFieldValue: func(i any) string {
			return wrap(ternary(color, cError), toString(i))
		},
	}
	return w
}

func ternary(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

// toString converts a field value (which zerolog passes as its decoded type:
// string, []byte, json.Number, bool, float64, etc.) into a display string.
// zerolog hands non-string fields to the console formatter as raw []byte
// tokens, so handle that explicitly.
func toString(i any) string {
	switch v := i.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case *string:
		if v != nil {
			return *v
		}
		return ""
	case nil:
		return "<nil>"
	default:
		return fmt.Sprintf("%v", v)
	}
}
