package sound

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// makeWAV builds a minimal valid 16-bit PCM mono WAV with the given number of
// samples, for use as test audio without any external asset.
func makeWAV(samples int) []byte {
	const sampleRate = 44100
	dataLen := samples * 2
	buf := new(bytes.Buffer)
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))            // block align
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))           // bits/sample
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataLen))
	for i := range samples {
		v := int16(i%2000 - 1000)
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func TestEffectiveVolumeFormula(t *testing.T) {
	cases := []struct {
		master, clip, player, want float64
	}{
		{1, 1, 1, 1},
		{0, 1, 1, 0},
		{1, 0.5, 1, 0.5},
		{1, 1, 0.25, 0.25},
		{0.5, 0.5, 0.5, 0.125},
		{2, 0.5, 1, 0.5},   // master clamped to 1
		{-1, 1, 1, 0},      // clamped to 0
		{0.5, -2, 2, 0},    // all clamped -> 0
		{1, 1.5, 0.5, 0.5}, // clip clamped to 1
	}
	for _, c := range cases {
		if got := computeEffective(c.master, c.clip, c.player); got != c.want {
			t.Errorf("computeEffective(%v,%v,%v)=%v want %v", c.master, c.clip, c.player, got, c.want)
		}
	}
}

func TestVolumeAppliedToBackend(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	clip, err := ctx.LoadAudio(makeWAV(1024))
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}

	p, err := clip.Play()
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	defer p.Stop()

	if got := p.EffectiveVolume(); got != 1 {
		t.Fatalf("default backend volume = %v, want 1", got)
	}

	// Master 0 => silence.
	ctx.SetMasterVolume(0)
	if got := p.EffectiveVolume(); got != 0 {
		t.Fatalf("after master=0 backend volume = %v, want 0", got)
	}

	// clip 0.5 * master 1 * player 1 = 0.5
	ctx.SetMasterVolume(1)
	clip.SetVolume(0.5)
	if got := p.EffectiveVolume(); got != 0.5 {
		t.Fatalf("after clip=0.5 backend volume = %v, want 0.5", got)
	}

	// player 0.5 * clip 0.5 * master 1 = 0.25
	p.SetVolume(0.5)
	if got := p.EffectiveVolume(); got != 0.25 {
		t.Fatalf("after player=0.5 backend volume = %v, want 0.25", got)
	}
}

func TestCacheBytes(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	data := makeWAV(2048)
	a, err := ctx.LoadAudio(data)
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}
	b, err := ctx.LoadAudio(data)
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}
	if a != b {
		t.Fatal("same bytes returned different *Clip pointers")
	}

	c, err := ctx.LoadAudio(makeWAV(2049))
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}
	if a == c {
		t.Fatal("different bytes returned the same *Clip pointer")
	}
}

func TestCacheFile(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.wav")
	if err := os.WriteFile(p1, makeWAV(1024), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := ctx.LoadAudioFile(p1)
	if err != nil {
		t.Fatalf("LoadAudioFile: %v", err)
	}
	b, err := ctx.LoadAudioFile(p1)
	if err != nil {
		t.Fatalf("LoadAudioFile: %v", err)
	}
	if a != b {
		t.Fatal("same path returned different *Clip pointers")
	}

	// Normalised-equivalent path should also hit the cache.
	b2, err := ctx.LoadAudioFile(filepath.Join(dir, "..", filepath.Base(dir), "a.wav"))
	if err != nil {
		t.Fatalf("LoadAudioFile(rel): %v", err)
	}
	if a != b2 {
		t.Fatal("equivalent path returned different *Clip pointers")
	}

	p2 := filepath.Join(dir, "b.wav")
	if err := os.WriteFile(p2, makeWAV(1024), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ctx.LoadAudioFile(p2)
	if err != nil {
		t.Fatalf("LoadAudioFile: %v", err)
	}
	if a == c {
		t.Fatal("different path returned same *Clip pointer")
	}
}

func TestUnsupportedFormat(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	garbage := []byte("this is not audio at all, really")
	if _, err := ctx.LoadAudio(garbage); err == nil {
		t.Fatal("expected error for unsupported format")
	} else if !contains(err.Error(), ErrUnsupportedFormat.Error()) {
		t.Fatalf("error %q does not mention %q", err, ErrUnsupportedFormat)
	}
}

func TestCloseIdempotentAndClosedFails(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ctx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Double close must not panic.
	if err := ctx.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if _, err := ctx.LoadAudio(makeWAV(64)); err != ErrClosed {
		t.Fatalf("LoadAudio after close = %v, want ErrClosed", err)
	}
}

func TestOverlappingPlay(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	clip, err := ctx.LoadAudio(makeWAV(4096))
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}

	const n = 8
	players := make([]*Player, n)
	for i := range n {
		p, err := clip.Play()
		if err != nil {
			t.Fatalf("Play %d: %v", i, err)
		}
		players[i] = p
	}
	if len(ctx.players) != n {
		t.Fatalf("active players = %d, want %d", len(ctx.players), n)
	}
	for _, p := range players {
		p.Stop()
	}
	if len(ctx.players) != 0 {
		t.Fatalf("active players after stop = %d, want 0", len(ctx.players))
	}
}

func TestStressNoLeak(t *testing.T) {
	ctx, err := New(WithSilent(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()

	clip, err := ctx.LoadAudio(makeWAV(1024))
	if err != nil {
		t.Fatalf("LoadAudio: %v", err)
	}

	goroutinesStart := runtime.NumGoroutine()

	const iterations = 500
	for i := range iterations {
		p, err := clip.Play()
		if err != nil {
			t.Fatalf("Play %d: %v", i, err)
		}
		p.SetVolume(0.7)
		if i%3 == 0 {
			p.Pause()
			p.Resume()
		}
		p.Stop()
	}

	if len(ctx.players) != 0 {
		t.Fatalf("active players after stress = %d, want 0 (leak)", len(ctx.players))
	}
	if len(clip.players) != 0 {
		t.Fatalf("clip active players after stress = %d, want 0 (leak)", len(clip.players))
	}

	// Allow any pending cleanup; goroutine count should be stable.
	runtime.GC()
	if g := runtime.NumGoroutine(); g > goroutinesStart+2 {
		t.Fatalf("goroutines grew from %d to %d (possible leak)", goroutinesStart, g)
	}
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
