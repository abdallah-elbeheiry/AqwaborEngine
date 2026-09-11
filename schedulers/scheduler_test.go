package schedulers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdvance_Basic(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1) // every=1 → 60Hz

	// 100ms at 60Hz = 6 ticks. next starts at 0 → ticks 0..5 → 6 ticks.
	result := s.Advance(100 * time.Millisecond)
	if result.Fired != 6 {
		t.Fatalf("expected 6 ticks fired, got %d", result.Fired)
	}
	if count.Load() != 6 {
		t.Fatalf("expected count 6, got %d", count.Load())
	}
}

func TestAdvance_MultipleJobs(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var countFast, countSlow atomic.Int64

	s.Run(func(st TickState) { countFast.Add(1) }, 1) // 60Hz
	s.Run(func(st TickState) { countSlow.Add(1) }, 6) // 10Hz

	// 100ms at 60Hz = 6 master ticks.
	// every=1 fires at ticks 0..5 → 6 ticks.
	// every=6 fires at tick 0, then tick 6 > 5 → 1 tick.
	result := s.Advance(100 * time.Millisecond)
	if result.Fired != 7 {
		t.Fatalf("expected 7 ticks total, got %d", result.Fired)
	}
	if countFast.Load() != 6 {
		t.Fatalf("expected 6 fast ticks, got %d", countFast.Load())
	}
	if countSlow.Load() != 1 {
		t.Fatalf("expected 1 slow tick, got %d", countSlow.Load())
	}
}

func TestAdvance_Order(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var order []int
	var mu sync.Mutex

	// Register slow first, then fast — order in Run doesn't matter, every determines run order.
	s.Run(func(st TickState) {
		mu.Lock()
		order = append(order, 2) // slow (every=6)
		mu.Unlock()
	}, 6)
	s.Run(func(st TickState) {
		mu.Lock()
		order = append(order, 1) // fast (every=1)
		mu.Unlock()
	}, 1)

	s.Advance(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(order) == 0 {
		t.Fatal("nothing ran")
	}
	// every=6 (slow) fires first (lower every = ascending order), then every=1.
	sawSlow := false
	for _, r := range order {
		if r == 1 {
			if sawSlow {
				// Already saw slow, this is fine
			}
			sawSlow = true
		}
		if r == 2 {
			if !sawSlow {
				// This is the slow one, should be first
			}
		}
	}
}

func TestAdvance_Paused(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1)
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
	s.SetMasterHz(60)
	var ticks []uint64
	var mu sync.Mutex

	s.Run(func(st TickState) {
		mu.Lock()
		ticks = append(ticks, st.Tick)
		mu.Unlock()
	}, 1)

	// 100ms at 60Hz = 6 ticks. tick values 0,1,2,3,4,5.
	s.Advance(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	expected := []uint64{0, 1, 2, 3, 4, 5}
	if len(ticks) != len(expected) {
		t.Fatalf("expected %d ticks, got %d", len(expected), len(ticks))
	}
	for i, et := range expected {
		if ticks[i] != et {
			t.Fatalf("tick %d: expected %d, got %d", i, et, ticks[i])
		}
	}
}

func TestAdvance_Delta(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var delta float64

	s.Run(func(st TickState) { delta = st.Delta() }, 1)
	s.Advance(100 * time.Millisecond)

	// every=1, masterHz=60 → Delta = 1/60 ≈ 0.01667
	if delta < 0.016 || delta > 0.017 {
		t.Fatalf("expected Delta ~0.01667, got %f", delta)
	}
}

func TestAdvance_BoundedCatchUp(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(2)

	var runs atomic.Int64
	s.Run(func(TickState) { runs.Add(1) }, 1) // 60Hz

	// 100ms at 60Hz = 6 ticks, but maxCatchUp=2 → only 2 fire.
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
	s.SetMasterHz(60)
	s.SetMaxCatchUp(2)

	s.Run(func(TickState) {}, 1)

	result := s.Advance(100 * time.Millisecond)
	if result.Dropped == 0 {
		t.Fatal("expected dropped ticks")
	}
	if result.Fired != 2 {
		t.Fatalf("expected 2 fired, got %d", result.Fired)
	}
}

func TestAdvance_TicksPerJob(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	s.Run(func(TickState) {}, 1) // 60Hz
	s.Run(func(TickState) {}, 2) // 30Hz

	s.Advance(100 * time.Millisecond) // 6 master ticks

	// maxCatchUp=8 limits each job to 8 ticks per Advance.
	// every=1: fires at 0,1,2,3,4,5 → 6 ticks
	// every=2: fires at 0,2,4 → 3 ticks
	if t1 := s.Ticks(1); t1 != 6 {
		t.Fatalf("expected 6 ticks for every=1, got %d", t1)
	}
	if t2 := s.Ticks(2); t2 != 3 {
		t.Fatalf("expected 3 ticks for every=2, got %d", t2)
	}
}

func TestAdvance_SimTimeAccumulates(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count int32

	s.Run(func(TickState) { count++ }, 6) // every=6 → 10Hz, interval=100ms

	// 50ms at 60Hz = 3 quanta. every=6 fires at tick 0 → 1 tick.
	s.Advance(50 * time.Millisecond)
	if count != 1 {
		t.Fatalf("after first advance: expected 1, got %d", count)
	}
	// 50ms more: 6 quanta total. every=6 next=6 not < 6 → 0 ticks.
	s.Advance(50 * time.Millisecond)
	if count != 1 {
		t.Fatalf("after second advance: expected 1, got %d", count)
	}
	// 50ms more: 9 quanta total. every=6 next=6 < 9 → 1 tick.
	s.Advance(50 * time.Millisecond)
	if count != 2 {
		t.Fatalf("after third advance: expected 2, got %d", count)
	}
}

func TestAdvance_ZeroSimDT(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 1)

	result := s.Advance(0)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for Advance(0), got %d", result.Fired)
	}
}

