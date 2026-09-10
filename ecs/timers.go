package ecs

// Timers schedules an entity for a future tick and does not visit it before
// then. It is the second half of iterating only what is doing something: the
// awake partition covers an entity woken by a neighbour changing something it
// depends on, and this covers one that is simply waiting.
//
// A machine with 3.2 seconds of work left at 120 Hz is idle for 384 ticks.
// Without this it is either awake and rejected 384 times, or asleep with no way
// to wake itself.
//
// The near future is a ring of buckets indexed by tick, so scheduling and
// firing touch one slice and allocate nothing. Anything beyond the ring's
// horizon goes into a map keyed by its exact tick, which is read once per
// advance alongside the ring. Nothing migrates between the two, because the
// map is consulted on the tick the entry is due rather than when it comes into
// range.
//
// Time here is the world's tick count, not the wall clock, so a paused or
// fast-forwarded simulation carries its timers with it.

// horizonBits sets the ring size as a power of two. 4096 buckets is 34 seconds
// at 120 Hz, which covers the ordinary machine process; a longer wait costs one
// map entry.
const (
	horizonBits = 12
	horizonSize = 1 << horizonBits
	horizonMask = horizonSize - 1
)

type timerState int8

const (
	timerNone timerState = iota
	timerRing
	timerFar
)

// timerSlot records where an entity's pending timer sits, so cancelling is a
// swap-remove rather than a search.
type timerSlot struct {
	owner Entity
	tick  uint64
	pos   int32
	state timerState
}

// Timers is the wheel. One belongs to a world.
type Timers struct {
	now uint64

	ring [horizonSize][]Entity
	far  map[uint64][]Entity

	slots []timerSlot
}

func newTimers() *Timers {
	return &Timers{
		far:   make(map[uint64][]Entity),
		slots: make([]timerSlot, 0, 256),
	}
}

// Now is the tick the wheel has advanced to.
func (t *Timers) Now() uint64 { return t.now }

func (t *Timers) growSlots(idx int) {
	for len(t.slots) <= idx {
		t.slots = append(t.slots, timerSlot{state: timerNone})
	}
}

func (t *Timers) slot(e Entity) *timerSlot {
	idx := int(e.Index())
	if idx >= len(t.slots) {
		return nil
	}
	s := &t.slots[idx]
	if s.state == timerNone || s.owner != e {
		return nil
	}
	return s
}

// At schedules e to fire on the given tick, replacing any timer it already had.
// A tick at or before now fires on the next advance rather than being lost.
//
// One timer per entity is deliberate: an entity waiting on two things at once
// is waiting on the earlier of them, and holding both here would make cancelling
// ambiguous.
func (t *Timers) At(e Entity, tick uint64) {
	t.Cancel(e)
	if tick <= t.now {
		tick = t.now + 1
	}

	idx := int(e.Index())
	t.growSlots(idx)

	if tick-t.now < horizonSize {
		b := int(tick & horizonMask)
		t.ring[b] = append(t.ring[b], e)
		t.slots[idx] = timerSlot{owner: e, tick: tick, pos: int32(len(t.ring[b]) - 1), state: timerRing}
		return
	}
	t.far[tick] = append(t.far[tick], e)
	t.slots[idx] = timerSlot{owner: e, tick: tick, pos: int32(len(t.far[tick]) - 1), state: timerFar}
}

// After schedules e that many ticks from now.
func (t *Timers) After(e Entity, ticks uint64) { t.At(e, t.now+ticks) }

// Scheduled reports the tick e is waiting for.
func (t *Timers) Scheduled(e Entity) (uint64, bool) {
	s := t.slot(e)
	if s == nil {
		return 0, false
	}
	return s.tick, true
}

// Cancel drops e's pending timer, reporting whether there was one. It is a
// swap-remove from the bucket holding it, so an entity whose situation changes
// before its timer fires costs the same as one that waits.
func (t *Timers) Cancel(e Entity) bool {
	s := t.slot(e)
	if s == nil {
		return false
	}

	var bucket []Entity
	switch s.state {
	case timerRing:
		bucket = t.ring[int(s.tick&horizonMask)]
	case timerFar:
		bucket = t.far[s.tick]
	}

	pos := int(s.pos)
	if pos < len(bucket) && bucket[pos] == e {
		last := len(bucket) - 1
		bucket[pos] = bucket[last]
		if pos != last {
			moved := bucket[pos]
			if ms := t.slot(moved); ms != nil {
				ms.pos = int32(pos)
			}
		}
		bucket = bucket[:last]
	}

	switch s.state {
	case timerRing:
		t.ring[int(s.tick&horizonMask)] = bucket
	case timerFar:
		if len(bucket) == 0 {
			delete(t.far, s.tick)
		} else {
			t.far[s.tick] = bucket
		}
	}

	s.state = timerNone
	return true
}

// Advance moves to the next tick and calls fn for every entity due on it, in
// the order they were scheduled. A timer fires once.
//
// fn may schedule new timers, including for the tick it is being called on;
// those are collected on the next advance rather than during this one, so a
// timer that reschedules itself immediately cannot spin.
func (t *Timers) Advance(fn func(e Entity)) {
	t.now++
	b := int(t.now & horizonMask)

	due := t.ring[b]
	t.ring[b] = t.ring[b][:0]
	for _, e := range due {
		if s := t.slot(e); s != nil && s.tick == t.now {
			s.state = timerNone
			fn(e)
		}
	}

	if far, ok := t.far[t.now]; ok {
		delete(t.far, t.now)
		for _, e := range far {
			if s := t.slot(e); s != nil && s.tick == t.now {
				s.state = timerNone
				fn(e)
			}
		}
	}
}

// Pending is how many timers are outstanding. It walks the far map, so it is
// for tests and diagnostics rather than for a tick.
func (t *Timers) Pending() int {
	n := 0
	for i := range t.ring {
		n += len(t.ring[i])
	}
	for _, b := range t.far {
		n += len(b)
	}
	return n
}

// reset clears every pending timer and returns the wheel to tick zero.
func (t *Timers) reset() {
	for i := range t.ring {
		t.ring[i] = t.ring[i][:0]
	}
	clear(t.far)
	t.slots = t.slots[:0]
	t.now = 0
}
