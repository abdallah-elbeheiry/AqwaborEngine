package render

import "math"

// Cells draws a fixed grid whose colour per cell is a palette index: the
// compact 16-byte instance, against 64 for a sprite.
//
// It is the layer for the thing a game has most of and moves least. The grid
// does not live in the ECS - a cell is not an entity, it is a row in a dense
// array the game already owns - so this takes that array and a set of what
// changed in it, and turns them into writes.
//
// A cell's slot is a pure function of its coordinates, because cells never move
// between chunks. So there is no arena here, no holes and no relayout: the
// buffer is built once in chunk order and only the palette index of a cell ever
// changes afterwards.
//
//	cells := render.NewCells(gfx, render.CellsConfig{W: 512, H: 512, CellSize: 1})
//	cells.TouchAll()
//	cells.Sync(materials)          // the game's dense array, read not kept
//	cells.Draw(view)
type Cells struct {
	gfx *GPU
	buf cellSink

	w, h   int // cells
	chunk  int // cells a chunk side
	cw, ch int // chunks
	slots  int // instances, which is the chunks padded out

	size             float32
	originX, originY float32

	// dirty is which cells have to be written again. seen dedupes without
	// clearing a map: a cell is listed once however often it is touched.
	dirty []int32
	seen  []bool
	all   bool

	// damaged is the world rectangles the last Sync wrote into, one per cell.
	damaged []ViewBounds

	stats CellStats
}

// CellsConfig is what a grid needs to know that it cannot work out.
type CellsConfig struct {
	// W and H are the grid in cells.
	W, H int

	// CellSize is the world size of one cell. A grid shares it, which is what
	// keeps the instance at 16 bytes: the size is a uniform, not a field.
	// Defaults to 1.
	CellSize float32

	// OriginX and OriginY place cell 0,0 in the world.
	OriginX, OriginY float32

	// Chunk is how many cells a chunk covers on a side. A view draws whole
	// chunks, so smaller means a tighter fit and more draws. Defaults to 32.
	Chunk int
}

// CellStats is what the last Sync and Draw did.
type CellStats struct {
	// Written is cells rewritten by the last Sync. A still grid drives it to
	// zero.
	Written int
	// Submitted is cells the last Draw covered.
	Submitted int
	// Draws is draw calls the last Draw issued.
	Draws int
}

// cellSink is the writing half of a subcell buffer, and it is here for the
// tests rather than for game code: it is unexported and cannot be supplied
// from outside the package. See instanceSink in scene.go.
type cellSink interface {
	Write(index int, inst *SubcellInstance)
	WriteAll(insts []SubcellInstance)
}

// NewCells builds the grid's buffer with every cell in its place and every
// palette index zero, which draws nothing. Touch what should be visible, or
// TouchAll, and Sync.
func NewCells(gfx *GPU, cfg CellsConfig) *Cells {
	if cfg.Chunk <= 0 {
		cfg.Chunk = 32
	}
	if cfg.CellSize <= 0 {
		cfg.CellSize = 1
	}

	c := &Cells{
		gfx:     gfx,
		w:       max(cfg.W, 0),
		h:       max(cfg.H, 0),
		chunk:   cfg.Chunk,
		size:    cfg.CellSize,
		originX: cfg.OriginX,
		originY: cfg.OriginY,
	}
	c.cw = (c.w + c.chunk - 1) / c.chunk
	c.ch = (c.h + c.chunk - 1) / c.chunk
	c.seen = make([]bool, c.w*c.h)

	// The buffer holds whole chunks, so a grid that is not a multiple of the
	// chunk size is padded rather than truncated. The slots past the edge are
	// never written and carry palette zero, which draws nothing.
	c.slots = c.cw * c.ch * c.chunk * c.chunk

	if gfx != nil {
		c.buf = gfx.Subcells(c.slots)
		gfx.SetCellSize(c.size, c.size)
	}
	c.writePositions()
	return c
}

// writePositions lays the grid out in chunk order and gives every cell its
// place. Only the palette index changes after this.
func (c *Cells) writePositions() {
	if c.buf == nil || c.w == 0 || c.h == 0 {
		return
	}
	grid := make([]SubcellInstance, c.slots)
	for y := range c.h {
		for x := range c.w {
			grid[c.slotOf(x, y)] = SubcellInstance{
				X: c.originX + (float32(x)+0.5)*c.size,
				Y: c.originY + (float32(y)+0.5)*c.size,
			}
		}
	}
	c.buf.WriteAll(grid)
}

// slotOf is where a cell's instance lives: its chunk's range, then its place
// inside that chunk. A chunk is contiguous, which is what lets a view draw one
// range per chunk rather than one per cell.
func (c *Cells) slotOf(x, y int) int {
	cx, cy := x/c.chunk, y/c.chunk
	chunkStart := (cy*c.cw + cx) * c.chunk * c.chunk
	return chunkStart + (y%c.chunk)*c.chunk + (x % c.chunk)
}

