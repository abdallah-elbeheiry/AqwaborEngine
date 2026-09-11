package schedulers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Tests using Advance (pure core, no wall clock) ---

func TestAdvance_Basic(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) {
		count.Add(1)
	}, 100.0)

	// 100Hz: interval=10ms, next starts at 0.
	// Advance 50ms fires ticks at simTime 0,10,20,30,40,50 → 6 ticks.
	result := s.Advance(50 * time.Millisecond)

	if result.Fired != 6 {
		t.Fatalf("expected 6 ticks fired, got %d", result.Fired)
	}
	if count.Load() != 6 {
		t.Fatalf("expected count 6, got %d", count.Load())
	}
}

func TestAdvance_MultipleRates(t *testing.T) {
	s := NewScheduler()
	var count100, count10 atomic.Int64

	s.Run(func(st TickState) { count100.Add(1) }, 100.0)
	s.Run(func(st TickState) { count10.Add(1) }, 10.0)

	// 50ms: 100Hz fires 6 times (next=0→50ms), 10Hz fires 1 time (next=0, then 100>50).
	result := s.Advance(50 * time.Millisecond)

	if result.Fired != 7 {
		t.Fatalf("expected 7 ticks total, got %d", result.Fired)
	}
	if count100.Load() != 6 {
		t.Fatalf("expected 6 at 100Hz, got %d", count100.Load())
	}
	if count10.Load() != 1 {
		t.Fatalf("expected 1 at 10Hz, got %d", count10.Load())
	}
}

func TestAdvance_MultipleRatesAllFire(t *testing.T) {
	s := NewScheduler()
	var count100, count10 atomic.Int64

	s.Run(func(st TickState) { count100.Add(1) }, 100.0)
	s.Run(func(st TickState) { count10.Add(1) }, 10.0)

	// 100Hz: interval=10ms, maxCatchUp=8 → 8 ticks (next=0..70ms).
	// 10Hz: interval=100ms → 2 ticks (next=0,100 ≤ 100ms).
	result := s.Advance(100 * time.Millisecond)

	if count100.Load() != 8 {
		t.Fatalf("expected 8 at 100Hz, got %d", count100.Load())
	}
	if count10.Load() != 2 {
		t.Fatalf("expected 2 at 10Hz, got %d", count10.Load())
	}
	if result.Fired != 10 {
		t.Fatalf("expected 10 total, got %d", result.Fired)
	}
}

func TestAdvance_Order(t *testing.T) {
	s := NewScheduler()
	var order []int
	var mu sync.Mutex

	s.Run(func(st TickState) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	}, 100.0)

	s.Run(func(st TickState) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	}, 50.0)

	s.Advance(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(order) == 0 {
		t.Fatal("nothing ran")
	}
	// 50Hz < 100Hz, so 50Hz group runs first, then 100Hz group.
	// All 50Hz entries should come before all 100Hz entries.
	saw50 := false
	for _, r := range order {
		if r == 2 {
			if saw50 {
				// Already saw 50Hz, this is fine
			}
			saw50 = true
		}
		if r == 1 {
			if !saw50 {
				t.Fatal("100Hz ran before 50Hz")
			}
		}
	}
}

func TestAdvance_SpeedZero(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	s.SetSpeed(0.0)

	result := s.Advance(100 * time.Millisecond)

	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks at speed 0, got %d", result.Fired)
	}
	if count.Load() != 0 {
		t.Fatalf("expected count 0, got %d", count.Load())
	}
}

func TestAdvance_Paused(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 100.0)
	s.Pause()

	result := s.Advance(100 * time.Millisecond)

	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks when paused, got %d", result.Fired)
	}
	if count.Load() != 0 {
		t.Fatalf("expected count 0 when paused, got %d", count.Load())
	}
}

