package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// Scene draws what a world holds. It is the layer between the ECS and the GPU:
// a game spawns entities and writes components, and nothing it touches names a
// buffer, a pipeline or a bind group.
//
// The shape is one instance buffer per layer, divided into chunks of world
// space (see chunks.go). An entity that has not moved is not written and not
// walked; a view is the handful of buffer ranges its chunks cover; and what
// those ranges hold is handed to the GPU cull, which drops the instances that
// are inside a visible chunk but outside the view. Coarse on the CPU, fine on
// the GPU, and never per entity in Go.
//
// What has changed is told, not discovered, and what tells it is the ECS's own
// awake partition: a Transform that is awake is one whose instance has to be
// written again. Sync writes those and puts them back to sleep. A still world
// is a world with nothing awake, and costs nothing per frame.
//
// The scene invents no vocabulary of its own for this. Waking already means
// "this needs visiting" everywhere else in the engine, and a second way of
// saying it is how two subsystems end up disagreeing about what changed.
type Scene struct {
	gfx   *GPU
	world *ecs.World
	comps Components

	layers  []*sceneLayer
	layerOf map[ecs.Entity]int

	// awake is the entities Sync is about to visit, copied out of the store
	// before any of them is slept: sleeping swaps rows, so walking the live
	// partition while emptying it would skip half of them.
	awake []ecs.Entity

	cullFrom int
	maxRuns  int
	stats    SceneStats
}

// SceneConfig is what a scene needs to know that it cannot work out.
type SceneConfig struct {
	// ChunkSize is the side of a chunk in world units. Too small and a view
	// covers many chunks; too large and a chunk is redrawn for one moving
	// entity. Defaults to 64.
	ChunkSize float32

	// Layers is how many draw buckets there are. An entity's bucket is its
	// Sprite.Layer, clamped. Defaults to 1.
	Layers int

	// CullFrom is the instance count from which a range is worth culling on
	// the GPU rather than drawn as it stands. Defaults to 64. A number larger
	// than the world turns the GPU cull off and leaves the chunking.
	CullFrom int

	// MaxRuns is how many ranges a layer may draw in a frame. Runs past it are
	// joined, which draws the gap between them as well. Defaults to the cull's
	// slot count, which is what bounds it in practice. One run means the whole
	// layer in a single draw.
	MaxRuns int
}

// SceneStats is what the last frame did, which is what a change to the scene is
// argued with.
type SceneStats struct {
	// Written is instances rewritten by the last Sync, which is the number a
	// still world drives to zero.
	Written int
	// Rebuilt is layers laid out again by the last Sync, which is the
	// expensive case: every instance in the layer written.
	Rebuilt int
	// Submitted is instances the last Draw covered, before the GPU cull.
	Submitted int
	// Draws is draw calls the last Draw issued.
	Draws int
}

type sceneLayer struct {
	grid  *grid
	batch *SpriteBatch
	// sweep is the chunk liveness is checked in next. One chunk a Sync, so a
	// game that destroys without dropping still gets its slots back, at a cost
	// that does not grow with the world.
	sweep int
	// sink is what instances are written to. It is the batch in a running
	// game; naming it separately is what lets the packing be tested without a
	// GPU, which is most of what this file decides.
	sink    instanceSink
	scratch []InstanceData
}

// instanceSink is the writing half of a sprite batch.
type instanceSink interface {
	Set(index int, inst InstanceData)
	SetAll(insts []InstanceData)
}

// NewScene builds the scene for a world. The component handles are the ones
// RegisterECS returned; a scene reads through them and registers nothing of its
// own.
func NewScene(gfx *GPU, w *ecs.World, comps Components, cfg SceneConfig) *Scene {
	if cfg.Layers < 1 {
		cfg.Layers = 1
	}
	if cfg.CullFrom <= 0 {
		cfg.CullFrom = 64
	}
	if cfg.MaxRuns <= 0 {
		cfg.MaxRuns = cullSlots
	}

	s := &Scene{
		gfx:      gfx,
		world:    w,
		comps:    comps,
		layerOf:  make(map[ecs.Entity]int),
		cullFrom: cfg.CullFrom,
		maxRuns:  cfg.MaxRuns,
	}
	for range cfg.Layers {
		s.layers = append(s.layers, &sceneLayer{grid: newGrid(cfg.ChunkSize)})
	}
	return s
}

// Spawn creates a drawable entity and wakes it, so the next Sync writes it.
//
// It exists because setting a component does not wake it, and an entity that is
// asleep is in nothing the scene walks. That was found from the outside: the
// first game on this engine fell back to visiting every row because of it.
func (s *Scene) Spawn(t Transform, c Color, sp Sprite) ecs.Entity {
	e := s.world.Create()
	s.comps.Transform.Set(e, t)
	s.comps.Color.Set(e, c)
	s.comps.Sprite.Set(e, sp)
	s.comps.Transform.Wake(e)
	return e
}

