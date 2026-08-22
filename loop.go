package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// Task is a schedulable piece of work.
type Task struct {
	fn        func()
	interval  int64
	timeDelay int64
	tickDelay uint32
	maxRuns   uint32

	lastRun   int64
	finishAt  int64
	tickCount uint32

	canceled atomic.Uint32
	done     atomic.Bool
	cond     func() bool

	onDone *atomic.Uint64
}

func CreateTask(fn func(), opts ...func(*Task)) *Task {
	t := &Task{fn: fn}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *Task) run(now int64) {
	if t.canceled.Load() != 0 {
		t.complete()
		return
	}
	if t.finishAt != 0 && now >= t.finishAt {
		t.complete()
		return
	}
	if t.tickDelay > 0 {
		t.tickDelay--
		return
	}
	if t.interval != 0 && now-t.lastRun < t.interval {
		return
	}
	t.fn()
	t.lastRun = now
	t.tickCount++
	if t.maxRuns != 0 && t.tickCount >= t.maxRuns {
		t.complete()
		return
	}
	if t.cond != nil && t.cond() {
		t.complete()
	}
}

func (t *Task) complete() {
	t.done.Store(true)
	if t.onDone != nil {
		t.onDone.Add(1)
	}
}

func (t *Task) Cancel()      { t.canceled.Store(1) }
func (t *Task) IsDone() bool { return t.done.Load() }

func Once() func(*Task)                   { return func(t *Task) { t.maxRuns = 1 } }
func Every(d time.Duration) func(*Task)   { return func(t *Task) { t.interval = int64(d) } }
func Times(n uint32) func(*Task)          { return func(t *Task) { t.maxRuns = n } }
func After(d time.Duration) func(*Task)   { return func(t *Task) { t.timeDelay = int64(d) } }
func AfterTicks(count uint32) func(*Task) { return func(t *Task) { t.tickDelay = count } }

// Loop is the deterministic, single-threaded fixed-step simulation core.
type Loop struct {
	hz           float64
	lastTickNano atomic.Int64
	delta        atomic.Int64
	tickCount    atomic.Uint64
	lag          atomic.Int64
	startedAt    time.Time

	tasks      []*Task
	pending    []*Task
	pendingMu  sync.Mutex
	hasPending atomic.Uint32

	groupMu sync.Mutex
	groups  []*SerialGroup

	stop    chan struct{}
	runDone chan struct{}
	running atomic.Bool

	completed atomic.Uint64
}

func NewLoop(hz float32) *Loop {
	return &Loop{
		hz:      float64(hz),
		tasks:   make([]*Task, 0, 1024),
		pending: make([]*Task, 0, 128),
	}
}

func (l *Loop) Serial() *SerialGroup {
	g := &SerialGroup{
		loop:    l,
		tasks:   make([]*Task, 0, 64),
		pending: make([]*Task, 0, 16),
	}
	l.groupMu.Lock()
	l.groups = append(l.groups, g)
	l.groupMu.Unlock()
	return g
}

func (l *Loop) snapshotGroups() []*SerialGroup {
	l.groupMu.Lock()
	defer l.groupMu.Unlock()
	if len(l.groups) == 0 {
		return nil
	}
	out := make([]*SerialGroup, len(l.groups))
	copy(out, l.groups)
	return out
}

type SerialGroup struct {
	loop       *Loop
	tasks      []*Task
	pending    []*Task
	pendingMu  sync.Mutex
	hasPending atomic.Uint32
}

func (g *SerialGroup) add(t *Task) {
	t.onDone = &g.loop.completed
	g.pendingMu.Lock()
	g.pending = append(g.pending, t)
	g.pendingMu.Unlock()
	g.hasPending.Store(1)
}

func (g *SerialGroup) run(nowUnix int64) {
	if g.hasPending.Load() != 0 {
		g.pendingMu.Lock()
		if len(g.pending) > 0 {
			g.tasks = append(g.tasks, g.pending...)
			g.pending = g.pending[:0]
		}
		g.hasPending.Store(0)
		g.pendingMu.Unlock()
	}
	for _, t := range g.tasks {
		t.run(nowUnix)
	}
}

func (g *SerialGroup) compact() {
	n := 0
	for _, t := range g.tasks {
		if !t.IsDone() {
			g.tasks[n] = t
			n++
		}
	}
	g.tasks = g.tasks[:n]
}

func (g *SerialGroup) Do(fn func()) *Task {
	t := CreateTask(fn)
	g.add(t)
	return t
}
func (g *SerialGroup) Once(fn func()) *Task {
	t := CreateTask(fn, Once())
	g.add(t)
	return t
}
func (g *SerialGroup) Every(d time.Duration, fn func()) *Task {
	t := CreateTask(fn, Every(d))
	g.add(t)
	return t
}
func (g *SerialGroup) Times(n uint32, fn func()) *Task {
	t := CreateTask(fn, Times(n))
	g.add(t)
	return t
}
func (g *SerialGroup) After(d time.Duration, fn func()) *Task {
	t := CreateTask(fn, After(d))
	g.add(t)
	return t
}
func (g *SerialGroup) AfterTicks(count uint32, fn func()) *Task {
	t := CreateTask(fn, AfterTicks(count))
	g.add(t)
	return t
}

