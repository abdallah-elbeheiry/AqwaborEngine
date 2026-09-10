package render

import (
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// recorder stands in for a sprite batch, so what a scene writes can be counted
// without a GPU.
type recorder struct {
	writes  []int
	bulk    int
	entries map[int]InstanceData
}

func newRecorder() *recorder {
	return &recorder{entries: map[int]InstanceData{}}
}

func (r *recorder) Set(index int, inst InstanceData) {
	r.writes = append(r.writes, index)
	r.entries[index] = inst
}

func (r *recorder) SetAll(insts []InstanceData) {
	r.bulk++
	for i, in := range insts {
		r.entries[i] = in
	}
}

func (r *recorder) reset() { r.writes = r.writes[:0]; r.bulk = 0 }

// sceneForTest builds a scene whose layers write to recorders instead of to GPU
// buffers. Everything but Draw works without a device.
func sceneForTest(t *testing.T, cfg SceneConfig) (*Scene, *ecs.World, []*recorder) {
	t.Helper()
	w := ecs.NewWorld()
	comps := MustRegisterECS(w)
	s := NewScene(nil, w, comps, cfg)

	recs := make([]*recorder, len(s.layers))
	for i, l := range s.layers {
		recs[i] = newRecorder()
		l.sink = recs[i]
	}
	return s, w, recs
}

func TestSceneWritesOnlyWhatChanged(t *testing.T) {
	s, _, recs := sceneForTest(t, SceneConfig{ChunkSize: 32})

	var moved ecs.Entity
	for i := range 100 {
		e := s.Spawn(Transform{X: float32(i), Y: 0, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
		if i == 7 {
			moved = e
		}
	}
	s.Sync()
	recs[0].reset()

	// A still world: nothing was touched, so nothing is written.
	s.Sync()
	if got := s.Stats().Written; got != 0 {
		t.Fatalf("a still world wrote %d instances, want 0", got)
	}
	if len(recs[0].writes) != 0 {
		t.Fatalf("a still world made %d writes, want 0", len(recs[0].writes))
	}

	// One entity moves inside its chunk: one write, no relayout.
	tr, _ := s.comps.Transform.Get(moved)
	tr.X += 0.5
	s.comps.Transform.Wake(moved)
	s.Sync()

	if got := s.Stats().Written; got != 1 {
		t.Fatalf("one moved entity wrote %d instances, want 1", got)
	}
	if got := s.Stats().Rebuilt; got != 0 {
		t.Fatalf("one moved entity rebuilt %d layers, want 0", got)
	}
}

func TestSceneKeepsSlotAcrossAChunk(t *testing.T) {
	s, _, _ := sceneForTest(t, SceneConfig{ChunkSize: 32})

	// A second entity holds the first chunk open, so leaving it is a move
	// between chunks rather than the chunk being dropped as empty.
	s.Spawn(Transform{X: 2, Y: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	e := s.Spawn(Transform{X: 1, Y: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Sync()
	first, ok := s.layers[0].grid.indexOf(e)
	if !ok {
		t.Fatal("the entity has no slot")
	}

	// Moving within the chunk keeps the slot.
	tr, _ := s.comps.Transform.Get(e)
	tr.X = 30
	s.comps.Transform.Wake(e)
	s.Sync()
	if got, _ := s.layers[0].grid.indexOf(e); got != first {
		t.Fatalf("slot moved to %d inside one chunk, want %d", got, first)
	}

	// Crossing into the next chunk takes a slot there.
	tr, _ = s.comps.Transform.Get(e)
	tr.X = 40
	s.comps.Transform.Wake(e)
	s.Sync()
	if got, _ := s.layers[0].grid.indexOf(e); got == first {
		t.Fatal("crossing a chunk boundary kept the old slot")
	}
	if s.layers[0].grid.homes[e].key.CX != 1 {
		t.Fatalf("entity is in chunk %v, want CX 1", s.layers[0].grid.homes[e].key)
	}
}

func TestSceneDropBlanksTheSlot(t *testing.T) {
	s, _, recs := sceneForTest(t, SceneConfig{ChunkSize: 32})

	e := s.Spawn(Transform{X: 1, Y: 1, SX: 2, SY: 2}, Color{A: 1}, Sprite{})
	s.Sync()
	idx, _ := s.layers[0].grid.indexOf(e)
	recs[0].reset()

	s.Drop(e)
	if got := recs[0].entries[idx]; got.Scale != [2]float32{0, 0} {
		t.Fatalf("dropped slot holds scale %v, want zero so nothing is drawn", got.Scale)
	}
	if _, ok := s.layers[0].grid.indexOf(e); ok {
		t.Fatal("a dropped entity still has a slot")
	}
}

func TestSceneDropBeforeDestroyIsImmediate(t *testing.T) {
	s, w, _ := sceneForTest(t, SceneConfig{ChunkSize: 32})

	e := s.Spawn(Transform{X: 1, Y: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Sync()

	s.Drop(e)
	w.Destroy(e)

	if _, ok := s.layers[0].grid.indexOf(e); ok {
		t.Fatal("a dropped entity is still in the grid")
	}
}

func TestSceneSweepFindsADestroyedEntity(t *testing.T) {
	s, w, recs := sceneForTest(t, SceneConfig{ChunkSize: 32})

	// Several chunks, so the sweep has to come round to the right one.
	var e ecs.Entity
	for i := range 5 {
		got := s.Spawn(Transform{X: float32(i) * 40, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
		if i == 3 {
			e = got
		}
	}
	s.Sync()
	idx, _ := s.layers[0].grid.indexOf(e)

	// Destroyed without a Drop: waking it is refused because its row is gone,
	// so only the sweep can find it.
	w.Destroy(e)
	if s.comps.Transform.Wake(e) {
		t.Fatal("a destroyed entity was woken")
	}

	chunks := len(s.layers[0].grid.chunks)
	for range chunks + 1 {
		s.Sync()
		if _, ok := s.layers[0].grid.indexOf(e); !ok {
			break
		}
	}

	if _, ok := s.layers[0].grid.indexOf(e); ok {
		t.Fatalf("the sweep did not reclaim a destroyed entity in %d syncs", chunks+1)
	}
	if got := recs[0].entries[idx]; got.Scale != [2]float32{0, 0} {
		t.Fatalf("the reclaimed slot still draws: scale %v", got.Scale)
	}
}

func TestSceneLayersAreSeparateBuffers(t *testing.T) {
	s, _, recs := sceneForTest(t, SceneConfig{ChunkSize: 32, Layers: 2})

	s.Spawn(Transform{X: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{Layer: 0})
	back := s.Spawn(Transform{X: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{Layer: 1})
	s.Sync()

	if len(recs[0].entries) == 0 || len(recs[1].entries) == 0 {
		t.Fatalf("layers hold %d and %d instances, want both non-empty", len(recs[0].entries), len(recs[1].entries))
	}
	if s.layerOf[back] != 1 {
		t.Fatalf("entity is on layer %d, want 1", s.layerOf[back])
	}

	// A sprite that changes layer leaves a blank behind in the old one.
	idx, _ := s.layers[1].grid.indexOf(back)
	sp, _ := s.comps.Sprite.Get(back)
	sp.Layer = 0
	s.comps.Transform.Wake(back)
	s.Sync()

	if got := recs[1].entries[idx]; got.Scale != [2]float32{0, 0} {
		t.Fatalf("the old layer still draws the sprite: scale %v", got.Scale)
	}
	if s.layerOf[back] != 0 {
		t.Fatalf("entity is on layer %d after the change, want 0", s.layerOf[back])
	}
}

func TestSceneZeroScaleMeansOne(t *testing.T) {
	s, _, recs := sceneForTest(t, SceneConfig{ChunkSize: 32})

	e := s.Spawn(Transform{X: 3, Y: 4}, Color{R: 1, A: 1}, Sprite{})
	s.Sync()

	idx, _ := s.layers[0].grid.indexOf(e)
	got := recs[0].entries[idx]
	if got.Scale != [2]float32{1, 1} {
		t.Fatalf("scale = %v, want 1,1: a spawn with no size is a unit sprite", got.Scale)
	}
	if got.Position != [2]float32{3, 4} {
		t.Fatalf("position = %v, want 3,4", got.Position)
	}
}

func TestSceneCrossingAChunkLeavesNoGhost(t *testing.T) {
	s, _, recs := sceneForTest(t, SceneConfig{ChunkSize: 32})

	// Both chunks already exist and both keep a resident, so the move creates
	// no chunk and forces no relayout. A relayout rewrites the whole layer and
	// would erase the evidence.
	s.Spawn(Transform{X: 2, Y: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Spawn(Transform{X: 34, Y: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	e := s.Spawn(Transform{X: 10, Y: 10, SX: 5, SY: 5}, Color{A: 1}, Sprite{})
	s.Sync()

	was, _ := s.layers[0].grid.indexOf(e)
	if s.Stats().Rebuilt != 0 {
		// The first Sync always lays the layer out; from here on it must not.
		s.Sync()
	}

	tr, _ := s.comps.Transform.Get(e)
	tr.X = 40 // into the chunk next door, which already exists
	s.comps.Transform.Wake(e)
	s.Sync()

	if got := s.Stats().Rebuilt; got != 0 {
		t.Fatalf("the move caused %d relayouts, so this proves nothing", got)
	}
	now, _ := s.layers[0].grid.indexOf(e)
	if now == was {
		t.Fatal("the entity did not change slot, so this proves nothing")
	}
	if got := recs[0].entries[was]; got.Scale != [2]float32{0, 0} {
		t.Fatalf("the slot it left still draws it: scale %v at %v; the sprite stays frozen where it was",
			got.Scale, got.Position)
	}
}

// Waking is how a scene is told something changed, and Sync consumes it: the
// awake partition of Transform is the redraw set and nothing else.
func TestSceneConsumesTheAwakeSet(t *testing.T) {
	s, _, _ := sceneForTest(t, SceneConfig{ChunkSize: 32})

	a := s.Spawn(Transform{X: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	b := s.Spawn(Transform{X: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})

	if s.comps.Transform.AwakeLen() != 2 {
		t.Fatalf("awake after two spawns = %d, want 2", s.comps.Transform.AwakeLen())
	}

	s.Sync()
	if got := s.comps.Transform.AwakeLen(); got != 0 {
		t.Fatalf("awake after Sync = %d, want 0: Sync consumes what it wrote", got)
	}
	if s.Stats().Written < 2 {
		t.Fatalf("Sync wrote %d instances for two spawns", s.Stats().Written)
	}

	// Waking one entity is one write, and it does not wake the other.
	tr, _ := s.comps.Transform.Get(b)
	tr.X = 5
	s.comps.Transform.Wake(b)
	s.Sync()

	if got := s.Stats().Written; got != 1 {
		t.Fatalf("one woken entity wrote %d instances, want 1", got)
	}
	if s.comps.Transform.Awake(a) {
		t.Fatal("waking one entity woke another")
	}
}