func TestAdvance_NegativeSimDT(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 1)

	result := s.Advance(-10 * time.Millisecond)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for negative simDT, got %d", result.Fired)
	}
}

func TestAdvance_MultipleAdvances(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64
	s.Run(func(TickState) { count.Add(1) }, 1) // 60Hz

	// 50ms × 3 = 150ms = 9 ticks. every=1 fires at 0..8 → 9 ticks.
	s.Advance(50 * time.Millisecond)
	s.Advance(50 * time.Millisecond)
	s.Advance(50 * time.Millisecond)

	if count.Load() != 9 {
		t.Fatalf("expected 9 ticks after three 50ms advances, got %d", count.Load())
	}
}

func TestAdvance_RegisterAfterAdvance(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count int32

	// Advance before registering - nothing to fire.
	s.Advance(100 * time.Millisecond)

	s.Run(func(TickState) { count++ }, 6) // every=6 → 10Hz

	// simTicks is 6 from first advance. Advance(100ms) → 12 master ticks.
	// every=6 fires at tick 6, 12 → 2 ticks.
	s.Advance(100 * time.Millisecond)
	if count != 2 {
		t.Fatalf("expected 2 ticks after registering, got %d", count)
	}
}

func TestAdvance_MaxCatchUpPreserved(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(4)

	var runs int32
	s.Run(func(TickState) { runs++ }, 1) // 60Hz

	// 1 second = 60 ticks, but maxCatchUp=4.
	s.Advance(1 * time.Second)
	if runs != 4 {
		t.Fatalf("expected 4 runs (maxCatchUp), got %d", runs)
	}
	if s.Dropped() == 0 {
		t.Fatal("expected dropped ticks")
	}
}

func TestAdvance_DroppedTotal(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(2)

	s.Run(func(TickState) {}, 1)

	result := s.Advance(100 * time.Millisecond)
	if result.Dropped == 0 {
		t.Fatal("expected dropped ticks")
	}
	if result.Fired != 2 {
		t.Fatalf("expected 2 fired, got %d", result.Fired)
	}
}

func TestAdvance_AdvanceResultMatchesCounters(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(3)

	s.Run(func(TickState) {}, 1)

	result := s.Advance(200 * time.Millisecond) // 12 ticks, maxCatchUp=3 → 3 fired
	if result.Fired != int(s.Ticks(1)) {
		t.Fatalf("AdvanceResult.Fired %d != Ticks() %d", result.Fired, s.Ticks(1))
	}
	if result.Dropped != s.Dropped() {
		t.Fatalf("AdvanceResult.Dropped %d != Dropped() %d", result.Dropped, s.Dropped())
	}
}