// Touch records that a cell's material changed and its instance has to be
// written again. Out-of-range coordinates are ignored.
func (c *Cells) Touch(x, y int) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	i := y*c.w + x
	if c.seen[i] {
		return
	}
	c.seen[i] = true
	c.dirty = append(c.dirty, int32(i))
}

// TouchAll records the whole grid, which is what a load or a teleport needs.
func (c *Cells) TouchAll() { c.all = true }

// Sync writes the cells that changed. materials is the game's dense array of
// palette values, one per cell in row-major order: it is read and neither kept
// nor written.
//
// A value of zero draws nothing, so a cleared cell needs no special case. Use
// PaletteOf to turn a material into the value a cell carries.
func (c *Cells) Sync(materials []uint32) {
	c.stats.Written = 0
	c.damaged = c.damaged[:0]
	if c.buf == nil || len(materials) < c.w*c.h {
		if len(materials) < c.w*c.h {
			log.Error("the materials array is smaller than the grid",
				"cells", c.w*c.h, "materials", len(materials))
		}
		return
	}

	if c.all {
		for y := range c.h {
			for x := range c.w {
				c.write(x, y, materials[y*c.w+x])
			}
		}
		c.all = false
		c.clearDirty()
		return
	}

	for _, i := range c.dirty {
		x, y := int(i)%c.w, int(i)/c.w
		c.write(x, y, materials[i])
	}
	c.clearDirty()
}

// damage records the world rectangle of the cell itself. A cell is small and a
// chunk is not, so reporting the chunk would claim a thousand cells changed
// when one did.
func (c *Cells) damage(x, y int) {
	c.damaged = append(c.damaged, ViewBounds{
		MinX: c.originX + float32(x)*c.size,
		MinY: c.originY + float32(y)*c.size,
		MaxX: c.originX + float32(x+1)*c.size,
		MaxY: c.originY + float32(y+1)*c.size,
	})
}

// Damaged is the world rectangles the last Sync wrote into, one per chunk.
func (c *Cells) Damaged() []ViewBounds { return c.damaged }

func (c *Cells) write(x, y int, palette uint32) {
	inst := SubcellInstance{
		X:       c.originX + (float32(x)+0.5)*c.size,
		Y:       c.originY + (float32(y)+0.5)*c.size,
		Palette: palette,
	}
	c.buf.Write(c.slotOf(x, y), &inst)
	c.damage(x, y)
	c.stats.Written++
}

func (c *Cells) clearDirty() {
	for _, i := range c.dirty {
		c.seen[i] = false
	}
	c.dirty = c.dirty[:0]
}

// Draw submits the chunks a view covers. Call it between Begin and End, with
// the camera already set.
//
// There is no per-cell cull: a cell is 16 bytes and the chunk test is what
// keeps the count down. The sprite path culls per instance because an instance
// there carries a size and can be anywhere; a cell cannot.
func (c *Cells) Draw(view ViewBounds) {
	c.stats.Submitted = 0
	c.stats.Draws = 0
	if c.buf == nil || c.w == 0 || c.h == 0 {
		return
	}

	batch, ok := c.buf.(*SubcellBuffer)
	if !ok {
		return
	}
	for _, r := range c.visible(view) {
		c.gfx.DrawSubcellsRange(batch, r.First, r.Count)
		c.stats.Submitted += r.Count
		c.stats.Draws++
	}
}

// visible is the ranges of the buffer a view covers: whole chunks, merged where
// they are next to each other in the buffer, which is along a row of chunks.
func (c *Cells) visible(v ViewBounds) []instRange {
	minCX, minCY := 0, 0
	maxCX, maxCY := c.cw-1, c.ch-1

	if v.MinX < v.MaxX && v.MinY < v.MaxY {
		minCX = c.chunkAt(v.MinX-c.originX, c.cw)
		maxCX = c.chunkAt(v.MaxX-c.originX, c.cw)
		minCY = c.chunkAt(v.MinY-c.originY, c.ch)
		maxCY = c.chunkAt(v.MaxY-c.originY, c.ch)
	}
	if minCX > maxCX || minCY > maxCY {
		return nil
	}

	area := c.chunk * c.chunk
	runs := make([]instRange, 0, maxCY-minCY+1)
	for cy := minCY; cy <= maxCY; cy++ {
		first := (cy*c.cw + minCX) * area
		runs = append(runs, instRange{
			First: first,
			Count: (maxCX - minCX + 1) * area,
		})
	}
	return runs
}

// chunkAt turns a world offset into a chunk index, clamped to the grid.
func (c *Cells) chunkAt(offset float32, chunks int) int {
	i := int(math.Floor(float64(offset / (c.size * float32(c.chunk)))))
	if i < 0 {
		return 0
	}
	if i >= chunks {
		return chunks - 1
	}
	return i
}

// Stats is what the last Sync and Draw did.
func (c *Cells) Stats() CellStats { return c.stats }

// Release frees the grid's buffer.
func (c *Cells) Release() {
	if b, ok := c.buf.(*SubcellBuffer); ok && b != nil {
		b.Release()
	}
	c.buf = nil
}