func TestAdvance_TickState(t *testing.T) {
	s := NewScheduler()
	var ticks []uint64
	var mu sync.Mutex

	s.Run(func(st TickState) {
		mu.Lock()
		ticks = append(ticks, st.Tick)
		mu.Unlock()
	}, 50.0)

	// 50Hz: interval=20ms. Advance 100ms → ticks at 0,20,40,60,80,100 → 6 ticks.
	s.Advance(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	expected := []uint64{0, 1, 2, 3, 4, 5}
	if len(ticks) != len(expected) {
		t.Fatalf("expected %d ticks, got %d", len(expected), len(ticks))
	}
	for i, expectedTick := range expected {
		if ticks[i] != expectedTick {
			t.Fatalf("tick %d: expected %d, got %d", i, expectedTick, ticks[i])
		}
	}
}

func TestAdvance_Delta(t *testing.T) {
	s := NewScheduler()
	var delta float64

	s.Run(func(st TickState) { delta = st.Delta() }, 50.0)
	s.Advance(100 * time.Millisecond)

	if delta < 0.019 || delta > 0.021 {
		t.Fatalf("expected Delta ~0.02, got %f", delta)
	}
}

func TestAdvance_BoundedCatchUp(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(2)

	var runs atomic.Int64
	s.Run(func(TickState) {
		runs.Add(1)
	}, 500.0)

	// 500Hz: interval=2ms. Advance 100ms would need 50 ticks.
	// With maxCatchUp=2, only 2 fire and the rest are dropped.
	result := s.Advance(100 * time.Millisecond)

	if result.Fired != 2 {
		t.Fatalf("expected 2 ticks fired with catch-up bound, got %d", result.Fired)
	}
	if result.Dropped == 0 {
		t.Fatal("expected some dropped ticks with catch-up bound")
	}
	if runs.Load() != 2 {
		t.Fatalf("expected 2 runs, got %d", runs.Load())
	}
}

func TestAdvance_DroppedCount(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(2)

	s.Run(func(TickState) {}, 500.0)

	result := s.Advance(100 * time.Millisecond)
	if result.Dropped == 0 {
		t.Fatal("expected dropped ticks")
	}
	if result.Fired != 2 {
		t.Fatalf("expected 2 fired, got %d", result.Fired)
	}
}

func TestAdvance_TicksPerRate(t *testing.T) {
	s := NewScheduler()

	s.Run(func(TickState) {}, 100.0)
	s.Run(func(TickState) {}, 50.0)

	s.Advance(200 * time.Millisecond)

	// maxCatchUp=8 limits each group to 8 ticks per Advance.
	if t100 := s.Ticks(100.0); t100 != 8 {
		t.Fatalf("expected 8 ticks at 100Hz, got %d", t100)
	}
	if t50 := s.Ticks(50.0); t50 != 8 {
		t.Fatalf("expected 8 ticks at 50Hz, got %d", t50)
	}
}

func TestAdvance_SimTimeAccumulates(t *testing.T) {
	s := NewScheduler()
	var count int32

	s.Run(func(TickState) { count++ }, 10.0) // 100ms interval

	// Advance 50ms: simTime=50ms, next=0 → tick, next=100 > 50 → stop. 1 tick.
	s.Advance(50 * time.Millisecond)
	if count != 1 {
		t.Fatalf("after first advance: expected 1, got %d", count)
	}
	// Advance 50ms: simTime=100ms, next=100 → tick, next=200 > 100 → stop. 1 tick.
	s.Advance(50 * time.Millisecond)
	if count != 2 {
		t.Fatalf("after second advance: expected 2, got %d", count)
	}
	// Advance 50ms: simTime=150ms, next=200 > 150 → stop. 0 ticks.
	s.Advance(50 * time.Millisecond)
	if count != 2 {
		t.Fatalf("after third advance: expected 2, got %d", count)
	}
}

// --- Determinism tests ---

func TestAdvance_Deterministic(t *testing.T) {
	s1 := NewScheduler()
	s2 := NewScheduler()

	s1.Run(func(st TickState) { _ = st.Tick }, 100.0)
	s1.Run(func(st TickState) { _ = st.Tick }, 50.0)
	s2.Run(func(st TickState) { _ = st.Tick }, 100.0)
	s2.Run(func(st TickState) { _ = st.Tick }, 50.0)

	// Same sequence of Advance calls.
	seq := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 5 * time.Millisecond, 100 * time.Millisecond}

	var ticks1, ticks2 []uint64
	var mu1, mu2 sync.Mutex

	s1.Run(func(st TickState) {
		mu1.Lock()
		ticks1 = append(ticks1, st.Tick)
		mu1.Unlock()
	}, 100.0)
	s1.Run(func(st TickState) {
		mu1.Lock()
		ticks1 = append(ticks1, st.Tick+1000) // offset to distinguish from s2
		mu1.Unlock()
	}, 50.0)

	s2.Run(func(st TickState) {
		mu2.Lock()
		ticks2 = append(ticks2, st.Tick)
		mu2.Unlock()
	}, 100.0)
	s2.Run(func(st TickState) {
		mu2.Lock()
		ticks2 = append(ticks2, st.Tick+1000)
		mu2.Unlock()
	}, 50.0)

	for _, dt := range seq {
		s1.Advance(dt)
		s2.Advance(dt)
	}

	if len(ticks1) != len(ticks2) {
		t.Fatalf("determinism failed: tick counts differ %d vs %d", len(ticks1), len(ticks2))
	}
}

