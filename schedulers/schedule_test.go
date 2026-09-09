package schedulers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	rPos  = ResourceOf("Position")
	rVel  = ResourceOf("Velocity")
	rHeat = ResourceOf("Heat")
	rGrid = ResourceOf("Grid")
)

func TestAccessConflicts(t *testing.T) {
	cases := []struct {
		name string
		a, b Access
		want bool
	}{
		{"two reads of one resource", Reads(rPos), Reads(rPos), false},
		{"disjoint writes", Writes(rPos), Writes(rVel), false},
		{"same write", Writes(rPos), Writes(rPos), true},
		{"write against read", Writes(rPos), Reads(rPos), true},
		{"read against write", Reads(rPos), Writes(rPos), true},
		{"disjoint entirely", Reads(rPos).Writes(rVel), Reads(rHeat).Writes(rGrid), false},
		{"overlap on the second", Reads(rPos).Writes(rVel), Reads(rVel), true},
	}
	for _, c := range cases {
		if _, got := c.a.conflictsWith(c.b); got != c.want {
			t.Errorf("%s: conflict = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStagesGroupNonConflicting(t *testing.T) {
	s := NewSchedule()
	s.Add("move", Reads(rVel).Writes(rPos), func(TickState) {})
	s.Add("thermal", Writes(rHeat), func(TickState) {})
	s.Add("render-extract", Reads(rPos), func(TickState) {})

	stages := s.Stages()
	if len(stages) != 2 {
		t.Fatalf("stages = %v, want two", stages)
	}
	if len(stages[0]) != 2 {
		t.Fatalf("first stage = %v, want move and thermal together", stages[0])
	}
	if len(stages[1]) != 1 || stages[1][0] != "render-extract" {
		t.Fatalf("second stage = %v, want render-extract alone", stages[1])
	}
}

// Everything writing the same resource is a chain of stages, one deep each.
func TestStagesSerialiseOnOneResource(t *testing.T) {
	s := NewSchedule()
	for _, n := range []string{"a", "b", "c", "d"} {
		s.Add(n, Writes(rGrid), func(TickState) {})
	}
	stages := s.Stages()
	if len(stages) != 4 {
		t.Fatalf("stages = %v, want four", stages)
	}
}

// Systems that share nothing collapse into one stage.
func TestStagesCollapseWhenDisjoint(t *testing.T) {
	s := NewSchedule()
	s.Add("a", Writes(rPos), func(TickState) {})
	s.Add("b", Writes(rVel), func(TickState) {})
	s.Add("c", Writes(rHeat), func(TickState) {})
	s.Add("d", Reads(rPos, rVel, rHeat), func(TickState) {})

	stages := s.Stages()
	if len(stages) != 2 {
		t.Fatalf("stages = %v, want two", stages)
	}
	if len(stages[0]) != 3 {
		t.Fatalf("first stage = %v, want three writers together", stages[0])
	}
}

// A conflicting system runs after the one it conflicts with, every tick.
func TestConflictingSystemsAreOrdered(t *testing.T) {
	s := NewSchedule()
	var mu sync.Mutex
	var order []string
	note := func(n string) func(TickState) {
		return func(TickState) {
			mu.Lock()
			order = append(order, n)
			mu.Unlock()
		}
	}
	s.Add("writer", Writes(rPos), note("writer"))
	s.Add("reader", Reads(rPos), note("reader"))

	for range 20 {
		s.Run(TickState{Hz: 120})
	}
	for i := 0; i < len(order); i += 2 {
		if order[i] != "writer" || order[i+1] != "reader" {
			t.Fatalf("order = %v, want writer before reader every tick", order[:min(6, len(order))])
		}
	}
}

func TestEverySystemRunsOncePerTick(t *testing.T) {
	s := NewSchedule()
	var a, b, c atomic.Int64
	s.Add("a", Writes(rPos), func(TickState) { a.Add(1) })
	s.Add("b", Writes(rVel), func(TickState) { b.Add(1) })
	s.Add("c", Reads(rPos, rVel), func(TickState) { c.Add(1) })

	const ticks = 50
	for i := range ticks {
		s.Run(TickState{Tick: uint64(i), Hz: 120})
	}
	if a.Load() != ticks || b.Load() != ticks || c.Load() != ticks {
		t.Fatalf("ran %d %d %d times, want %d each", a.Load(), b.Load(), c.Load(), ticks)
	}
}

func TestBarrierRunsBetweenStages(t *testing.T) {
	s := NewSchedule()
	var mu sync.Mutex
	var order []string
	s.Add("first", Writes(rPos), func(TickState) {
		mu.Lock()
		order = append(order, "first")
		mu.Unlock()
	})
	s.Add("second", Reads(rPos), func(TickState) {
		mu.Lock()
		order = append(order, "second")
		mu.Unlock()
	})
	s.SetBarrier(func() {
		mu.Lock()
		order = append(order, "barrier")
		mu.Unlock()
	})

	s.Run(TickState{Hz: 120})

	want := []string{"first", "barrier", "second", "barrier"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// A cheap stage runs serially, because splitting costs more than it saves. An
// expensive one is split once it has been measured.
func TestCheapStageStaysSerial(t *testing.T) {
	s := NewSchedule()
	s.SetParallelFloor(time.Millisecond)

	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64
	work := func(TickState) {
		n := concurrent.Add(1)
		for {
			m := maxConcurrent.Load()
			if n <= m || maxConcurrent.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(10 * time.Microsecond)
		concurrent.Add(-1)
	}
	s.Add("a", Writes(rPos), work)
	s.Add("b", Writes(rVel), work)

	for range 20 {
		s.Run(TickState{Hz: 120})
	}
	if maxConcurrent.Load() != 1 {
		t.Fatalf("a stage under the floor ran %d systems at once, want 1", maxConcurrent.Load())
	}
}

func TestExpensiveStageGoesParallel(t *testing.T) {
	s := NewSchedule()
	s.SetParallelFloor(time.Microsecond)

	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64
	work := func(TickState) {
		n := concurrent.Add(1)
		for {
			m := maxConcurrent.Load()
			if n <= m || maxConcurrent.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		concurrent.Add(-1)
	}
	s.Add("a", Writes(rPos), work)
	s.Add("b", Writes(rVel), work)

	for range 10 {
		s.Run(TickState{Hz: 120})
	}
	if maxConcurrent.Load() < 2 {
		t.Fatalf("an expensive stage never ran two systems at once")
	}
}

// Turning concurrency off must not change what the systems compute.
func TestSerialAndParallelAgree(t *testing.T) {
	run := func(parallel bool) int64 {
		var total atomic.Int64
		s := NewSchedule()
		s.SetParallel(parallel)
		s.SetParallelFloor(time.Nanosecond)
		for i := range 8 {
			r := ResourceOf("agree-" + string(rune('a'+i)))
			s.Add("sys", Writes(r), func(TickState) {
				for j := range 1000 {
					total.Add(int64(j % 7))
				}
			})
		}
		for range 20 {
			s.Run(TickState{Hz: 120})
		}
		return total.Load()
	}
	if a, b := run(false), run(true); a != b {
		t.Fatalf("serial total %d, parallel total %d", a, b)
	}
}

func TestPanickingSystemDoesNotStopTheTick(t *testing.T) {
	s := NewSchedule()
	var after atomic.Int64
	s.Add("bad", Writes(rPos), func(TickState) { panic("boom") })
	s.Add("good", Writes(rVel), func(TickState) { after.Add(1) })

	s.Run(TickState{Hz: 120})
	if after.Load() != 1 {
		t.Fatal("a panicking system stopped the tick")
	}
}

func TestConflictsReport(t *testing.T) {
	s := NewSchedule()
	s.Add("move", Writes(rPos), func(TickState) {})
	s.Add("draw", Reads(rPos), func(TickState) {})
	s.Add("thermal", Writes(rHeat), func(TickState) {})

	got := s.Conflicts()
	if len(got) != 1 {
		t.Fatalf("conflicts = %v, want one", got)
	}
	if got[0] != "move and draw both need Position" {
		t.Fatalf("conflict reads %q", got[0])
	}
}

func BenchmarkScheduleRunSerialStage(b *testing.B) {
	s := NewSchedule()
	for i := range 8 {
		r := ResourceOf("bench-serial-" + string(rune('a'+i)))
		s.Add("sys", Writes(r), func(TickState) {})
	}
	s.Build()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.Run(TickState{Hz: 120})
	}
}

// What the concurrency is actually worth on a stage that is compute-bound, and
// what it is worth on one that is not. The second number is the one that keeps
// the floor honest.
func BenchmarkScheduleStage(b *testing.B) {
	const systems = 8

	compute := func(TickState) {
		v := 1.0
		for range 40000 {
			v = v*1.0000001 + 0.5
		}
		if v == 0 {
			b.Fatal("optimised away")
		}
	}

	memory := func(buf []float64) func(TickState) {
		return func(TickState) {
			for i := range buf {
				buf[i] = buf[i]*1.000001 + 0.5
			}
		}
	}

	build := func(parallel bool, fn func(int) func(TickState)) *Schedule {
		s := NewSchedule()
		s.SetParallel(parallel)
		s.SetParallelFloor(time.Nanosecond)
		for i := range systems {
			s.Add("sys", Writes(ResourceOf("stage-bench-"+string(rune('a'+i)))), fn(i))
		}
		s.Build()
		// One warm run so the estimates exist before the timed loop.
		s.Run(TickState{Hz: 120})
		return s
	}

	b.Run("compute-bound/serial", func(b *testing.B) {
		s := build(false, func(int) func(TickState) { return compute })
		for b.Loop() {
			s.Run(TickState{Hz: 120})
		}
	})
	b.Run("compute-bound/parallel", func(b *testing.B) {
		s := build(true, func(int) func(TickState) { return compute })
		for b.Loop() {
			s.Run(TickState{Hz: 120})
		}
	})

	bufs := make([][]float64, systems)
	for i := range bufs {
		bufs[i] = make([]float64, 40000)
	}
	b.Run("memory-bound/serial", func(b *testing.B) {
		s := build(false, func(i int) func(TickState) { return memory(bufs[i]) })
		for b.Loop() {
			s.Run(TickState{Hz: 120})
		}
	})
	b.Run("memory-bound/parallel", func(b *testing.B) {
		s := build(true, func(i int) func(TickState) { return memory(bufs[i]) })
		for b.Loop() {
			s.Run(TickState{Hz: 120})
		}
	})
}
