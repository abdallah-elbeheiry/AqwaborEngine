package render

import (
	"sort"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// The coarse half of "draw only what is on screen", and the half the GPU cannot
// do for you.
//
// A layer's instances live in one buffer, divided into chunks of world space. A
// chunk is a contiguous range of that buffer, so a still chunk is never
// rewritten and never re-walked: the cost of a frame is what moved, not what
// exists. What survives the chunk test is handed to the GPU cull, which throws
// away the instances that fall outside the view but inside a visible chunk.
//
// Chunks are ordered by row and then by column, so the chunks a rectangular
// view covers form one contiguous run per row of chunks. A view is therefore a
// handful of ranges rather than a range per chunk, which is what keeps the draw
// count inside the cull's slots.
//
// Nothing here touches the GPU, so all of it is tested without a device.

// slack is how many spare slots a chunk keeps. Membership changes inside the
// slack are a local write; running out of it is what forces the layer to be
// laid out again.
const slack = 8

type chunkKey struct{ CX, CY int32 }

// gridChunk is one square of world space and the range of the instance buffer
// that holds it. slots is the range: an entity per slot, ecs.NoEntity where the
// slot is free. A free slot holds a zero-scale instance, which both the draw and
// the cull already treat as nothing.
type gridChunk struct {
	key   chunkKey
	slots []ecs.Entity
	used  int
	start int

	// The chunk's own bounds, which is what the view is tested against. It
	// grows with what is placed in it and is only tightened by a relayout,
	// because shrinking it per removal would cost a walk of the chunk.
	minX, minY, maxX, maxY float32
}

func (c *gridChunk) empty() bool { return c.used == 0 }

func (c *gridChunk) cover(x, y, w, h float32) {
	hw, hh := w/2, h/2
	if x-hw < c.minX {
		c.minX = x - hw
	}
	if x+hw > c.maxX {
		c.maxX = x + hw
	}
	if y-hh < c.minY {
		c.minY = y - hh
	}
	if y+hh > c.maxY {
		c.maxY = y + hh
	}
}

// home is where an entity's instance lives: which chunk, and which slot of it.
// The chunk is named by key rather than by index because inserting a chunk
// shifts every index after it.
type home struct {
	key  chunkKey
	slot int
}

// grid is one layer's chunking. It maps entities to slots in the layer's
// instance buffer and answers which ranges of that buffer a view can see.
type grid struct {
	size   float32
	chunks []gridChunk
	byKey  map[chunkKey]int
	homes  map[ecs.Entity]home

	// total is how many slots the layer's buffer holds, free ones included.
	total int
	// stale is set when the ranges no longer describe the buffer, which means
	// every instance has to be written again rather than only what changed.
	stale bool
}

func newGrid(size float32) *grid {
	if size <= 0 {
		size = 64
	}
	return &grid{
		size:  size,
		byKey: make(map[chunkKey]int),
		homes: make(map[ecs.Entity]home),
	}
}

func (g *grid) keyFor(x, y float32) chunkKey {
	return chunkKey{CX: floorDiv(x, g.size), CY: floorDiv(y, g.size)}
}

// floorDiv rounds towards minus infinity, so a world coordinate left of the
// origin lands in the chunk left of it rather than in the same one as its
// mirror.
func floorDiv(v, size float32) int32 {
	q := v / size
	i := int32(q)
	if q < 0 && float32(i) != q {
		i--
	}
	return i
}

// place puts an entity at the position given and returns the slot in the
// layer's instance buffer that holds it.
//
// An entity that has not moved out of its chunk keeps its slot, which is what
// makes a moving entity cost one instance write. Crossing a chunk boundary
// costs a slot in the new chunk, and only running out of slack costs a
// relayout.
func (g *grid) place(e ecs.Entity, x, y, w, h float32) int {
	key := g.keyFor(x, y)

	if prev, ok := g.homes[e]; ok {
		if prev.key == key {
			c := &g.chunks[g.byKey[key]]
			c.cover(x, y, w, h)
			return c.start + prev.slot
		}
		g.take(e, prev)
	}

	ci := g.chunkFor(key)
	c := &g.chunks[ci]
	slot, ok := c.free()
	if !ok {
		// Grow the way a slice does. A fixed step meant a chunk holding 256
		// cells took a relayout - every instance in the layer written - each
		// time one more entity wandered into it. Measured on the demo: 10,264
		// instances written on such a frame against 64 on the others.
		by := max(slack, len(c.slots)/4)
		slot = len(c.slots)
		c.slots = append(c.slots, make([]ecs.Entity, by)...)
		for i := slot; i < len(c.slots); i++ {
			c.slots[i] = ecs.NoEntity
		}
		g.total += by
		g.stale = true
	}
	c.slots[slot] = e
	c.used++
	c.cover(x, y, w, h)
	g.homes[e] = home{key: key, slot: slot}
	return c.start + slot
}

// remove takes an entity out of the grid. Its slot stays as a free one, so a
// removal is a zero-scale write rather than a relayout.
func (g *grid) remove(e ecs.Entity) (int, bool) {
	h, ok := g.homes[e]
	if !ok {
		return 0, false
	}
	idx := g.chunks[g.byKey[h.key]].start + h.slot
	g.take(e, h)
	return idx, true
}

func (g *grid) take(e ecs.Entity, h home) {
	ci, ok := g.byKey[h.key]
	if !ok {
		return
	}
	c := &g.chunks[ci]
	if h.slot < len(c.slots) && c.slots[h.slot] == e {
		c.slots[h.slot] = ecs.NoEntity
		c.used--
	}
	delete(g.homes, e)
}

func (c *gridChunk) free() (int, bool) {
	for i, e := range c.slots {
		if e == ecs.NoEntity {
			return i, true
		}
	}
	return 0, false
}

// chunkFor returns the chunk for a key, creating it in row-then-column order so
// that a view covers one run of chunks per row.
func (g *grid) chunkFor(key chunkKey) int {
	if i, ok := g.byKey[key]; ok {
		return i
	}

	at := sort.Search(len(g.chunks), func(i int) bool { return !before(g.chunks[i].key, key) })
	c := gridChunk{
		key:   key,
		slots: make([]ecs.Entity, slack),
		start: 0,
		minX:  float32(key.CX) * g.size,
		minY:  float32(key.CY) * g.size,
		maxX:  float32(key.CX+1) * g.size,
		maxY:  float32(key.CY+1) * g.size,
	}
	for i := range c.slots {
		c.slots[i] = ecs.NoEntity
	}

	g.chunks = append(g.chunks, gridChunk{})
	copy(g.chunks[at+1:], g.chunks[at:])
	g.chunks[at] = c

	g.reindex()
	g.total += slack
	g.stale = true
	return at
}

func before(a, b chunkKey) bool {
	if a.CY != b.CY {
		return a.CY < b.CY
	}
	return a.CX < b.CX
}

func (g *grid) reindex() {
	clear(g.byKey)
	for i := range g.chunks {
		g.byKey[g.chunks[i].key] = i
	}
}

// relayout assigns every chunk its range and drops the ones nothing is left in.
// It is what a stale grid needs before its buffer is written again.
func (g *grid) relayout() {
	kept := g.chunks[:0]
	for i := range g.chunks {
		if g.chunks[i].empty() {
			continue
		}
		kept = append(kept, g.chunks[i])
	}
	g.chunks = kept

	at := 0
	for i := range g.chunks {
		g.chunks[i].start = at
		at += len(g.chunks[i].slots)
	}
	g.total = at
	g.reindex()
	g.stale = false
}

// indexOf is where an entity's instance sits in the layer's buffer.
func (g *grid) indexOf(e ecs.Entity) (int, bool) {
	h, ok := g.homes[e]
	if !ok {
		return 0, false
	}
	ci, ok := g.byKey[h.key]
	if !ok {
		return 0, false
	}
	return g.chunks[ci].start + h.slot, true
}

// instRange is a contiguous run of the layer's instance buffer.
type instRange struct {
	First int
	Count int
}

// visible returns the ranges of the buffer a view covers, at most max of them.
//
// Chunks are ordered by row, so the chunks a rectangular view covers are
// already runs; two runs are joined when there are more than max, which draws
// the gap between them as well. That is the cheaper mistake: the instances in
// the gap are thrown away by the GPU cull, while a draw beyond the cull's slots
// is not drawn at all.
func (g *grid) visible(v ViewBounds, max int) []instRange {
	if max < 1 {
		max = 1
	}

	var runs []instRange
	for i := range g.chunks {
		c := &g.chunks[i]
		if c.empty() || !overlaps(c, v) {
			continue
		}
		r := instRange{First: c.start, Count: len(c.slots)}
		if n := len(runs); n > 0 && runs[n-1].First+runs[n-1].Count == r.First {
			runs[n-1].Count += r.Count
			continue
		}
		runs = append(runs, r)
	}

	for len(runs) > max {
		// Join the two neighbours with the smallest gap between them, so the
		// instances drawn and then culled are as few as possible.
		best, bestGap := 0, -1
		for i := 0; i+1 < len(runs); i++ {
			gap := runs[i+1].First - (runs[i].First + runs[i].Count)
			if bestGap < 0 || gap < bestGap {
				best, bestGap = i, gap
			}
		}
		runs[best].Count = runs[best+1].First + runs[best+1].Count - runs[best].First
		runs = append(runs[:best+1], runs[best+2:]...)
	}
	return runs
}

func overlaps(c *gridChunk, v ViewBounds) bool {
	if v.MinX >= v.MaxX || v.MinY >= v.MaxY {
		return true // an empty view means no bound, not nothing visible
	}
	return c.maxX >= v.MinX && c.minX <= v.MaxX && c.maxY >= v.MinY && c.minY <= v.MaxY
}
