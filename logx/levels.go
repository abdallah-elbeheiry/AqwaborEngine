package logx

import "github.com/rs/zerolog"

// Level values are re-exported from zerolog (as constants) so callers can tune
// verbosity without importing zerolog directly.
const (
	DebugLevel = zerolog.DebugLevel
	InfoLevel  = zerolog.InfoLevel
	WarnLevel  = zerolog.WarnLevel
	ErrorLevel = zerolog.ErrorLevel
	FatalLevel = zerolog.FatalLevel
	PanicLevel = zerolog.PanicLevel
	TraceLevel = zerolog.TraceLevel
	NoLevel    = zerolog.NoLevel
	Disabled   = zerolog.Disabled
)
