package render

import (
	"testing"
	"unsafe"
)

func TestStrokeSegmentStride(t *testing.T) {
	if got := unsafe.Sizeof(StrokeSegment{}); got != strokeSegmentSize {
		t.Fatalf("sizeof(StrokeSegment) = %d, want %d", got, strokeSegmentSize)
	}
	if strokeSegmentSize != 72 {
		t.Fatalf("strokeSegmentSize = %d, want 72", strokeSegmentSize)
	}
	// Offsets must match WGSL and vertex layout.
	if off := unsafe.Offsetof(StrokeSegment{}.P0); off != 0 {
		t.Fatalf("P0 offset = %d, want 0", off)
	}
	if off := unsafe.Offsetof(StrokeSegment{}.Color0); off != 32 {
		t.Fatalf("Color0 offset = %d, want 32", off)
	}
	if off := unsafe.Offsetof(StrokeSegment{}.Color1); off != 48 {
		t.Fatalf("Color1 offset = %d, want 48", off)
	}
	if off := unsafe.Offsetof(StrokeSegment{}.Width); off != 64 {
		t.Fatalf("Width offset = %d, want 64", off)
	}
	if off := unsafe.Offsetof(StrokeSegment{}.Flags); off != 68 {
		t.Fatalf("Flags offset = %d, want 68", off)
	}
	// Layout stride must match.
	if StrokeSegmentLayout.ArrayStride != strokeSegmentSize {
		t.Fatalf("layout stride = %d, want %d", StrokeSegmentLayout.ArrayStride, strokeSegmentSize)
	}
	// Vertex attribute offsets.
	want := map[uint32]uint32{0: 0, 1: 8, 2: 16, 3: 24, 4: 32, 5: 48, 6: 64, 7: 68}
	for _, a := range StrokeSegmentLayout.Attributes {
		if w, ok := want[a.ShaderLocation]; ok && uint32(a.Offset) != w {
			t.Fatalf("location %d offset = %d, want %d", a.ShaderLocation, a.Offset, w)
		}
	}
}

func TestBuildSegments(t *testing.T) {
	points := [][2]float32{{0, 0}, {10, 0}, {20, 0}}
	color := [4]float32{1, 0, 0, 1}
	segs := BuildSegments(points, color, 2.0, WidthModePixels, 0)

	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}

	// First segment: prev duplicates p0 (start cap).
	s0 := segs[0]
	if s0.P0 != [2]float32{0, 0} || s0.P1 != [2]float32{10, 0} {
		t.Fatalf("seg0 endpoints: got %v, %v", s0.P0, s0.P1)
	}
	if s0.Prev != s0.P0 {
		t.Fatalf("seg0 prev should duplicate p0 for start cap")
	}
	if s0.Next != [2]float32{20, 0} {
		t.Fatalf("seg0 next = %v, want [20 0]", s0.Next)
	}

	// Second segment: next duplicates p1 (end cap).
	s1 := segs[1]
	if s1.P0 != [2]float32{10, 0} || s1.P1 != [2]float32{20, 0} {
		t.Fatalf("seg1 endpoints: got %v, %v", s1.P0, s1.P1)
	}
	if s1.Prev != [2]float32{0, 0} {
		t.Fatalf("seg1 prev = %v, want [0 0]", s1.Prev)
	}
	if s1.Next != s1.P1 {
		t.Fatalf("seg1 next should duplicate p1 for end cap")
	}

	// All segments have the same color and width.
	for i, s := range segs {
		if s.Color0 != color || s.Color1 != color {
			t.Fatalf("seg%d color mismatch", i)
		}
		if s.Width != 2.0 {
			t.Fatalf("seg%d width = %f, want 2.0", i, s.Width)
		}
	}
}

func TestBuildSegmentsTooFew(t *testing.T) {
	segs := BuildSegments(nil, [4]float32{}, 1, 0, 0)
	if segs != nil {
		t.Fatal("nil points should return nil")
	}
	segs = BuildSegments([][2]float32{{0, 0}}, [4]float32{}, 1, 0, 0)
	if segs != nil {
		t.Fatal("single point should return nil")
	}
}

func TestStrokeBufferWriteAll(t *testing.T) {
	sb := &StrokeBuffer{
		cpuData:  make([]StrokeSegment, 16),
		capacity: 16,
	}
	segs := []StrokeSegment{
		{Width: 1}, {Width: 2}, {Width: 3},
	}
	sb.WriteAll(segs)
	if sb.count != 3 {
		t.Fatalf("count = %d, want 3", sb.count)
	}
	if sb.cpuData[2].Width != 3 {
		t.Fatal("WriteAll did not copy data")
	}
	if len(sb.dirtyRanges) != 1 || sb.dirtyRanges[0] != [2]int{0, 3} {
		t.Fatalf("dirty = %v, want [0 3]", sb.dirtyRanges)
	}
}

func TestStrokeBufferWrite(t *testing.T) {
	sb := &StrokeBuffer{
		cpuData:  make([]StrokeSegment, 16),
		capacity: 16,
	}
	sb.Write(5, &StrokeSegment{Width: 7})
	if sb.count != 6 {
		t.Fatalf("count = %d, want 6", sb.count)
	}
	if sb.cpuData[5].Width != 7 {
		t.Fatal("Write did not store data")
	}
	if len(sb.dirtyRanges) != 1 || sb.dirtyRanges[0] != [2]int{5, 6} {
		t.Fatalf("dirty = %v, want [5 6]", sb.dirtyRanges)
	}
}

func TestMergeStrokeRanges(t *testing.T) {
	in := [][2]int{{5, 7}, {0, 2}, {1, 4}, {10, 12}, {11, 15}}
	got := mergeStrokeRanges(append([][2]int(nil), in...))
	want := [][2]int{{0, 4}, {5, 7}, {10, 15}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSimplifyPolyline(t *testing.T) {
	// Points within minSegPx should be skipped.
	coords := []int32{0, 0, 1, 0, 2, 0, 100, 0}
	points := SimplifyPolyline(coords, false, 5)
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2 (0,0 and 100,0)", len(points))
	}
	if points[0] != [2]float32{0, 0} || points[1] != [2]float32{100, 0} {
		t.Fatalf("points = %v", points)
	}
}