// Drop takes an entity out of the scene. Its slot is blanked rather than
// reclaimed, so dropping is a write and not a relayout.
//
// Call it before destroying an entity. A destroyed entity cannot be Touched -
// an ecs.Set refuses a handle whose generation has moved on - so the scene has
// no way of being told after the fact. What it has instead is the sweep, which
// finds a dead entity within a bounded number of frames; Drop is what makes it
// immediate.
func (s *Scene) Drop(e ecs.Entity) {
	s.comps.Transform.Sleep(e)
	li, ok := s.layerOf[e]
	if !ok {
		return
	}
	if idx, had := s.layers[li].grid.remove(e); had && s.layers[li].sink != nil {
		s.layers[li].sink.Set(idx, InstanceData{})
		s.stats.Written++
	}
	delete(s.layerOf, e)
}

// Sync writes the awake transforms into the layer buffers and puts them back to
// sleep. Wake an entity when you move, recolour or re-layer it; leave it asleep
// and it keeps the instance it already has.
//
// It reads Transform, Sprite and Color and writes none of them, so a Schedule
// can declare it as Reads(transform, sprite, color) and run it beside anything
// that does not write them. What it does write is the awake partition of
// Transform, which belongs to the renderer.
func (s *Scene) Sync() {
	s.stats.Written = 0
	s.stats.Rebuilt = 0

	s.awake = append(s.awake[:0], s.comps.Transform.Owners()...)
	for _, e := range s.awake {
		s.sync(e)
		s.comps.Transform.Sleep(e)
	}

	for li := range s.layers {
		s.sweepOne(li)
	}

	for li, l := range s.layers {
		if l.grid.stale {
			s.rebuild(li)
			continue
		}
		if len(l.grid.moved) > 0 {
			s.rewriteMoved(li)
		}
	}
}

// rewriteMoved writes the chunks whose range moved this Sync. A chunk that
// outgrew its range took a new one off the end of the arena, so its instances
// are somewhere else in the buffer now and the old range holds them still.
//
// This is the cheap half of what used to be a relayout: a chunk's worth of
// writes rather than a layer's.
func (s *Scene) rewriteMoved(li int) {
	l := s.layers[li]
	s.ensure(li)

	for _, key := range l.grid.moved {
		ci, ok := l.grid.byKey[key]
		if !ok {
			continue
		}
		c := &l.grid.chunks[ci]
		for slot, e := range c.slots {
			if e == ecs.NoEntity {
				l.sink.Set(c.start+slot, InstanceData{})
				s.stats.Written++
				continue
			}
			t, okT := s.comps.Transform.Get(e)
			sp, okS := s.comps.Sprite.Get(e)
			if !okT || !okS {
				l.sink.Set(c.start+slot, InstanceData{})
				s.stats.Written++
				continue
			}
			l.sink.Set(c.start+slot, s.instance(e, *t, *sp))
			s.stats.Written++
		}
	}
	l.grid.moved = l.grid.moved[:0]
}

// sweepOne checks one chunk for entities that have been destroyed, and takes
// them out. Destroying without dropping is the case it covers: the entity is
// gone, its handle can no longer be added to a set, and its instance would
// otherwise keep being drawn until something else moved the layout.
func (s *Scene) sweepOne(li int) {
	l := s.layers[li]
	if len(l.grid.chunks) == 0 {
		return
	}
	if l.sweep >= len(l.grid.chunks) {
		l.sweep = 0
	}
	c := &l.grid.chunks[l.sweep]
	l.sweep++

	for _, e := range c.slots {
		if e == ecs.NoEntity || s.world.Alive(e) {
			continue
		}
		s.Drop(e)
	}
}

func (s *Scene) sync(e ecs.Entity) {
	if !s.world.Alive(e) {
		s.Drop(e)
		return
	}
	t, ok := s.comps.Transform.Get(e)
	if !ok {
		s.Drop(e)
		return
	}
	sp, ok := s.comps.Sprite.Get(e)
	if !ok {
		s.Drop(e)
		return
	}

	li := s.layerFor(*sp)
	if was, ok := s.layerOf[e]; ok && was != li {
		if idx, had := s.layers[was].grid.remove(e); had {
			s.layers[was].sink.Set(idx, InstanceData{})
			s.stats.Written++
		}
	}
	s.layerOf[e] = li

	l := s.layers[li]
	sx, sy := scaleOf(*t)
	idx, vacated, freed := l.grid.place(e, t.X, t.Y, sx, sy)
	s.ensure(li)
	if freed {
		// The slot it left still holds the instance written there, and a free
		// slot nothing claims keeps drawing it: the sprite would stay frozen
		// where it was.
		l.sink.Set(vacated, InstanceData{})
		s.stats.Written++
	}
	l.sink.Set(idx, s.instance(e, *t, *sp))
	s.stats.Written++
}

