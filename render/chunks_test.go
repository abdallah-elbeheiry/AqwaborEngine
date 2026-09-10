package render

import (
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

func TestGridKeepsSlackForMembershipChanges(t *testing.T) {
	g := newGrid(32)

	for i := range slack {
		g.place(ecs.Entity(i+1), 1, 1, 1, 1)
	}
	g.relayout()

	if g.total != slack {
		t.Fatalf("total = %d, want %d: one chunk of slack holds them all", g.total, slack)
	}
	if g.stale {
		t.Fatal("a relayout left the grid stale")
	}

	// One more than the slack grows the chunk, which is what a relayout is for.
	g.place(ecs.Entity(slack+1), 1, 1, 1, 1)
	if !g.stale {
		t.Fatal("growing a chunk past its slack did not mark the grid stale")
	}
}

func TestGridChunksAreOrderedByRow(t *testing.T) {
	g := newGrid(10)

	// Placed out of order, on purpose.
	g.place(1, 25, 25, 1, 1) // chunk 2,2
	g.place(2, 5, 5, 1, 1)   // chunk 0,0
	g.place(3, 15, 5, 1, 1)  // chunk 1,0
	g.place(4, 5, 15, 1, 1)  // chunk 0,1
	g.relayout()

	want := []chunkKey{{0, 0}, {1, 0}, {0, 1}, {2, 2}}
	if len(g.chunks) != len(want) {
		t.Fatalf("chunks = %d, want %d", len(g.chunks), len(want))
	}
	for i, k := range want {
		if g.chunks[i].key != k {
			t.Fatalf("chunk %d is %v, want %v: row then column", i, g.chunks[i].key, k)
		}
	}

	at := 0
	for i := range g.chunks {
		if g.chunks[i].start != at {
			t.Fatalf("chunk %d starts at %d, want %d", i, g.chunks[i].start, at)
		}
		at += len(g.chunks[i].slots)
	}
}

func TestGridNegativeCoordinatesGetTheirOwnChunk(t *testing.T) {
	g := newGrid(10)

	g.place(1, -5, -5, 1, 1)
	g.place(2, 5, 5, 1, 1)

	if g.homes[1].key == g.homes[2].key {
		t.Fatalf("-5 and 5 landed in the same chunk %v; the divide has to round down", g.homes[1].key)
	}
	if got := g.homes[1].key; got != (chunkKey{-1, -1}) {
		t.Fatalf("-5,-5 is in chunk %v, want -1,-1", got)
	}
}

func TestGridVisibleIsOneRunPerRow(t *testing.T) {
	g := newGrid(10)

	// Three by three chunks, one entity each.
	var id ecs.Entity
	for cy := range 3 {
		for cx := range 3 {
			id++
			g.place(id, float32(cx)*10+5, float32(cy)*10+5, 1, 1)
		}
	}
	g.relayout()

	// A view over the left two columns of every row: three runs, one a row.
	runs := g.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 19, MaxY: 29}, 8)
	if len(runs) != 3 {
		t.Fatalf("runs = %d (%v), want 3, one per row of chunks", len(runs), runs)
	}
	for _, r := range runs {
		if r.Count != 2*slack {
			t.Fatalf("run covers %d instances, want %d: two chunks of a row", r.Count, 2*slack)
		}
	}
}

func TestGridVisibleJoinsRunsPastTheLimit(t *testing.T) {
	g := newGrid(10)

	// Two rows of three chunks. A view over the outer columns leaves a hole in
	// the middle of each row, so the runs are not adjacent and cannot merge on
	// their own.
	var id ecs.Entity
	for cy := range 2 {
		for cx := range 3 {
			id++
			g.place(id, float32(cx)*10+5, float32(cy)*10+5, 1, 1)
		}
	}
	g.relayout()

	// Everything is visible here, so the two rows are one run between them.
	if got := g.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 29, MaxY: 19}, 8); len(got) != 1 {
		t.Fatalf("runs = %d (%v), want 1: every chunk is visible and they are contiguous", len(got), got)
	}

	// Now a view that skips the middle column of each row.
	left := g.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 9, MaxY: 19}, 8)
	if len(left) != 2 {
		t.Fatalf("runs = %d (%v), want 2: one chunk in each of two rows", len(left), left)
	}

	joined := g.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 9, MaxY: 19}, 1)
	if len(joined) != 1 {
		t.Fatalf("runs = %d, want 1 once the limit is 1", len(joined))
	}
	if joined[0].Count < left[0].Count+left[1].Count {
		t.Fatalf("the joined run covers %d, want at least %d: joining draws the gap too, never less",
			joined[0].Count, left[0].Count+left[1].Count)
	}
}

func TestGridVisibleSkipsWhatIsOffScreen(t *testing.T) {
	g := newGrid(10)

	g.place(1, 5, 5, 1, 1)
	g.place(2, 105, 105, 1, 1)
	g.relayout()

	runs := g.visible(ViewBounds{MinX: 0, MinY: 0, MaxX: 9, MaxY: 9}, 8)
	if len(runs) != 1 {
		t.Fatalf("runs = %d (%v), want 1: the far chunk is off screen", len(runs), runs)
	}
	if runs[0].First != 0 || runs[0].Count != slack {
		t.Fatalf("run = %+v, want the first chunk", runs[0])
	}
}

func TestGridEmptyViewDrawsEverything(t *testing.T) {
	g := newGrid(10)
	g.place(1, 5, 5, 1, 1)
	g.place(2, 105, 105, 1, 1)
	g.relayout()

	runs := g.visible(ViewBounds{}, 8)
	covered := 0
	for _, r := range runs {
		covered += r.Count
	}
	if covered != g.total {
		t.Fatalf("an unbounded view covers %d of %d instances, want all", covered, g.total)
	}
}

func TestGridDropsEmptyChunks(t *testing.T) {
	g := newGrid(10)
	g.place(1, 5, 5, 1, 1)
	g.place(2, 105, 105, 1, 1)
	g.relayout()

	g.remove(1)
	g.relayout()

	if len(g.chunks) != 1 {
		t.Fatalf("chunks = %d, want 1: an empty chunk is not kept", len(g.chunks))
	}
	if g.chunks[0].key != (chunkKey{10, 10}) {
		t.Fatalf("the surviving chunk is %v, want 10,10", g.chunks[0].key)
	}
	if idx, ok := g.indexOf(2); !ok || idx != 0 {
		t.Fatalf("the survivor is at %d (%v), want 0 after the layout closed the gap", idx, ok)
	}
}