func (l *Loop) Add(t *Task) {
	t.onDone = &l.completed
	l.pendingMu.Lock()
	l.pending = append(l.pending, t)
	l.pendingMu.Unlock()
	l.hasPending.Store(1)
}

func (l *Loop) Start() {
	if l.running.Swap(true) {
		return
	}
	l.stop = make(chan struct{})
	l.runDone = make(chan struct{})
	go func() {
		defer close(l.runDone)
		l.run()
	}()
}

const maxStepsPerRound = 1024

func (l *Loop) run() {
	hz := l.hz
	if hz <= 0 {
		hz = 60
	}
	interval := time.Duration(float64(time.Second) / hz)
	intervalNano := int64(interval)
	l.delta.Store(intervalNano)
	l.startedAt = time.Now()

	timer := time.NewTimer(interval)
	defer timer.Stop()

	nextTickNano := time.Now().UnixNano() + intervalNano

	for {
		select {
		case <-l.stop:
			return
		case <-timer.C:
		}

		nowUnix := time.Now().UnixNano()
		if nowUnix < nextTickNano {
			timer.Reset(time.Duration(nextTickNano - nowUnix))
			continue
		}

		steps := 0
		for nowUnix >= nextTickNano {
			l.tick(nowUnix)
			nextTickNano += intervalNano
			steps++
			nowUnix = time.Now().UnixNano()
			if lag := nowUnix - nextTickNano; lag > 0 {
				l.lag.Store(lag)
			} else {
				l.lag.Store(0)
			}
			select {
			case <-l.stop:
				return
			default:
			}
			if steps >= maxStepsPerRound {
				break
			}
		}

		timer.Reset(time.Duration(max(nextTickNano-time.Now().UnixNano(), 0)))
	}
}

func (l *Loop) tick(nowUnix int64) {
	l.lastTickNano.Store(nowUnix)
	l.drainPending()
	groups := l.snapshotGroups()
	for _, g := range groups {
		g.run(nowUnix)
	}
	if len(l.tasks) > 0 {
		l.runTasks(l.tasks, nowUnix)
	}
	if l.completed.Load() > 0 {
		l.compactTasks()
		for _, g := range groups {
			g.compact()
		}
		l.completed.Store(0)
	}
	l.tickCount.Add(1)
}

func (l *Loop) drainPending() {
	if l.hasPending.Load() == 0 {
		return
	}
	l.pendingMu.Lock()
	if len(l.pending) > 0 {
		l.tasks = append(l.tasks, l.pending...)
		l.pending = l.pending[:0]
	}
	l.hasPending.Store(0)
	l.pendingMu.Unlock()
}

func (l *Loop) runTasks(tasks []*Task, nowUnix int64) {
	for _, t := range tasks {
		t.run(nowUnix)
	}
}

func (l *Loop) compactTasks() {
	n := 0
	for _, t := range l.tasks {
		if !t.IsDone() {
			l.tasks[n] = t
			n++
		}
	}
	l.tasks = l.tasks[:n]
}

func (l *Loop) Stop() {
	if !l.running.Swap(false) {
		return
	}
	close(l.stop)
	<-l.runDone
}

func (l *Loop) Alpha() float32 {
	now := time.Now().UnixNano()
	last := l.lastTickNano.Load()
	delta := l.delta.Load()
	if last == 0 || delta <= 0 {
		return 1
	}
	alpha := float32(float64(now-last) / float64(delta))
	if alpha < 0 {
		return 0
	}
	if alpha > 1 {
		return 1
	}
	return alpha
}

func (l *Loop) TickCount() uint64    { return l.tickCount.Load() }
func (l *Loop) Delta() time.Duration { return time.Duration(l.delta.Load()) }
func (l *Loop) Lag() time.Duration   { return time.Duration(l.lag.Load()) }
func (l *Loop) AchievedHz() float64 {
	if l.startedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(l.startedAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(l.tickCount.Load()) / elapsed
}

func (l *Loop) Do(fn func()) *Task {
	t := CreateTask(fn)
	l.Add(t)
	return t
}
func (l *Loop) Once(fn func()) *Task {
	t := CreateTask(fn, Once())
	l.Add(t)
	return t
}
func (l *Loop) Every(d time.Duration, fn func()) *Task {
	t := CreateTask(fn, Every(d))
	l.Add(t)
	return t
}
func (l *Loop) Times(n uint32, fn func()) *Task {
	t := CreateTask(fn, Times(n))
	l.Add(t)
	return t
}
func (l *Loop) After(d time.Duration, fn func()) *Task {
	t := CreateTask(fn, After(d))
	l.Add(t)
	return t
}
func (l *Loop) AfterTicks(count uint32, fn func()) *Task {
	t := CreateTask(fn, AfterTicks(count))
	l.Add(t)
	return t
}
