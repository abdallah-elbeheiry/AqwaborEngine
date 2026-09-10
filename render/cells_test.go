package render

import "testing"

// cellRecorder stands in for a subcell buffer, so what a grid writes can be
// counted without a GPU.
type cellRecorder struct {
	writes  []int
	bulk    int
	entries map[int]SubcellInstance
}

func newCellRecorder() *cellRecorder {
	return &cellRecorder{entries: map[int]SubcellInstance{}}
}

func (r *cellRecorder) Write(index int, inst *SubcellInstance) {
	r.writes = append(r.writes, index)
	r.entries[index] = *inst
}

func (r *cellRecorder) WriteAll(insts []SubcellInstance) {
	r.bulk++
	for i, in := range insts {
		r.entries[i] = in
	}
}

func cellsForTest(cfg CellsConfig) (*Cells, *cellRecorder) {
	c := NewCells(nil, cfg)
	rec := newCellRecorder()
	c.buf = rec
	c.writePositions()
	rec.bulk = 0
	return c, rec
}

func TestCellsSlotsAreChunkContiguous(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 8, H: 8, Chunk: 4})

	// A chunk's cells occupy one unbroken range, in row order inside it.
	seen := map[int]bool{}
	for y := range 4 {
		for x := range 4 {
			slot := c.slotOf(x, y)
			if slot < 0 || slot >= 16 {
				t.Fatalf("cell %d,%d is at slot %d, outside the first chunk", x, y, slot)
			}
			if seen[slot] {
				t.Fatalf("slot %d is used twice", slot)
			}
			seen[slot] = true
		}
	}

	// The next chunk along starts where the first one ends.
	if got := c.slotOf(4, 0); got != 16 {
		t.Fatalf("the chunk to the right starts at %d, want 16", got)
	}
	// The chunk below starts after the whole first row of chunks.
	if got := c.slotOf(0, 4); got != 32 {
		t.Fatalf("the chunk below starts at %d, want 32", got)
	}
}

func TestCellsWriteOnlyWhatChanged(t *testing.T) {
	c, rec := cellsForTest(CellsConfig{W: 64, H: 64, Chunk: 16})
	materials := make([]uint32, 64*64)
	for i := range materials {
		materials[i] = PaletteOf(i % 8)
	}

	c.TouchAll()
	c.Sync(materials)
	if c.Stats().Written != 64*64 {
		t.Fatalf("TouchAll wrote %d cells, want %d", c.Stats().Written, 64*64)
	}
	rec.writes = rec.writes[:0]

	// A still grid writes nothing.
	c.Sync(materials)
	if got := c.Stats().Written; got != 0 {
		t.Fatalf("a still grid wrote %d cells, want 0", got)
	}

	// One cell changes: one write, at that cell's slot.
	materials[5*64+9] = PaletteOf(3)
	c.Touch(9, 5)
	c.Sync(materials)

	if got := c.Stats().Written; got != 1 {
		t.Fatalf("one changed cell wrote %d, want 1", got)
	}
	slot := c.slotOf(9, 5)
	if got := rec.entries[slot].Palette; got != PaletteOf(3) {
		t.Fatalf("slot %d holds palette %d, want %d", slot, got, PaletteOf(3))
	}
}

func TestCellsTouchIsIdempotentAndBounded(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 16, H: 16, Chunk: 8})
	materials := make([]uint32, 16*16)

	for range 10 {
		c.Touch(3, 3)
	}
	c.Touch(-1, 0) // outside; ignored
	c.Touch(0, 99)

	c.Sync(materials)
	if got := c.Stats().Written; got != 1 {
		t.Fatalf("ten touches of one cell wrote %d, want 1", got)
	}
}

func TestCellsVisibleCoversTheView(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 64, H: 64, Chunk: 16, CellSize: 1})
	area := 16 * 16

	// A view over the top-left chunk only.
	runs := c.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 15, MaxY: 15})
	if len(runs) != 1 {
		t.Fatalf("runs = %v, want one chunk", runs)
	}
	if runs[0] != (instRange{First: 0, Count: area}) {
		t.Fatalf("run = %+v, want the first chunk", runs[0])
	}

	// A view two chunks wide and two tall: one run per row of chunks.
	runs = c.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 31, MaxY: 31})
	if len(runs) != 2 {
		t.Fatalf("runs = %v, want one per chunk row", runs)
	}
	for _, r := range runs {
		if r.Count != 2*area {
			t.Fatalf("run covers %d cells, want %d", r.Count, 2*area)
		}
	}

	// An unbounded view covers everything.
	runs = c.visible(ViewBounds{})
	covered := 0
	for _, r := range runs {
		covered += r.Count
	}
	if covered != 64*64 {
		t.Fatalf("an unbounded view covers %d cells of %d", covered, 64*64)
	}
}

func TestCellsVisibleClampsToTheGrid(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 32, H: 32, Chunk: 16, CellSize: 1})

	// Far outside, in both directions: still clamped to a real chunk rather
	// than reading past the buffer.
	for _, v := range []ViewBounds{
		{MinX: -1000, MinY: -1000, MaxX: -900, MaxY: -900},
		{MinX: 900, MinY: 900, MaxX: 1000, MaxY: 1000},
	} {
		for _, r := range c.visible(v) {
			if r.First < 0 || r.First+r.Count > 32*32 {
				t.Fatalf("view %+v gives range %+v, outside the buffer", v, r)
			}
		}
	}
}

func TestCellsRefusesAShortArray(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 8, H: 8, Chunk: 4})
	c.TouchAll()
	c.Sync(make([]uint32, 10)) // too small
	if got := c.Stats().Written; got != 0 {
		t.Fatalf("a short array wrote %d cells, want none", got)
	}
}

// A grid that is not a multiple of the chunk size is padded, not truncated:
// every cell has a slot inside the buffer, and the slots past the edge draw
// nothing.
func TestCellsHandleARaggedGrid(t *testing.T) {
	const w, h, chunk = 50, 30, 16
	c, rec := cellsForTest(CellsConfig{W: w, H: h, Chunk: chunk})

	for y := range h {
		for x := range w {
			if slot := c.slotOf(x, y); slot < 0 || slot >= c.slots {
				t.Fatalf("cell %d,%d is at slot %d, outside a buffer of %d", x, y, slot, c.slots)
			}
		}
	}

	materials := make([]uint32, w*h)
	for i := range materials {
		materials[i] = PaletteOf(1)
	}
	c.TouchAll()
	c.Sync(materials)

	if got := c.Stats().Written; got != w*h {
		t.Fatalf("wrote %d cells, want %d", got, w*h)
	}

	// The padding is untouched, so it draws nothing.
	painted := 0
	for _, inst := range rec.entries {
		if inst.Palette != 0 {
			painted++
		}
	}
	if painted != w*h {
		t.Fatalf("%d slots carry a colour, want %d: the padding must stay clear", painted, w*h)
	}

	// And a view cannot ask for a range past the buffer.
	for _, r := range c.visible(ViewBounds{MinX: -100, MinY: -100, MaxX: 1000, MaxY: 1000}) {
		if r.First < 0 || r.First+r.Count > c.slots {
			t.Fatalf("range %+v is outside a buffer of %d", r, c.slots)
		}
	}
}
