package logx

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// capture runs fn with a captured, colorless, debug-level logger.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	Init(WithOutput(&buf), WithColor(false), WithLevel(DebugLevel))
	defer Init(WithColor(false), WithLevel(InfoLevel))
	fn()
	return buf.String()
}

func TestLevelsAndFields(t *testing.T) {
	out := capture(t, func() {
		Info("started", "component", "window", "w", 1280, "h", 720, "ok", true)
		Debug("debug line", "count", int64(3), "ratio", 0.5, "d", int(10))
		Warn("low mem", "mb", uint(12))
		Errorf("boom: %s", "fail")
	})
	if !strings.Contains(out, "started") || !strings.Contains(out, "window") {
		t.Fatalf("info output missing: %q", out)
	}
	if !strings.Contains(out, "debug line") {
		t.Fatalf("debug output missing (level filter?): %q", out)
	}
	if !strings.Contains(out, "low mem") || !strings.Contains(out, "boom") {
		t.Fatalf("warn/error missing: %q", out)
	}
}

func TestDisabledLevelNoEmit(t *testing.T) {
	out := capture(t, func() {
		SetLevel(WarnLevel)
		Debug("should not appear", "k", 1)
		Info("also hidden", "k", 2)
		Warn("visible", "k", 3)
	})
	if strings.Contains(out, "should not appear") || strings.Contains(out, "also hidden") {
		t.Fatalf("disabled levels leaked: %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("enabled level missing: %q", out)
	}
}

func TestWithContext(t *testing.T) {
	out := capture(t, func() {
		l := With("component", "scheduler")
		l.Info("tick", "hz", 60)
		l.With("window", "main").Warn("slow frame", "ms", 22)
	})
	if !strings.Contains(out, "component") || !strings.Contains(out, "scheduler") {
		t.Fatalf("With context missing: %q", out)
	}
	if !strings.Contains(out, "window") || !strings.Contains(out, "main") {
		t.Fatalf("nested With context missing: %q", out)
	}
}

func TestOddKeyValue(t *testing.T) {
	out := capture(t, func() {
		Info("odd", "onlykey")
	})
	if !strings.Contains(out, "onlykey") {
		t.Fatalf("odd key not handled: %q", out)
	}
}

func TestErrField(t *testing.T) {
	out := capture(t, func() {
		Error("bad", "err", io.EOF)
	})
	if !strings.Contains(out, "EOF") {
		t.Fatalf("error field missing: %q", out)
	}
}

// Policy: explicit WithLevel wins over AQWABOR_LOG; env only a default.
func TestEnvLevelPolicy(t *testing.T) {
	t.Setenv("AQWABOR_LOG", "warn")

	// No WithLevel -> env (warn) is the effective level.
	Init(WithColor(false))
	if got := Level(); got != WarnLevel {
		t.Fatalf("env default: got %v want warn", got)
	}

	// Explicit WithLevel must win over env.
	Init(WithColor(false), WithLevel(DebugLevel))
	if got := Level(); got != DebugLevel {
		t.Fatalf("explicit level: got %v want debug (env must not override)", got)
	}
}

// Fatal must terminate even when the level is Disabled.
func TestFatalTerminatesWhenDisabled(t *testing.T) {
	var exited int
	old := fatalExitFunc
	fatalExitFunc = func(code int) { exited = code }
	defer func() { fatalExitFunc = old }()

	Init(WithColor(false), WithLevel(Disabled))
	Fatal("must exit")
	if exited != 1 {
		t.Fatalf("Fatal did not terminate under Disabled level (exited=%d)", exited)
	}
}

// Panic must panic even when the level is Disabled.
func TestPanicTerminatesWhenDisabled(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Panic did not panic under Disabled level")
		}
	}()
	Init(WithColor(false), WithLevel(Disabled))
	Panic("must panic")
}

func TestConcurrentLoggingAndReconfig(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for j := range 200 {
				Info("concurrent", "i", j)
				Debug("concurrent debug", "i", j)
				With("g", 1).Warn("nested", "i", j)
			}
		})
	}
	stop := make(chan struct{})
	go func() {
		// Reconfigure against concurrency-safe writers (os.Stderr /
		// io.Discard). A shared bytes.Buffer is NOT safe for concurrent
		// writes, so it must not be used here.
		for {
			select {
			case <-stop:
				return
			default:
				SetLevel(DebugLevel)
				SetLevel(WarnLevel)
				counter++
				if counter%2 == 0 {
					SetOutput(io.Discard)
				} else {
					SetOutput(os.Stderr)
				}
			}
		}
	}()
	wg.Wait()
	close(stop)
}

var counter int

func BenchmarkInfoDisabled(b *testing.B) {
	Discard()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("noop", "k", i)
	}
}

func BenchmarkInfoEnabled(b *testing.B) {
	var buf bytes.Buffer
	Init(WithOutput(&buf), WithColor(false), WithLevel(DebugLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("msg", "k", i, "s", "val")
	}
	_ = zerolog.DebugLevel
}
