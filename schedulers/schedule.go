package schedulers

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// A Schedule runs systems concurrently where their declared access proves it is
// safe, and in order where it does not.
//
// Each system says what it reads and what it writes. Two systems may run at the
// same time when neither writes something the other touches; a write excludes
// every other access to that resource. The scheduler derives that rather than
// the author asserting it, which is what makes the concurrency a proof instead
// of a convention.
//
// This is available here and not in Factorio because lockstep determinism forces
// one fixed serial order, and replay determinism is not a requirement.
//
// What a Resource names is deliberately not a component type. The game this
// engine serves keeps its cell grid in dense arrays outside the entity system,
// and two systems touching that grid must not overlap either. So a Resource is
// any exclusively-written thing: a component type, a grid, a lookup table.

// Resource identifies something systems contend over. Component ids convert
// directly; anything else takes an id from ResourceOf.
type Resource uint32

// resourceNames records a label per resource, for the stage report.
var (
	resourceMu    sync.Mutex
	resourceNames = map[Resource]string{}
	nextResource  = Resource(1 << 24) // above any plausible component id
)

// ResourceOf returns a stable id for a named non-component resource. Calling it
// twice with the same name returns the same id.
func ResourceOf(name string) Resource {
	resourceMu.Lock()
	defer resourceMu.Unlock()
	for id, n := range resourceNames {
		if n == name {
			return id
		}
	}
	id := nextResource
	nextResource++
	resourceNames[id] = name
	return id
}

// NameResource labels a resource that already has an id, such as a component
// type, so a conflict report reads as words rather than numbers.
func NameResource(r Resource, name string) {
	resourceMu.Lock()
	resourceNames[r] = name
	resourceMu.Unlock()
}

func resourceName(r Resource) string {
	resourceMu.Lock()
	defer resourceMu.Unlock()
	if n, ok := resourceNames[r]; ok {
		return n
	}
	return fmt.Sprintf("resource(%d)", uint32(r))
}

// Access is what one system touches. Build it with Reads and Writes.
type Access struct {
	reads  []Resource
	writes []Resource
}

// Reads declares resources a system only reads.
func Reads(rs ...Resource) Access {
	return Access{reads: slices.Clone(rs)}
}

// Writes declares resources a system writes. A write excludes every other
// access to that resource, including another read.
func Writes(rs ...Resource) Access {
	return Access{writes: slices.Clone(rs)}
}

// Reads adds read resources to an existing declaration.
func (a Access) Reads(rs ...Resource) Access {
	a.reads = append(slices.Clone(a.reads), rs...)
	return a
}

// Writes adds written resources to an existing declaration.
func (a Access) Writes(rs ...Resource) Access {
	a.writes = append(slices.Clone(a.writes), rs...)
	return a
}

// conflictsWith reports whether two systems may not run at the same time, and
// names the resource that decides it.
func (a Access) conflictsWith(b Access) (Resource, bool) {
	for _, w := range a.writes {
		if slices.Contains(b.writes, w) || slices.Contains(b.reads, w) {
			return w, true
		}
	}
	for _, w := range b.writes {
		if slices.Contains(a.reads, w) {
			return w, true
		}
	}
	return 0, false
}

// sysEntry is one registered system.
type sysEntry struct {
	name   string
	access Access
	fn     func(TickState)

	// estimate is an exponentially weighted mean of how long the system takes,
	// used to decide whether splitting a stage is worth the cost of splitting.
	estimate time.Duration
}

// DefaultParallelFloor is the estimated stage cost below which a stage runs
// serially.
//
// Handing work to other goroutines costs about 3.3 microseconds before any of it
// happens, measured on an M4 Max, so a stage cheaper than that loses by being
// split. Sixteen cores return around 3.3x on a stage of eight compute-bound
// systems, and around 2.2x on eight systems each walking its own 320 KB array.
//
// Whether memory traffic costs the gain depends on what is shared: splitting one
// array across workers returned nothing in a separate measurement, while systems
// over separate arrays scaled, because those fit in separate caches. So this is
// a floor rather than a rule about which passes are worth splitting; the
// estimate decides that per stage, from what the stage actually cost last time.
const DefaultParallelFloor = 50 * time.Microsecond

// Schedule holds systems and the stages they were grouped into.
type Schedule struct {
	mu       sync.Mutex
	systems  []*sysEntry
	stages   [][]*sysEntry
	built    bool
	floor    time.Duration
	barrier  func()
	parallel bool
}

// NewSchedule returns an empty schedule.
func NewSchedule() *Schedule {
	return &Schedule{floor: DefaultParallelFloor, parallel: true}
}

// SetParallelFloor overrides the estimated stage cost below which a stage runs
// serially.
func (s *Schedule) SetParallelFloor(d time.Duration) {
	s.mu.Lock()
	s.floor = d
	s.mu.Unlock()
}