func TestAdvance_Replay(t *testing.T) {
	// Record a sequence of commands.
	type command struct {
		simDT  time.Duration
		speed  float64
		paused bool
	}

	commands := []command{
		{simDT: 10 * time.Millisecond, speed: 1.0},
		{simDT: 20 * time.Millisecond, speed: 1.0},
		{simDT: 5 * time.Millisecond, speed: 2.0},
		{simDT: 100 * time.Millisecond, speed: 1.0},
		{simDT: 50 * time.Millisecond, speed: 0.5},
	}

	// Run 1.
	s1 := NewScheduler()
	var counts1 []uint64
	var mu1 sync.Mutex
	s1.Run(func(st TickState) {
		mu1.Lock()
		counts1 = append(counts1, st.Tick)
		mu1.Unlock()
	}, 100.0)

	for _, cmd := range commands {
		s1.SetSpeed(cmd.speed)
		if cmd.paused {
			s1.Pause()
		} else {
			s1.Resume()
		}
		s1.Advance(cmd.simDT)
	}

	// Run 2 (replay).
	s2 := NewScheduler()
	var counts2 []uint64
	var mu2 sync.Mutex
	s2.Run(func(st TickState) {
		mu2.Lock()
		counts2 = append(counts2, st.Tick)
		mu2.Unlock()
	}, 100.0)

	for _, cmd := range commands {
		s2.SetSpeed(cmd.speed)
		if cmd.paused {
			s2.Pause()
		} else {
			s2.Resume()
		}
		s2.Advance(cmd.simDT)
	}

	if len(counts1) != len(counts2) {
		t.Fatalf("replay failed: tick counts differ %d vs %d", len(counts1), len(counts2))
	}
	for i := range counts1 {
		if counts1[i] != counts2[i] {
			t.Fatalf("replay failed at tick %d: %d vs %d", i, counts1[i], counts2[i])
		}
	}
}

func TestAdvance_BitIdenticalReplays(t *testing.T) {
	// Run the same command sequence multiple times and verify bit-identical results.
	commands := []struct {
		simDT time.Duration
		speed float64
	}{
		{10 * time.Millisecond, 1.0},
		{20 * time.Millisecond, 2.0},
		{5 * time.Millisecond, 0.5},
		{100 * time.Millisecond, 1.0},
		{50 * time.Millisecond, 3.0},
	}

	var firstResult []uint64
	for run := 0; run < 3; run++ {
		s := NewScheduler()
		var ticks []uint64
		var mu sync.Mutex
		s.Run(func(st TickState) {
			mu.Lock()
			ticks = append(ticks, st.Tick)
			mu.Unlock()
		}, 100.0)

		for _, cmd := range commands {
			s.SetSpeed(cmd.speed)
			s.Advance(cmd.simDT)
		}

		if firstResult == nil {
			firstResult = ticks
		} else {
			if len(ticks) != len(firstResult) {
				t.Fatalf("run %d: tick count differs: %d vs %d", run, len(ticks), len(firstResult))
			}
			for i := range ticks {
				if ticks[i] != firstResult[i] {
					t.Fatalf("run %d: tick %d differs: %d vs %d", run, i, ticks[i], firstResult[i])
				}
			}
		}
	}
}

