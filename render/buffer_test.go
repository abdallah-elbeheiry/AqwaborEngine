package render

import (
	"testing"
)

// newCPUBuffer builds an InstanceBuffer with no GPU device, which exercises
// everything up to the upload. Growth needs a device and is covered by the
// no-panic tests below rather than by a real reallocation.
func newCPUBuffer(capacity int) *InstanceBuffer {
	return &InstanceBuffer{
		cpuData:     make([]InstanceData, capacity),
		capacity:    capacity,
		dirtyRanges: make([][2]int, 0, 8),
	}
}

func TestWriteTracksCountAndRanges(t *testing.T) {
	ib := newCPUBuffer(16)

	ib.Write(3, &InstanceData{Position: [2]float32{1, 2}})
	if ib.Count() != 4 {
		t.Fatalf("count = %d after writing index 3, want 4", ib.Count())
	}
	ib.Write(1, &InstanceData{})
	if ib.Count() != 4 {
		t.Fatalf("count = %d after writing a lower index, want it unchanged at 4", ib.Count())
	}

	merged := mergeRanges(append([][2]int(nil), ib.dirtyRanges...))
	if len(merged) != 2 {
		t.Fatalf("dirty ranges merged to %v, want two", merged)
	}
}

func TestWriteAllSetsCount(t *testing.T) {
	ib := newCPUBuffer(16)
	ib.WriteAll(make([]InstanceData, 10))
	if ib.Count() != 10 {
		t.Fatalf("count = %d, want 10", ib.Count())
	}
	ib.Reset()
	if ib.Count() != 0 || len(ib.dirtyRanges) != 0 {
		t.Fatal("reset left state behind")
	}
}

// Writing past capacity used to panic. Without a device it cannot grow either,
// but it must decline rather than take the process down: a renderer that cannot
// allocate should drop a frame, not end the game.
func TestWritingPastCapacityDoesNotPanic(t *testing.T) {
	ib := newCPUBuffer(4)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writing past capacity panicked: %v", r)
		}
	}()

	ib.Write(99, &InstanceData{})
	ib.WriteAll(make([]InstanceData, 64))
	ib.WriteAt(2, make([]InstanceData, 64))
	ib.SetCount(1000)
	if s := InstanceSlice(ib, 1000); s != nil {
		t.Fatal("InstanceSlice handed back a slice it could not back with a buffer")
	}

	if ib.Count() > ib.Capacity() {
		t.Fatalf("count %d exceeds capacity %d, so a flush would read past the array",
			ib.Count(), ib.Capacity())
	}
}

func TestNegativeIndexIsRefused(t *testing.T) {
	ib := newCPUBuffer(4)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a negative index panicked: %v", r)
		}
	}()
	ib.Write(-1, &InstanceData{})
	ib.WriteAt(-5, make([]InstanceData, 2))
	ib.SetCount(-1)
	if ib.Count() != 0 {
		t.Fatalf("count = %d after refused writes, want 0", ib.Count())
	}
}

// The stride guard is what keeps the Go struct and the WGSL layout in step.
func TestInstanceStrideMatchesTheStruct(t *testing.T) {
	if got := int(unsafeSizeofInstanceData()); got != instanceDataSize {
		t.Fatalf("InstanceData is %d bytes, the constant says %d", got, instanceDataSize)
	}
}

// The plan is what decides how many uploads a frame costs, and it is the
// decision worth pinning: a call is expensive, the bytes in a gap are not.
func TestUploadPlanMergesAcrossSmallGaps(t *testing.T) {
	plan := uploadPlan([][2]int{{0, 1}, {gapMerge, gapMerge + 1}})
	if len(plan) != 1 {
		t.Fatalf("plan = %v, want one upload: the gap is inside the threshold", plan)
	}
	if plan[0] != [2]int{0, gapMerge + 1} {
		t.Fatalf("merged range = %v, want the span of both", plan[0])
	}

	far := uploadPlan([][2]int{{0, 1}, {gapMerge * 4, gapMerge*4 + 1}})
	if len(far) != 2 {
		t.Fatalf("plan = %v, want two uploads: the gap is wider than the threshold", far)
	}
}

func TestUploadPlanBoundsTheCallCount(t *testing.T) {
	// A thousand single instances, spread far enough apart that nothing merges
	// on distance alone. This is the moving-entity case: 2,000 of these cost
	// 2 ms a frame as separate uploads.
	var ranges [][2]int
	for i := range 1000 {
		at := i * gapMerge * 4
		ranges = append(ranges, [2]int{at, at + 1})
	}

	plan := uploadPlan(ranges)
	if len(plan) > maxUploads {
		t.Fatalf("plan issues %d uploads, want at most %d", len(plan), maxUploads)
	}

	// Every dirty instance still has to be inside some uploaded range.
	for _, want := range ranges {
		covered := false
		for _, r := range plan {
			if want[0] >= r[0] && want[1] <= r[1] {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("instance %v is dirty and in no upload", want)
		}
	}
}

func TestUploadPlanKeepsOneRangeAlone(t *testing.T) {
	plan := uploadPlan([][2]int{{5, 9}})
	if len(plan) != 1 || plan[0] != [2]int{5, 9} {
		t.Fatalf("plan = %v, want the range unchanged", plan)
	}
	if got := uploadPlan(nil); len(got) != 0 {
		t.Fatalf("plan for nothing dirty = %v, want none", got)
	}
}
