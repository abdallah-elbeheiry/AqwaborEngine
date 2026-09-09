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
