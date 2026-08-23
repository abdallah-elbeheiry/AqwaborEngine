package logx

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func capture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := cfg.output
	mu.Lock()
	c := defaultConfig()
	c.output = &buf
	c.color = false
	applyConfig(c)
	mu.Unlock()
	defer func() {
		mu.Lock()
		cfg.output = prev
		applyConfig(cfg)
		mu.Unlock()
	}()
	fn()
	return buf.String()
}

func TestLevelsAndFields(t *testing.T) {
	out := capture(t, func() {
		SetLevel(zerolog.DebugLevel)
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
		SetLevel(zerolog.WarnLevel)
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
		SetLevel(zerolog.DebugLevel)
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
		SetLevel(zerolog.DebugLevel)
		Info("odd", "onlykey")
	})
	if !strings.Contains(out, "onlykey") {
		t.Fatalf("odd key not handled: %q", out)
	}
}

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
	mu.Lock()
	c := defaultConfig()
	c.output = &buf
	c.color = false
	applyConfig(c)
	mu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("msg", "k", i, "s", "val")
	}
}
