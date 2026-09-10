package schedulers

import (
	"sync"
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
		lastDelta = st.Delta()
	}, 50.0)

	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	if lastTick < 4 || lastTick > 7 {
		t.Fatalf("expected tick 4-7, got %d", lastTick)
	}
	if lastDelta < 0.019 || lastDelta > 0.021 {
		t.Fatalf("expected Delta ~0.02, got %f", lastDelta)
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

// A tick that overruns its budget must not make the next wake run more ticks.
// Without a bound the accumulator grows faster than it drains, and the rate
// runs away rather than falling behind at a steady distance.
func TestScheduler_BoundedCatchUp(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(2)

	var runs atomic.Int64
	// Each tick takes far longer than the interval it is scheduled at, so the
	// group is always behind.
	s.Run(func(TickState) {
		runs.Add(1)
		time.Sleep(2 * time.Millisecond)
	}, 500.0)

	s.Start()
	time.Sleep(200 * time.Millisecond)
	s.Stop()

	// Unbounded, this would have run every tick it owed, roughly 100 of them,
	// and each wake would owe more than the last. Bounded, it runs what it can.
	got := runs.Load()
	if got == 0 {
		t.Fatal("the bounded group never ran")
	}
	if s.Dropped() == 0 {
		t.Fatal("a group that cannot keep up dropped nothing, so it is still carrying the debt")
	}
	t.Logf("ran %d ticks, dropped %d", got, s.Dropped())
}

// Two runs of the same registrations must tick their rates in the same order.
// Iterating a Go map varied it per wake.
func TestScheduler_StableRateOrder(t *testing.T) {
	record := func() []float64 {
		s := NewScheduler()
		var mu sync.Mutex
		var order []float64
		for _, hz := range []float64{30, 120, 60, 5} {
			hz := hz
			s.Run(func(TickState) {
				mu.Lock()
				if len(order) < 16 {
					order = append(order, hz)
				}
				mu.Unlock()
			}, hz)
		}
		s.Start()
		time.Sleep(80 * time.Millisecond)
		s.Stop()
		mu.Lock()
		defer mu.Unlock()
		return append([]float64(nil), order...)
	}

	a, b := record(), record()
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("nothing ran")
	}
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			t.Fatalf("run order differs at %d: %v then %v", i, a[:n], b[:n])
		}
	}
}

// Registering while the scheduler is running must not race with the goroutine
// reading the group's functions.
func TestScheduler_RegisterWhileRunning(t *testing.T) {
	s := NewScheduler()
	var runs atomic.Int64
	s.Run(func(TickState) { runs.Add(1) }, 200.0)
	s.Start()

	for range 50 {
		s.Run(func(TickState) { runs.Add(1) }, 200.0)
		time.Sleep(time.Millisecond)
	}
	s.Stop()

	if runs.Load() == 0 {
		t.Fatal("nothing ran")
	}
}

func TestScheduler_TickCountIsPrimary(t *testing.T) {
	s := NewScheduler()
	var seen []uint64
	var mu sync.Mutex
	s.Run(func(st TickState) {
		mu.Lock()
		if len(seen) < 5 {
			seen = append(seen, st.Tick)
		}
		mu.Unlock()
		if st.Hz != 100 {
			t.Errorf("Hz = %v, want 100", st.Hz)
		}
	}, 100.0)

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	for i, tick := range seen {
		if tick != uint64(i) {
			t.Fatalf("tick %d of the run was %d, want %d", i, tick, i)
		}
	}
}