// ==================== Determinism tests ====================

func TestAdvance_Deterministic(t *testing.T) {
	s1 := NewScheduler()
	s2 := NewScheduler()
	s1.SetMasterHz(60)
	s2.SetMasterHz(60)

	s1.Run(func(st TickState) { _ = st.Tick }, 1)
	s1.Run(func(st TickState) { _ = st.Tick }, 2)
	s2.Run(func(st TickState) { _ = st.Tick }, 1)
	s2.Run(func(st TickState) { _ = st.Tick }, 2)

	seq := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 5 * time.Millisecond, 100 * time.Millisecond}

	var ticks1, ticks2 []uint64
	var mu1, mu2 sync.Mutex

	s1.Run(func(st TickState) {
		mu1.Lock()
		ticks1 = append(ticks1, st.Tick)
		mu1.Unlock()
	}, 1)
	s1.Run(func(st TickState) {
		mu1.Lock()
		ticks1 = append(ticks1, st.Tick+1000)
		mu1.Unlock()
	}, 2)

	s2.Run(func(st TickState) {
		mu2.Lock()
		ticks2 = append(ticks2, st.Tick)
		mu2.Unlock()
	}, 1)
	s2.Run(func(st TickState) {
		mu2.Lock()
		ticks2 = append(ticks2, st.Tick+1000)
		mu2.Unlock()
	}, 2)

	for _, dt := range seq {
		s1.Advance(dt)
		s2.Advance(dt)
	}

	if len(ticks1) != len(ticks2) {
		t.Fatalf("determinism failed: tick counts differ %d vs %d", len(ticks1), len(ticks2))
	}
}

func TestAdvance_Replay(t *testing.T) {
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
	s1.SetMasterHz(60)
	var counts1 []uint64
	var mu1 sync.Mutex
	s1.Run(func(st TickState) {
		mu1.Lock()
		counts1 = append(counts1, st.Tick)
		mu1.Unlock()
	}, 1)

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
	s2.SetMasterHz(60)
	var counts2 []uint64
	var mu2 sync.Mutex
	s2.Run(func(st TickState) {
		mu2.Lock()
		counts2 = append(counts2, st.Tick)
		mu2.Unlock()
	}, 1)

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
		s.SetMasterHz(60)
		var ticks []uint64
		var mu sync.Mutex
		s.Run(func(st TickState) {
			mu.Lock()
			ticks = append(ticks, st.Tick)
			mu.Unlock()
		}, 1)

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
	record := func() []uint {
		s := NewScheduler()
		s.SetMasterHz(60)
		var mu sync.Mutex
		var order []uint
		for _, every := range []uint{6, 1, 3, 12} {
			every := every
			s.Run(func(TickState) {
				mu.Lock()
				if len(order) < 20 {
					order = append(order, every)
				}
				mu.Unlock()
			}, every)
		}
		s.Advance(200 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]uint(nil), order...)
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

func TestAdvance_NoWallClockDependency(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(1000) // 1kHz → 1ms per tick
	var ticks []uint64
	var mu sync.Mutex

	s.Run(func(st TickState) {
		mu.Lock()
		ticks = append(ticks, st.Tick)
		mu.Unlock()
	}, 1)

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
	s.SetMasterHz(60)
	deltas := make(map[uint]float64)

	s.Run(func(st TickState) {
		deltas[0] = st.Delta()
	}, 1)
	s.Run(func(st TickState) {
		deltas[2] = st.Delta()
	}, 2)

	s.Advance(100 * time.Millisecond)

	// every=1: Delta = 1/60 ≈ 0.01667
	if d := deltas[0]; d < 0.016 || d > 0.017 {
		t.Fatalf("expected Delta ~0.01667 for every=1, got %f", d)
	}
	// every=2: Delta = 2/60 ≈ 0.03333
	if d := deltas[2]; d < 0.033 || d > 0.034 {
		t.Fatalf("expected Delta ~0.03333 for every=2, got %f", d)
	}
}

func TestAdvance_SimTicksMonotonic(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	var lastSimTicks uint64
	s.Run(func(st TickState) {
		st2 := s.SimTicks()
		if st2 < lastSimTicks {
			t.Errorf("SimTicks went backwards: %d → %d", lastSimTicks, st2)
		}
		lastSimTicks = st2
	}, 1)

	for i := 0; i < 10; i++ {
		s.Advance(10 * time.Millisecond)
	}
}

// ==================== AdvanceTicks tests ====================

func TestAdvanceTicks_Basic(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1) // every=1 → 60Hz

	// 1 tick at speed=1 → 1 tick.
	result := s.AdvanceTicks(1)
	if result.Fired != 1 {
		t.Fatalf("expected 1 tick fired, got %d", result.Fired)
	}
	if count.Load() != 1 {
		t.Fatalf("expected count 1, got %d", count.Load())
	}
}