func TestAdvance_OrderStableAcrossRuns(t *testing.T) {
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
		s.Advance(100 * time.Millisecond)
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

// --- Integration tests using Start/Stop (pacer) ---

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

func TestScheduler_BoundedCatchUp(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(2)

	var runs atomic.Int64
	s.Run(func(TickState) {
		runs.Add(1)
		time.Sleep(2 * time.Millisecond)
	}, 500.0)

	s.Start()
	time.Sleep(200 * time.Millisecond)
	s.Stop()

	got := runs.Load()
	if got == 0 {
		t.Fatal("the bounded group never ran")
	}
	if s.Dropped() == 0 {
		t.Fatal("a group that cannot keep up dropped nothing, so it is still carrying the debt")
	}
	t.Logf("ran %d ticks, dropped %d", got, s.Dropped())
}

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

// --- Edge cases ---

func TestAdvance_ZeroSimDT(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 100.0)

	result := s.Advance(0)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for Advance(0), got %d", result.Fired)
	}
	if count.Load() != 0 {
		t.Fatalf("expected count 0, got %d", count.Load())
	}
}

func TestAdvance_NegativeSimDT(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 100.0)

	result := s.Advance(-10 * time.Millisecond)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for negative simDT, got %d", result.Fired)
	}
}

func TestAdvance_MultipleAdvances(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 10.0) // 100ms interval

	// 10Hz: interval=100ms. next starts at 0.
	// Advance(50ms): simTime=50ms, next=0 → tick, next=100 > 50 → stop. 1 tick.
	s.Advance(50 * time.Millisecond)
	// Advance(50ms): simTime=100ms, next=100 → tick, next=200 > 100 → stop. 1 tick.
	s.Advance(50 * time.Millisecond)
	// Advance(50ms): simTime=150ms, next=200 > 150 → stop. 0 ticks.
	s.Advance(50 * time.Millisecond)

	if count.Load() != 2 {
		t.Fatalf("expected 2 ticks after three 50ms advances, got %d", count.Load())
	}
}

func TestAdvance_SortedRateOrder(t *testing.T) {
	s := NewScheduler()
	var order []int
	var mu sync.Mutex

	for _, hz := range []float64{200, 50, 100} {
		hz := hz
		s.Run(func(TickState) {
			mu.Lock()
			order = append(order, int(hz))
			mu.Unlock()
		}, hz)
	}

	s.Advance(100 * time.Millisecond)

	// Groups should run in ascending hz order: 50, 100, 200.
	first50, first100, first200 := -1, -1, -1
	for i, hz := range order {
		if hz == 50 && first50 == -1 {
			first50 = i
		}
		if hz == 100 && first100 == -1 {
			first100 = i
		}
		if hz == 200 && first200 == -1 {
			first200 = i
		}
	}

	if first50 == -1 || first100 == -1 || first200 == -1 {
		t.Fatal("not all rates fired")
	}
	if first50 > first100 || first100 > first200 {
		t.Fatalf("rates did not run in ascending order: 50@%d, 100@%d, 200@%d", first50, first100, first200)
	}
}

func TestAdvance_RegisterAfterAdvance(t *testing.T) {
	s := NewScheduler()
	var count int32

	// Advance before registering - should not fire anything.
	s.Advance(100 * time.Millisecond)

	s.Run(func(TickState) { count++ }, 10.0)

	// After registering, Advance should fire.
	// simTime was 100ms from the first Advance, now Advance(100ms) makes it 200ms.
	// 10Hz (interval=100ms): ticks at next=0,100,200 → 3 ticks.
	s.Advance(100 * time.Millisecond)

	if count != 3 {
		t.Fatalf("expected 3 ticks after registering, got %d", count)
	}
}

