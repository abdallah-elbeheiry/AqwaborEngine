package schedulers

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_BasicRun(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) {
		count.Add(1)
	}, 100.0)

	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if count.Load() < 4 || count.Load() > 6 {
		t.Fatalf("expected ~5 runs at 100Hz in 50ms, got %d", count.Load())
	}
}

func TestScheduler_MultipleRates(t *testing.T) {
	s := NewScheduler()
	var count100, count10 atomic.Int64

	s.Run(func(st TickState) { count100.Add(1) }, 100.0)
	s.Run(func(st TickState) { count10.Add(1) }, 10.0)

	s.Start()
	time.Sleep(110 * time.Millisecond)
	s.Stop()

	c100 := count100.Load()
	c10 := count10.Load()

	if c100 < 9 || c100 > 12 {
		t.Fatalf("100Hz: expected ~11 runs in 110ms, got %d", c100)
	}
	if c10 < 0 || c10 > 2 {
		t.Fatalf("10Hz: expected ~1 run in 110ms, got %d", c10)
	}
}

func TestScheduler_PauseResume(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	s.Start()
	time.Sleep(30 * time.Millisecond)
	s.Pause()
	pausedCount := count.Load()
	time.Sleep(30 * time.Millisecond)
	s.Resume()
	time.Sleep(30 * time.Millisecond)
	s.Stop()

	if pausedCount == count.Load() {
		t.Fatal("count should have increased after resume")
	}
	if pausedCount < 2 || pausedCount > 4 {
		t.Fatalf("expected ~3 runs before pause, got %d", pausedCount)
	}
}

func TestScheduler_SetSpeed(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	s.SetSpeed(2.0)
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if count.Load() < 8 || count.Load() > 12 {
		t.Fatalf("expected ~10 runs at 2x speed (100Hz) in 50ms, got %d", count.Load())
	}
}

func TestScheduler_SpeedZeroPauses(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	s.SetSpeed(0.0)
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if count.Load() != 0 {
		t.Fatalf("expected 0 runs at speed 0, got %d", count.Load())
	}
}

func TestScheduler_TickState(t *testing.T) {
	s := NewScheduler()
	var lastTick uint64
	var lastDelta float64

	s.Run(func(st TickState) {
		lastTick = st.Tick
		lastDelta = st.DeltaTime
	}, 50.0)

	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	if lastTick < 4 || lastTick > 7 {
		t.Fatalf("expected tick 4-7, got %d", lastTick)
	}
	if lastDelta < 0.019 || lastDelta > 0.021 {
		t.Fatalf("expected DeltaTime ~0.02, got %f", lastDelta)
	}
}

func TestScheduler_StopBeforeStart(t *testing.T) {
	s := NewScheduler()
	s.Stop()
}

func TestScheduler_DoubleStart(t *testing.T) {
	s := NewScheduler()
	s.Start()
	s.Start()
	s.Stop()
}

func TestScheduler_RunAfterStart(t *testing.T) {
	s := NewScheduler()
	s.Start()
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	time.Sleep(30 * time.Millisecond)
	s.Stop()

	if count.Load() < 2 || count.Load() > 4 {
		t.Fatalf("expected ~3 runs, got %d", count.Load())
	}
}