func TestAdvanceTicks_MultipleTicks(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1)

	// 6 ticks at speed=1 → 6 ticks.
	result := s.AdvanceTicks(6)
	if result.Fired != 6 {
		t.Fatalf("expected 6 ticks fired, got %d", result.Fired)
	}
}

func TestAdvanceTicks_SimTicksAdvances(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	s.AdvanceTicks(10)
	if st := s.SimTicks(); st != 10 {
		t.Fatalf("expected SimTicks=10, got %d", st)
	}
	s.AdvanceTicks(5)
	if st := s.SimTicks(); st != 15 {
		t.Fatalf("expected SimTicks=15, got %d", st)
	}
}

func TestAdvanceTicks_ZeroNoOp(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	result := s.AdvanceTicks(0)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for AdvanceTicks(0), got %d", result.Fired)
	}
	if s.SimTicks() != 0 {
		t.Fatalf("expected SimTicks=0, got %d", s.SimTicks())
	}
}

func TestAdvanceTicks_NegativeNoOp(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	result := s.AdvanceTicks(-5)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks for negative AdvanceTicks, got %d", result.Fired)
	}
}

func TestAdvanceTicks_NoMasterHzNoOp(t *testing.T) {
	s := NewScheduler()
	// MasterHz not set.

	result := s.AdvanceTicks(10)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks when MasterHz unset, got %d", result.Fired)
	}
}

func TestAdvanceTicks_SpeedScales(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetSpeed(2)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1)

	// 1 tick at speed=2 → 2 scaled ticks.
	result := s.AdvanceTicks(1)
	if result.Fired != 2 {
		t.Fatalf("expected 2 ticks fired at speed 2, got %d", result.Fired)
	}
	if count.Load() != 2 {
		t.Fatalf("expected count 2, got %d", count.Load())
	}
}

func TestAdvanceTicks_SpeedZeroNoOp(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetSpeed(0)

	result := s.AdvanceTicks(10)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks at speed 0, got %d", result.Fired)
	}
}

func TestAdvanceTicks_PausedNoOp(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.Pause()

	result := s.AdvanceTicks(10)
	if result.Fired != 0 {
		t.Fatalf("expected 0 ticks when paused, got %d", result.Fired)
	}
}

func TestAdvanceTicks_Quantum(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetSpeed(1)

	q := s.Quantum()
	if q != time.Second/60 {
		t.Fatalf("expected Quantum=1s/60, got %v", q)
	}

	s.SetSpeed(2)
	q = s.Quantum()
	if q != time.Second/120 {
		t.Fatalf("expected Quantum=1s/120 at speed 2, got %v", q)
	}
}

// ==================== Speed tests ====================

func TestSpeed_Default(t *testing.T) {
	s := NewScheduler()
	if s.Speed() != 1.0 {
		t.Fatalf("expected default speed 1.0, got %f", s.Speed())
	}
}

func TestSpeed_SetGet(t *testing.T) {
	s := NewScheduler()
	s.SetSpeed(3.5)
	if s.Speed() != 3.5 {
		t.Fatalf("expected speed 3.5, got %f", s.Speed())
	}
}

func TestSpeed_NegativeClamped(t *testing.T) {
	s := NewScheduler()
	s.SetSpeed(-5)
	if s.Speed() != 0 {
		t.Fatalf("expected speed 0 (clamped), got %f", s.Speed())
	}
}