// rebuild writes a whole layer again, which is what a grid whose ranges moved
// needs. It is the case worth avoiding, and Rebuilt in the stats is how a game
// sees it happening.
func (s *Scene) rebuild(li int) {
	l := s.layers[li]
	l.grid.relayout()
	s.ensure(li)

	if cap(l.scratch) < l.grid.total {
		l.scratch = make([]InstanceData, l.grid.total)
	}
	l.scratch = l.scratch[:l.grid.total]
	clear(l.scratch)

	for ci := range l.grid.chunks {
		c := &l.grid.chunks[ci]
		for slot, e := range c.slots {
			if e == ecs.NoEntity {
				continue
			}
			t, ok := s.comps.Transform.Get(e)
			if !ok {
				continue
			}
			sp, ok := s.comps.Sprite.Get(e)
			if !ok {
				continue
			}
			l.scratch[c.start+slot] = s.instance(e, *t, *sp)
		}
	}

	l.sink.SetAll(l.scratch)
	s.stats.Written += len(l.scratch)
	s.stats.Rebuilt++
}

// Draw submits the layers in order, through the camera already set on the GPU.
//
// It follows the frame's two phases on its own: every cull it needs is encoded
// before the first draw opens the render pass. Call it between Begin and End.
func (s *Scene) Draw(view ViewBounds) {
	s.stats.Submitted = 0
	s.stats.Draws = 0

	type submission struct {
		layer  int
		culled Culled
		direct instRange
	}

	slots := min(cullSlots, s.maxRuns)
	var plan []submission

	for li, l := range s.layers {
		if l.batch == nil || l.grid.total == 0 {
			continue
		}
		for _, r := range l.grid.visible(view, s.maxRuns) {
			s.stats.Submitted += r.Count
			if r.Count >= s.cullFrom && slots > 0 {
				c := s.gfx.CullSpritesRange(l.batch, r.First, r.Count, view)
				if c.ok {
					slots--
					plan = append(plan, submission{layer: li, culled: c})
					continue
				}
			}
			plan = append(plan, submission{layer: li, direct: r})
		}
	}

	for _, p := range plan {
		if p.culled.ok {
			s.gfx.DrawSpritesCulled(p.culled)
		} else {
			s.gfx.DrawSpritesRange(s.layers[p.layer].batch, p.direct.First, p.direct.Count)
		}
		s.stats.Draws++
	}
}

// Stats is what the last Sync and Draw did.
func (s *Scene) Stats() SceneStats { return s.stats }

// GPU is the facade the scene draws through, for the frame around it: Begin,
// SetCamera and End are the caller's, because a frame may hold more than one
// scene.
func (s *Scene) GPU() *GPU { return s.gfx }

// Release frees the layer buffers.
func (s *Scene) Release() {
	for _, l := range s.layers {
		if l.batch != nil {
			l.batch.Release()
			l.batch = nil
		}
	}
}

func (s *Scene) layerFor(sp Sprite) int {
	li := int(sp.Layer)
	if li < 0 {
		return 0
	}
	if li >= len(s.layers) {
		return len(s.layers) - 1
	}
	return li
}

// ensure makes sure the layer has a batch large enough for its grid. Writing
// past a batch grows it, so this is about the first write rather than every
// one.
func (s *Scene) ensure(li int) {
	l := s.layers[li]
	if l.sink != nil {
		return
	}
	l.batch = s.gfx.Sprites(max(l.grid.total, 64))
	l.sink = l.batch
}

func (s *Scene) instance(e ecs.Entity, t Transform, sp Sprite) InstanceData {
	col := Color{R: 1, G: 1, B: 1, A: 1}
	if c, ok := s.comps.Color.Get(e); ok {
		col = *c
	}
	sx, sy := scaleOf(t)
	return InstanceData{
		Position: [2]float32{t.X, t.Y},
		Scale:    [2]float32{sx, sy},
		Rotation: t.Rot,
		Color:    [4]float32{col.R, col.G, col.B, col.A},
		UVOffset: [2]float32{sp.UVX, sp.UVY},
		Layer:    sp.Layer,
	}
}

// scaleOf reads a transform's size, treating a zero scale as one rather than as
// nothing: a spawn that sets a position and no size means a unit sprite.
func scaleOf(t Transform) (float32, float32) {
	sx, sy := t.SX, t.SY
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	return sx, sy
}

// Clamp255 clamps a float32 in [0,1] to a uint32 in [0,255].
func Clamp255(v float32) uint32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint32(v * 255)
}
