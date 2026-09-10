package ecs

import (
	"math/rand"
	"testing"
)

func TestTimerFiresOnItsTick(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	e := w.Create()
	tm.After(e, 5)

	for i := 1; i <= 4; i++ {
		fired := 0
		tm.Advance(func(Entity) { fired++ })
		if fired != 0 {
			t.Fatalf("timer fired early, on tick %d", i)
		}
	}
	fired := 0
	tm.Advance(func(got Entity) {
		if got != e {
			t.Fatalf("fired for %d, want %d", uint64(got), uint64(e))
		}
		fired++
	})
	if fired != 1 {
		t.Fatalf("fired %d times on the due tick, want 1", fired)
	}

	tm.Advance(func(Entity) { t.Fatal("the timer fired a second time") })
}

// Beyond the ring's horizon an entry goes into the map, and must fire on the
// same tick it would have from the ring.
func TestTimerBeyondHorizon(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	e := w.Create()
	const far = horizonSize * 3
	tm.After(e, far)

	if got, ok := tm.Scheduled(e); !ok || got != far {
		t.Fatalf("scheduled for %d, want %d", got, far)
	}

	fired := uint64(0)
	for range far {
		tm.Advance(func(Entity) { fired = tm.Now() })
	}
	if fired != far {
		t.Fatalf("fired on tick %d, want %d", fired, far)
	}
}

// A ring bucket is reused every lap, so an entry one lap out must not be
// mistaken for one due now.
func TestTimerDoesNotWrapEarly(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	near := w.Create()
	lapAway := w.Create()
	tm.After(near, 10)
	tm.After(lapAway, horizonSize+10)

	var order []uint64
	for range horizonSize + 10 {
		tm.Advance(func(Entity) { order = append(order, tm.Now()) })
	}
	if len(order) != 2 || order[0] != 10 || order[1] != horizonSize+10 {
		t.Fatalf("fired on ticks %v, want [10 %d]", order, horizonSize+10)
	}
}

func TestTimerCancel(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	a, b, c := w.Create(), w.Create(), w.Create()
	tm.After(a, 3)
	tm.After(b, 3)
	tm.After(c, 3)

	if !tm.Cancel(b) {
		t.Fatal("cancel reported nothing to cancel")
	}
	if tm.Cancel(b) {
		t.Fatal("cancelling twice reported success")
	}
	if _, ok := tm.Scheduled(b); ok {
		t.Fatal("a cancelled timer is still scheduled")
	}

	var got []Entity
	for range 3 {
		tm.Advance(func(e Entity) { got = append(got, e) })
	}
	if len(got) != 2 {
		t.Fatalf("fired %d timers, want 2", len(got))
	}
	for _, e := range got {
		if e == b {
			t.Fatal("a cancelled timer fired")
		}
	}
	_ = a
	_ = c
}

// Rescheduling replaces rather than adding, so an entity cannot accumulate
// timers it never cancelled.
func TestTimerRescheduleReplaces(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	e := w.Create()
	tm.After(e, 5)
	tm.After(e, 9)

	if tm.Pending() != 1 {
		t.Fatalf("pending = %d after rescheduling, want 1", tm.Pending())
	}
	var ticks []uint64
	for range 12 {
		tm.Advance(func(Entity) { ticks = append(ticks, tm.Now()) })
	}
	if len(ticks) != 1 || ticks[0] != 9 {
		t.Fatalf("fired on %v, want [9]", ticks)
	}
}

// A timer set for a tick that has already passed fires next, not never.
func TestTimerInThePastFiresNext(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	for range 10 {
		tm.Advance(func(Entity) {})
	}
	e := w.Create()
	tm.At(e, 3)

	fired := false
	tm.Advance(func(got Entity) {
		if got == e {
			fired = true
		}
	})
	if !fired {
		t.Fatal("a timer set in the past never fired")
	}
}

// A timer rescheduling itself from inside its own callback must land on a later
// tick rather than spinning within this advance.
func TestTimerReschedulesFromCallback(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	e := w.Create()
	tm.After(e, 1)

	fires := 0
	for range 5 {
		tm.Advance(func(got Entity) {
			fires++
			if fires > 10 {
				t.Fatal("the callback spun inside one advance")
			}
			tm.After(got, 1)
		})
	}
	if fires != 5 {
		t.Fatalf("fired %d times over 5 ticks, want 5", fires)
	}
}

func TestDestroyCancelsTimer(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	e := w.Create()
	tm.After(e, 3)
	w.Destroy(e)

	if tm.Pending() != 0 {
		t.Fatalf("pending = %d after destroying the entity, want 0", tm.Pending())
	}
}

// A stale handle must not cancel or reschedule the timer of whichever entity
// reused its index.
func TestTimerRejectsStaleHandle(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	first := w.Create()
	w.Destroy(first)
	second := w.Create()
	if first.Index() != second.Index() {
		t.Skip("index was not reused")
	}
	tm.After(second, 4)

	if tm.Cancel(first) {
		t.Error("a stale handle cancelled a live entity's timer")
	}
	if _, ok := tm.Scheduled(first); ok {
		t.Error("a stale handle read a live entity's timer")
	}
	if _, ok := tm.Scheduled(second); !ok {
		t.Fatal("the live entity lost its timer")
	}
}

// The point of the wheel: a world where everything is waiting costs nothing a
// tick, rather than costing a scan.
func TestIdleWorldCostsNothing(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	for range 40000 {
		e := w.Create()
		tm.After(e, 10000)
	}

	if n := testing.AllocsPerRun(200, func() {
		tm.Advance(func(Entity) { t.Fatal("a timer fired during the idle stretch") })
	}); n != 0 {
		t.Errorf("an idle tick allocated %v times", n)
	}
}

func TestTimerChurn(t *testing.T) {
	w := newTestWorld(t)
	tm := w.Timers()

	rng := rand.New(rand.NewSource(7))
	ents := make([]Entity, 500)
	for i := range ents {
		ents[i] = w.Create()
	}
	want := map[Entity]uint64{}

	for range 50000 {
		e := ents[rng.Intn(len(ents))]
		switch rng.Intn(3) {
		case 0:
			d := uint64(rng.Intn(horizonSize*2) + 1)
			tm.At(e, tm.Now()+d)
			want[e] = tm.Now() + d
		case 1:
			tm.Cancel(e)
			delete(want, e)
		case 2:
			tm.Advance(func(got Entity) {
				due, ok := want[got]
				if !ok {
					t.Fatalf("entity %d fired with no timer recorded", uint64(got))
				}
				if due != tm.Now() {
					t.Fatalf("entity %d fired on %d, want %d", uint64(got), tm.Now(), due)
				}
				delete(want, got)
			})
			for e, due := range want {
				if due <= tm.Now() {
					t.Fatalf("entity %d was due on %d and did not fire by %d", uint64(e), due, tm.Now())
				}
			}
		}
		if tm.Pending() != len(want) {
			t.Fatalf("pending %d, tracked %d", tm.Pending(), len(want))
		}
	}
}

func BenchmarkTimerIdleTick(b *testing.B) {
	w := NewWorld()
	tm := w.Timers()
	for range 40000 {
		tm.After(w.Create(), 1_000_000)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tm.Advance(func(Entity) {})
	}
}

func BenchmarkTimerScheduleCancel(b *testing.B) {
	w := NewWorld()
	tm := w.Timers()
	ents := make([]Entity, 1000)
	for i := range ents {
		ents[i] = w.Create()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		e := ents[i%len(ents)]
		tm.After(e, uint64(i%500)+1)
		tm.Cancel(e)
	}
}