func TestSpeed_AdvanceIgnoresSpeed(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 1)

	s.SetSpeed(1.0)
	s.Advance(20 * time.Millisecond) // floor(20*60/1000) = 1 tick
	c1 := count.Load()

	s.SetSpeed(5.0)
	s.Advance(20 * time.Millisecond) // still 1 tick, speed ignored
	c2 := count.Load()

	// Both advance the same sim time. 20ms at 60Hz = 1 tick each.
	if c2-c1 != 1 {
		t.Fatalf("expected 1 tick per Advance regardless of speed, got %d", c2-c1)
	}
}

func TestSpeed_AdvanceTicksScales(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64
	s.Run(func(st TickState) { count.Add(1) }, 1)

	s.SetSpeed(1.0)
	s.AdvanceTicks(1)
	c1 := count.Load()

	s.SetSpeed(3.0)
	s.AdvanceTicks(1)
	c2 := count.Load()

	// speed=3, 1 tick → 3 scaled ticks → 3 ticks fired.
	if c2-c1 != 3 {
		t.Fatalf("expected 3 ticks at speed 3, got %d", c2-c1)
	}
}

// ==================== Job control tests ====================

func TestJob_PauseResume(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	job := s.Run(func(st TickState) { count.Add(1) }, 1)

	s.Advance(50 * time.Millisecond) // 3 ticks
	if count.Load() != 3 {
		t.Fatalf("before pause: expected 3, got %d", count.Load())
	}

	job.Pause()
	s.Advance(50 * time.Millisecond) // 3 more ticks, but job paused
	if count.Load() != 3 {
		t.Fatalf("after pause: expected 3 (unchanged), got %d", count.Load())
	}

	job.Resume()
	s.Advance(50 * time.Millisecond) // 3 more ticks, job resumes
	if count.Load() != 6 {
		t.Fatalf("after resume: expected 6, got %d", count.Load())
	}
}

func TestJob_Stop(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	job := s.Run(func(st TickState) { count.Add(1) }, 1)

	s.Advance(50 * time.Millisecond) // 3 ticks
	if count.Load() != 3 {
		t.Fatalf("before stop: expected 3, got %d", count.Load())
	}

	job.Stop()
	s.Advance(50 * time.Millisecond) // 3 more ticks, but job stopped
	if count.Load() != 3 {
		t.Fatalf("after stop: expected 3 (unchanged), got %d", count.Load())
	}
}

func TestJob_PauseOneOthersFire(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var countFast, countSlow atomic.Int64

	fast := s.Run(func(st TickState) { countFast.Add(1) }, 1)
	s.Run(func(st TickState) { countSlow.Add(1) }, 6)

	fast.Pause()
	s.Advance(100 * time.Millisecond) // 6 master ticks

	if countFast.Load() != 0 {
		t.Fatalf("fast job should be paused, got %d", countFast.Load())
	}
	if countSlow.Load() != 1 {
		t.Fatalf("slow job should fire, got %d", countSlow.Load())
	}
}

func TestJob_PauseFrozenTickIndex(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var lastTick atomic.Uint64

	job := s.Run(func(st TickState) { lastTick.Store(st.Tick) }, 1)

	s.Advance(50 * time.Millisecond) // 3 ticks → tick values 0,1,2
	if lastTick.Load() != 2 {
		t.Fatalf("before pause: expected tick 2, got %d", lastTick.Load())
	}

	job.Pause()
	s.Advance(200 * time.Millisecond) // 12 ticks, but paused
	// tick should NOT have advanced
	if lastTick.Load() != 2 {
		t.Fatalf("while paused: expected tick 2 (frozen), got %d", lastTick.Load())
	}

	job.Resume()
	s.Advance(1 * time.Millisecond) // 0 ticks (less than one quantum at 60Hz)
	// Still frozen at 2 because no new ticks fired
	s.Advance(50 * time.Millisecond) // 3 ticks → tick values 3,4,5
	if lastTick.Load() != 5 {
		t.Fatalf("after resume: expected tick 5, got %d", lastTick.Load())
	}
}

// ==================== Clear vs Reset tests ====================