func TestAdvance_MaxCatchUpPreserved(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(4)

	var runs int32
	s.Run(func(TickState) { runs++ }, 10.0) // 100ms interval

	// Advance 1 second = 10 ticks, but maxCatchUp=4.
	s.Advance(1000 * time.Millisecond)

	if runs != 4 {
		t.Fatalf("expected 4 runs (maxCatchUp), got %d", runs)
	}
	if s.Dropped() == 0 {
		t.Fatal("expected dropped ticks")
	}
}

func TestScheduler_SnapshotOrder(t *testing.T) {
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
		s.Advance(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]float64(nil), order...)
	}

	a, b := record(), record()
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			t.Fatalf("order differs at %d: %v then %v", i, a[:n], b[:n])
		}
	}
}

func TestAdvance_DroppedTotal(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(2)

	s.Run(func(TickState) {}, 500.0)

	result := s.Advance(100 * time.Millisecond)
	if result.Dropped == 0 {
		t.Fatal("expected dropped ticks")
	}
	if result.Fired != 2 {
		t.Fatalf("expected 2 fired, got %d", result.Fired)
	}
}

func TestAdvance_InvalidHz(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 0)
	s.Run(func(TickState) { count.Add(1) }, -1)

	s.Advance(100 * time.Millisecond)

	if count.Load() != 0 {
		t.Fatalf("expected 0 runs for invalid hz, got %d", count.Load())
	}
}

func TestAdvance_NoWallClockDependency(t *testing.T) {
	s := NewScheduler()
	var ticks []uint64
	var mu sync.Mutex

	s.Run(func(st TickState) {
		mu.Lock()
		ticks = append(ticks, st.Tick)
		mu.Unlock()
	}, 1000.0) // 1ms interval

	// Advance 10 times by 1ms. Ticks must be sequential and deterministic.
	for i := 0; i < 10; i++ {
		s.Advance(1 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ticks) < 10 {
		t.Fatalf("expected at least 10 ticks, got %d", len(ticks))
	}
	for i, tick := range ticks[:10] {
		if tick != uint64(i) {
			t.Fatalf("tick %d: expected %d, got %d", i, i, tick)
		}
	}
}

func TestAdvance_DeltaIsFixed(t *testing.T) {
	s := NewScheduler()
	deltas := make(map[float64]float64)

	s.Run(func(st TickState) {
		deltas[st.Hz] = st.Delta()
	}, 100.0)
	s.Run(func(st TickState) {
		deltas[st.Hz] = st.Delta()
	}, 200.0)

	s.Advance(100 * time.Millisecond)

	if deltas[100.0] < 0.009 || deltas[100.0] > 0.011 {
		t.Fatalf("expected Delta ~0.01 for 100Hz, got %f", deltas[100.0])
	}
	if deltas[200.0] < 0.0049 || deltas[200.0] > 0.0051 {
		t.Fatalf("expected Delta ~0.005 for 200Hz, got %f", deltas[200.0])
	}
}

func TestAdvance_WithoutStart(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 100.0)

	// Advance without calling Start.
	result := s.Advance(50 * time.Millisecond)

	if result.Fired != 6 {
		t.Fatalf("expected 6 ticks without Start, got %d", result.Fired)
	}
	if count.Load() != 6 {
		t.Fatalf("expected count 6, got %d", count.Load())
	}
}

func TestAdvance_PacerNotNeeded(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 60.0) // interval ≈16.67ms

	// Advance 160ms. 60Hz: interval≈16.67ms. next starts at 0.
	// At simTime=160ms: ticks at 0,16.67,...,160 → about 10 ticks.
	// But maxCatchUp=8 limits to 8.
	result := s.Advance(160 * time.Millisecond)

	if result.Fired > 8 {
		t.Fatalf("expected at most 8 ticks (maxCatchUp), got %d", result.Fired)
	}
	if int(count.Load()) != result.Fired {
		t.Fatalf("count %d doesn't match fired %d", count.Load(), result.Fired)
	}
}