// SetParallel turns concurrency off, which runs every stage serially in stage
// order. Useful for comparing a result against the concurrent one.
func (s *Schedule) SetParallel(on bool) {
	s.mu.Lock()
	s.parallel = on
	s.mu.Unlock()
}

// SetBarrier registers work to run between stages, where nothing is
// mid-iteration. Applying a world's deferred command buffer belongs here.
func (s *Schedule) SetBarrier(fn func()) {
	s.mu.Lock()
	s.barrier = fn
	s.mu.Unlock()
}

// Add registers a system. Registration order is the intended order: a system
// that conflicts with an earlier one runs after it, and one that conflicts with
// nothing may run alongside anything.
//
// A system interacting with another through something it did not declare is a
// bug in the declaration, not in the schedule.
func (s *Schedule) Add(name string, access Access, fn func(TickState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systems = append(s.systems, &sysEntry{name: name, access: access, fn: fn})
	s.built = false
}

// build groups systems into stages. Each goes into the earliest stage holding
// nothing it conflicts with, which keeps the stage count low; the number of
// stages is what caps the scaling, because each boundary is a synchronisation.
func (s *Schedule) build() {
	s.stages = s.stages[:0]
	for _, sys := range s.systems {
		placed := false
		for i := range s.stages {
			ok := true
			for _, other := range s.stages[i] {
				if _, conflict := sys.access.conflictsWith(other.access); conflict {
					ok = false
					break
				}
			}
			if ok {
				s.stages[i] = append(s.stages[i], sys)
				placed = true
				break
			}
		}
		if !placed {
			s.stages = append(s.stages, []*sysEntry{sys})
		}
	}
	s.built = true
	log.Debug("schedule built", "systems", len(s.systems), "stages", len(s.stages))
}

// Build groups the systems into stages. Run does it as needed; calling it
// explicitly is how a caller gets the report before the first tick.
func (s *Schedule) Build() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.built {
		s.build()
	}
}

// Stages returns the system names per stage, in order. Systems within one stage
// may run at the same time.
func (s *Schedule) Stages() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.built {
		s.build()
	}
	out := make([][]string, len(s.stages))
	for i, stage := range s.stages {
		out[i] = make([]string, len(stage))
		for j, sys := range stage {
			out[i][j] = sys.name
		}
	}
	return out
}

// Run executes one tick: every stage in order, and within a stage everything at
// once when the stage is expensive enough to be worth splitting.
//
// The barrier runs after each stage, which is where deferred structural changes
// land, because no iteration is in progress there.
func (s *Schedule) Run(state TickState) {
	s.mu.Lock()
	if !s.built {
		s.build()
	}
	stages := s.stages
	floor := s.floor
	barrier := s.barrier
	parallel := s.parallel
	s.mu.Unlock()

	for _, stage := range stages {
		s.runStage(stage, state, floor, parallel)
		if barrier != nil {
			barrier()
		}
	}
}

func (s *Schedule) runStage(stage []*sysEntry, state TickState, floor time.Duration, parallel bool) {
	if len(stage) == 0 {
		return
	}
	if len(stage) == 1 || !parallel {
		for _, sys := range stage {
			runSystem(sys, state)
		}
		return
	}

	var estimated time.Duration
	for _, sys := range stage {
		estimated += sys.estimate
	}
	// A stage that has never run has no estimate, so it runs serially once and
	// is measured. Guessing high would spend the split cost to find out.
	if estimated < floor {
		for _, sys := range stage {
			runSystem(sys, state)
		}
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(stage))
	for _, sys := range stage {
		go func(sys *sysEntry) {
			defer wg.Done()
			runSystem(sys, state)
		}(sys)
	}
	wg.Wait()
}

// runSystem calls one system, measuring it and recovering so that one panicking
// system does not take the tick with it.
func runSystem(sys *sysEntry, state TickState) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("system %s panicked on tick %d: %v", sys.name, state.Tick, r)
		}
		// An exponentially weighted mean, weighted toward recent ticks, so a
		// stage that becomes expensive starts being split without a step change
		// making it oscillate.
		d := time.Since(start)
		if sys.estimate == 0 {
			sys.estimate = d
		} else {
			sys.estimate = (sys.estimate*3 + d) / 4
		}
	}()
	sys.fn(state)
}

// Conflicts reports every pair of systems that cannot run together and the
// resource that decides it. It answers why a schedule has more stages than
// expected.
func (s *Schedule) Conflicts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for i := range s.systems {
		for j := i + 1; j < len(s.systems); j++ {
			if r, conflict := s.systems[i].access.conflictsWith(s.systems[j].access); conflict {
				out = append(out, fmt.Sprintf("%s and %s both need %s",
					s.systems[i].name, s.systems[j].name, resourceName(r)))
			}
		}
	}
	return out
}