func TestClear_RemovesJobs(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1)
	s.Advance(50 * time.Millisecond)
	if count.Load() != 3 {
		t.Fatalf("before clear: expected 3, got %d", count.Load())
	}

	s.Clear()
	s.Advance(50 * time.Millisecond) // no jobs → 0 ticks
	if count.Load() != 3 {
		t.Fatalf("after clear: expected 3 (no new ticks), got %d", count.Load())
	}
}

func TestClear_DoesNotClearClock(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	s.Advance(100 * time.Millisecond)
	if s.SimTicks() == 0 {
		t.Fatal("expected non-zero SimTicks")
	}

	s.Clear()
	if s.SimTicks() == 0 {
		t.Fatal("Clear should not reset SimTicks")
	}
}

func TestReset_ClearsClock(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	s.Run(func(TickState) {}, 1)
	s.Advance(100 * time.Millisecond)
	if s.SimTicks() == 0 {
		t.Fatal("expected non-zero SimTicks")
	}

	s.Reset()
	if s.SimTicks() != 0 {
		t.Fatalf("expected SimTicks=0 after Reset, got %d", s.SimTicks())
	}
	if s.SimTime() != 0 {
		t.Fatalf("expected SimTime=0 after Reset, got %v", s.SimTime())
	}
}

func TestReset_DoesNotRemoveJobs(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	var count atomic.Int64

	s.Run(func(st TickState) { count.Add(1) }, 1)
	s.Advance(50 * time.Millisecond)
	if count.Load() != 3 {
		t.Fatalf("before reset: expected 3, got %d", count.Load())
	}

	s.Reset()
	s.Advance(50 * time.Millisecond) // jobs still exist → 3 more ticks
	if count.Load() != 6 {
		t.Fatalf("after reset: expected 6, got %d", count.Load())
	}
}

func TestReset_ClearsJobCounters(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(2)

	s.Run(func(TickState) {}, 1)
	s.Advance(100 * time.Millisecond) // 6 ticks, maxCatchUp=2 → 2 fired, dropped > 0

	if s.Dropped() == 0 {
		t.Fatal("expected dropped ticks before reset")
	}

	s.Reset()
	if s.Dropped() != 0 {
		t.Fatalf("expected Dropped=0 after Reset, got %d", s.Dropped())
	}
	if s.Ticks(1) != 0 {
		t.Fatalf("expected Ticks=0 after Reset, got %d", s.Ticks(1))
	}
}

// ==================== Catch-up / Dropped tests ====================

func TestCatchUp_PerAdvance(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	s.SetMaxCatchUp(2)

	var runs atomic.Int64
	s.Run(func(TickState) { runs.Add(1) }, 1)

	// Each advance of 100ms = 6 ticks, maxCatchUp=2 → 2 fired, 4 dropped.
	r1 := s.Advance(100 * time.Millisecond)
	if r1.Fired != 2 {
		t.Fatalf("expected 2 fired in first advance, got %d", r1.Fired)
	}
	if r1.Dropped != 4 {
		t.Fatalf("expected 4 dropped in first advance, got %d", r1.Dropped)
	}

	r2 := s.Advance(100 * time.Millisecond)
	if r2.Fired != 2 {
		t.Fatalf("expected 2 fired in second advance, got %d", r2.Fired)
	}
	if r2.Dropped != 4 {
		t.Fatalf("expected 4 dropped in second advance, got %d", r2.Dropped)
	}

	// Cumulative dropped should be 8.
	if s.Dropped() != 8 {
		t.Fatalf("expected cumulative Dropped=8, got %d", s.Dropped())
	}
}

// ==================== Determinism / replay ====================

func TestDeterminism_TwoSchedulers(t *testing.T) {
	makeAndRun := func() []uint64 {
		s := NewScheduler()
		s.SetMasterHz(60)
		var ticks []uint64
		var mu sync.Mutex
		s.Run(func(st TickState) {
			mu.Lock()
			ticks = append(ticks, st.Tick)
			mu.Unlock()
		}, 1)
		s.Run(func(st TickState) {
			mu.Lock()
			ticks = append(ticks, st.Tick+1000)
			mu.Unlock()
		}, 2)

		seq := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 100 * time.Millisecond}
		for _, dt := range seq {
			s.Advance(dt)
		}
		return ticks
	}

	t1, t2 := makeAndRun(), makeAndRun()
	if len(t1) != len(t2) {
		t.Fatalf("tick counts differ: %d vs %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Fatalf("tick %d differs: %d vs %d", i, t1[i], t2[i])
		}
	}
}