func TestAdvance_AdvanceResultMatchesCounters(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(3)

	s.Run(func(TickState) {}, 500.0)

	result := s.Advance(200 * time.Millisecond)

	if result.Fired != int(s.Ticks(500.0)) {
		t.Fatalf("AdvanceResult.Fired %d != Ticks() %d", result.Fired, s.Ticks(500.0))
	}
	if result.Dropped != s.Dropped() {
		t.Fatalf("AdvanceResult.Dropped %d != Dropped() %d", result.Dropped, s.Dropped())
	}
}

func TestAdvance_SpeedChangeBetweenAdvances(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 100.0) // 10ms interval

	// Advance takes simulation time directly; speed only affects the pacer.
	// So Advance(10ms) always fires 1 tick regardless of speed setting.
	s.SetSpeed(1.0)
	s.Advance(10 * time.Millisecond)
	c1 := count.Load()

	s.SetSpeed(2.0)
	s.Advance(10 * time.Millisecond)
	c2 := count.Load()

	if c2-c1 != 1 {
		t.Fatalf("expected 1 tick per Advance regardless of speed, got %d", c2-c1)
	}
}

func TestAdvance_MultipleAdvanceCallsSameResult(t *testing.T) {
	// Test that advancing by N*dT in one call gives the same result as
	// advancing by dT N times (when dT doesn't cross tick boundaries).
	s1 := NewScheduler()
	s2 := NewScheduler()

	var ticks1, ticks2 []uint64
	var mu1, mu2 sync.Mutex

	s1.Run(func(st TickState) {
		mu1.Lock()
		ticks1 = append(ticks1, st.Tick)
		mu1.Unlock()
	}, 100.0)
	s2.Run(func(st TickState) {
		mu2.Lock()
		ticks2 = append(ticks2, st.Tick)
		mu2.Unlock()
	}, 100.0)

	dT := 10 * time.Millisecond
	N := 5

	// Single advance of N*dT = 50ms.
	s1.Advance(time.Duration(N) * dT)

	// N separate advances of dT.
	for i := 0; i < N; i++ {
		s2.Advance(dT)
	}

	mu1.Lock()
	defer mu1.Unlock()
	mu2.Lock()
	defer mu2.Unlock()

	if len(ticks1) != len(ticks2) {
		t.Fatalf("single vs multiple: tick count %d vs %d", len(ticks1), len(ticks2))
	}
	for i := range ticks1 {
		if ticks1[i] != ticks2[i] {
			t.Fatalf("single vs multiple: tick %d differs: %d vs %d", i, ticks1[i], ticks2[i])
		}
	}
}

func TestScheduler_StartStopReuse(t *testing.T) {
	s := NewScheduler()
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 100.0)

	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	c1 := count.Load()
	if c1 == 0 {
		t.Fatal("expected some ticks in first run")
	}

	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	c2 := count.Load()
	if c2 == 0 {
		t.Fatalf("expected some ticks in second run, got 0")
	}
}

func TestScheduler_StableRateOrderVsWallClock(t *testing.T) {
	recordOrder := func() []float64 {
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

	a, b := recordOrder(), recordOrder()
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			t.Fatalf("wall-clock order differs at %d: %v then %v", i, a[:n], b[:n])
		}
	}
}

func TestAdvance_RegisterDuringAdvance(t *testing.T) {
	s := NewScheduler()
	var runs atomic.Int64
	s.Run(func(TickState) { runs.Add(1) }, 200.0)

	for i := 0; i < 10; i++ {
		s.Run(func(TickState) { runs.Add(1) }, 200.0)
		s.Advance(10 * time.Millisecond)
	}

	if runs.Load() == 0 {
		t.Fatal("nothing ran")
	}
}
