package schedulers

import (
	"sync"
	"time"
)

type TickState struct {
	Tick      uint64
	DeltaTime float64
}

type rateGroup struct {
	hz       float64
	interval float64
	delta    float64
	fns      []func(TickState)
	tick     uint64
	accum    float64
}

type Scheduler struct {
	mu      sync.Mutex
	groups  map[float64]*rateGroup
	running bool
	paused  bool
	speed   float64
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		groups: make(map[float64]*rateGroup),
		speed:  1.0,
	}
}

func (s *Scheduler) Run(fn func(TickState), hz float64) {
	if hz <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	g, ok := s.groups[hz]
	if !ok {
		g = &rateGroup{
			hz:       hz,
			interval: 1.0 / hz,
			delta:    1.0 / hz,
			fns:      make([]func(TickState), 0, 8),
		}
		s.groups[hz] = g
	}
	g.fns = append(g.fns, fn)
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.paused = false
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.run()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.mu.Unlock()

	close(stopCh)
	<-doneCh
}

func (s *Scheduler) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

func (s *Scheduler) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
}

func (s *Scheduler) SetSpeed(speed float64) {
	if speed < 0 {
		speed = 0
	}
	s.mu.Lock()
	s.speed = speed
	s.mu.Unlock()
}

func (s *Scheduler) Speed() float64 {
	s.mu.Lock()
	speed := s.speed
	s.mu.Unlock()
	return speed
}

func (s *Scheduler) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	last := time.Now()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			wallDT := now.Sub(last).Seconds()
			last = now

			s.mu.Lock()
			if !s.running || s.paused || s.speed == 0 {
				s.mu.Unlock()
				continue
			}
			speed := s.speed
			groups := make([]*rateGroup, 0, len(s.groups))
			for _, g := range s.groups {
				groups = append(groups, g)
			}
			s.mu.Unlock()

			simDT := wallDT * speed

			for _, g := range groups {
				g.accum += simDT
				for g.accum >= g.interval {
					state := TickState{
						Tick:      g.tick,
						DeltaTime: g.delta,
					}
					for _, fn := range g.fns {
						fn(state)
					}
					g.tick++
					g.accum -= g.interval
				}
			}
		}
	}
}
