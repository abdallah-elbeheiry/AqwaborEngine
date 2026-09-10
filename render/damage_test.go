package render

import (
	"image"
	"testing"
)

func TestScreenRectMapsAWorldRectangle(t *testing.T) {
	// A camera at 100,100, zoom 2, on a 800x600 viewport: the camera's own
	// position is the centre of the screen.
	got := ScreenRect(ViewBounds{MinX: 100, MinY: 100, MaxX: 110, MaxY: 110}, 100, 100, 2, 800, 600, 1)
	if got.Min.X != 400 || got.Min.Y != 300 {
		t.Fatalf("rect starts at %v, want 400,300: the camera is the centre", got.Min)
	}
	if got.Dx() != 20 || got.Dy() != 20 {
		t.Fatalf("rect is %dx%d, want 20x20: ten world units at zoom 2", got.Dx(), got.Dy())
	}
}

func TestScreenRectFollowsTheBackingScale(t *testing.T) {
	one := ScreenRect(ViewBounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, 0, 0, 1, 800, 600, 1)
	two := ScreenRect(ViewBounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, 0, 0, 1, 800, 600, 2)
	if two.Dx() != 2*one.Dx() || two.Dy() != 2*one.Dy() {
		t.Fatalf("at scale 2 the rect is %v, want twice %v", two, one)
	}
}

func TestScreenRectGrowsToWholePixels(t *testing.T) {
	// Half a pixel of coverage still dirties the pixel.
	got := ScreenRect(ViewBounds{MinX: 0.1, MinY: 0.1, MaxX: 0.9, MaxY: 0.9}, 0, 0, 1, 2, 2, 1)
	if got.Dx() < 1 || got.Dy() < 1 {
		t.Fatalf("rect %v collapsed to nothing", got)
	}
}

func TestSceneReportsWhatItWrote(t *testing.T) {
	s, _, _ := sceneForTest(t, SceneConfig{ChunkSize: 32})

	a := s.Spawn(Transform{X: 1, Y: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Spawn(Transform{X: 40, Y: 1, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Sync()

	if len(s.Damaged()) != 2 {
		t.Fatalf("damage = %v, want one rectangle per spawned entity", s.Damaged())
	}

	// A still world damages nothing.
	s.Sync()
	if got := s.Damaged(); len(got) != 0 {
		t.Fatalf("a still world reported damage %v", got)
	}

	// A moved entity damages where it was and where it is, and the rectangles
	// are its own size rather than its chunk's.
	tr, _ := s.comps.Transform.Get(a)
	tr.X = 5
	s.comps.Transform.Wake(a)
	s.Sync()

	d := s.Damaged()
	if len(d) != 2 {
		t.Fatalf("damage = %v, want the old position and the new one", d)
	}
	for _, r := range d {
		if w := r.MaxX - r.MinX; w != 1 {
			t.Fatalf("damaged rect %v is %v wide, want the entity's own size of 1", r, w)
		}
	}
	if d[0].MinX != 0.5 || d[1].MinX != 4.5 {
		t.Fatalf("damage = %v, want it around x=1 then x=5", d)
	}
}

func TestSceneDamagesBothSidesOfAMove(t *testing.T) {
	s, _, _ := sceneForTest(t, SceneConfig{ChunkSize: 32})

	s.Spawn(Transform{X: 2, Y: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Spawn(Transform{X: 40, Y: 2, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	e := s.Spawn(Transform{X: 10, Y: 10, SX: 1, SY: 1}, Color{A: 1}, Sprite{})
	s.Sync()

	tr, _ := s.comps.Transform.Get(e)
	tr.X = 40 // into the chunk next door
	s.comps.Transform.Wake(e)
	s.Sync()

	if got := len(s.Damaged()); got != 2 {
		t.Fatalf("damage covers %d rectangles, want 2: where it was and where it is", got)
	}
}

func TestCellsReportTheCellsTheyWrote(t *testing.T) {
	c, _ := cellsForTest(CellsConfig{W: 64, H: 64, Chunk: 16, CellSize: 1})
	materials := make([]uint32, 64*64)

	c.Touch(1, 1)
	c.Touch(2, 2)
	c.Touch(40, 40)
	c.Sync(materials)

	d := c.Damaged()
	if len(d) != 3 {
		t.Fatalf("damage = %v, want one rectangle per changed cell", d)
	}
	if d[0] != (ViewBounds{MinX: 1, MinY: 1, MaxX: 2, MaxY: 2}) {
		t.Fatalf("first damaged rect = %+v, want the cell at 1,1", d[0])
	}

	c.Sync(materials)
	if got := c.Damaged(); len(got) != 0 {
		t.Fatalf("a still grid reported damage %v", got)
	}
}

func TestMergeRectsKeepsFewEnoughAlone(t *testing.T) {
	in := []image.Rectangle{image.Rect(0, 0, 2, 2), image.Rect(10, 10, 12, 12)}
	if got := MergeRects(in, 4); len(got) != 2 {
		t.Fatalf("merged %v to %v, want them left alone", in, got)
	}
}

func TestMergeRectsBoundsTheCount(t *testing.T) {
	var in []image.Rectangle
	for i := range 500 {
		x := (i * 7) % 1000
		y := (i * 13) % 800
		in = append(in, image.Rect(x, y, x+4, y+4))
	}

	got := MergeRects(in, 16)
	if len(got) > 16 {
		t.Fatalf("merged to %d rectangles, want at most 16", len(got))
	}

	// Nothing dirty may fall outside the result.
	for _, want := range in {
		covered := false
		for _, r := range got {
			if want.In(r) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("rect %v is dirty and outside the merged damage", want)
		}
	}
}

func TestMergeRectsCoversLessThanEverything(t *testing.T) {
	// Two tight clusters far apart: merging must not claim the space between
	// them.
	var in []image.Rectangle
	for i := range 100 {
		in = append(in, image.Rect(i, 0, i+2, 2))
		in = append(in, image.Rect(1000+i, 900, 1002+i, 902))
	}

	got := MergeRects(in, 16)
	area := 0
	for _, r := range got {
		area += r.Dx() * r.Dy()
	}
	whole := image.Rect(0, 0, 1102, 902)
	if area >= whole.Dx()*whole.Dy()/2 {
		t.Fatalf("merged damage covers %d pixels of %d; the gap should not be claimed",
			area, whole.Dx()*whole.Dy())
	}
}