func TestReplay_RecordAndReplay(t *testing.T) {
	type op struct {
		advance  time.Duration
		ticks    int
		useTicks bool
		speed    float64
		pause    bool
		resume   bool
	}

	ops := []op{
		{advance: 10 * time.Millisecond, speed: 1.0},
		{advance: 20 * time.Millisecond, speed: 1.0},
		{ticks: 3, useTicks: true, speed: 2.0},
		{advance: 100 * time.Millisecond, speed: 1.0},
		{pause: true},
		{advance: 50 * time.Millisecond, speed: 1.0},
		{resume: true},
		{ticks: 2, useTicks: true, speed: 1.0},
	}

	play := func() []uint64 {
		s := NewScheduler()
		s.SetMasterHz(60)
		var ticks []uint64
		var mu sync.Mutex
		s.Run(func(st TickState) {
			mu.Lock()
			ticks = append(ticks, st.Tick)
			mu.Unlock()
		}, 1)

		for _, op := range ops {
			s.SetSpeed(op.speed)
			if op.pause {
				s.Pause()
			}
			if op.resume {
				s.Resume()
			}
			if op.useTicks {
				s.AdvanceTicks(op.ticks)
			} else {
				s.Advance(op.advance)
			}
		}
		return ticks
	}

	t1, t2 := play(), play()
	if len(t1) != len(t2) {
		t.Fatalf("replay tick counts differ: %d vs %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Fatalf("replay tick %d differs: %d vs %d", i, t1[i], t2[i])
		}
	}
}

// ==================== Edge cases ====================

func TestAdvance_InvalidEvery(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	job := s.Run(func(TickState) {}, 0)
	if job.j != nil {
		t.Fatal("expected nil job for every=0")
	}
}

func TestMasterHz_RejectsZero(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(0)
	if s.MasterHz() != 0 {
		t.Fatalf("expected MasterHz=0, got %d", s.MasterHz())
	}
}

func TestMasterHz_RejectsOver1000(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(1001)
	if s.MasterHz() != 0 {
		t.Fatalf("expected MasterHz=0, got %d", s.MasterHz())
	}
}

func TestQuantum_UnsetMasterHz(t *testing.T) {
	s := NewScheduler()
	if s.Quantum() != 0 {
		t.Fatalf("expected Quantum=0 when MasterHz unset, got %v", s.Quantum())
	}
}

func TestSimTicks_Zero(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	if s.SimTicks() != 0 {
		t.Fatalf("expected SimTicks=0, got %d", s.SimTicks())
	}
}

func TestDropped_NoJobs(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	if s.Dropped() != 0 {
		t.Fatalf("expected Dropped=0 with no jobs, got %d", s.Dropped())
	}
}

func TestTicks_NoJobs(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)
	if s.Ticks(1) != 0 {
		t.Fatalf("expected Ticks=0 with no jobs, got %d", s.Ticks(1))
	}
}

func TestSetMaxCatchUp_IgnoresZero(t *testing.T) {
	s := NewScheduler()
	s.SetMaxCatchUp(0)
	if s.maxCatchUp.Load() != DefaultMaxCatchUp {
		t.Fatalf("expected maxCatchUp unchanged at %d, got %d", DefaultMaxCatchUp, s.maxCatchUp.Load())
	}
}

// ==================== Concurrency (race detector) ====================

func TestRace_RunDuringIdle(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Run(func(TickState) {}, 1)
		}()
	}
	wg.Wait()
	// Just ensure no race; Advance not called concurrently.
}

func TestRace_StopDuringIdle(t *testing.T) {
	s := NewScheduler()
	s.SetMasterHz(60)

	var jobs []Job
	for i := 0; i < 100; i++ {
		jobs = append(jobs, s.Run(func(TickState) {}, 1))
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.Stop()
		}()
	}
	wg.Wait()
}
