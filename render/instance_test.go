package render

import (
	"testing"
	"unsafe"
)

func TestInstanceDataStride(t *testing.T) {
	if got := unsafe.Sizeof(InstanceData{}); got != instanceDataSize {
		t.Fatalf("sizeof(InstanceData) = %d, want %d", got, instanceDataSize)
	}
	if instanceDataSize != 64 {
		t.Fatalf("instanceDataSize = %d, want 64 (WGSL storage stride)", instanceDataSize)
	}
	if off := unsafe.Offsetof(InstanceData{}.Color); off != 32 {
		t.Fatalf("Color offset = %d, want 32", off)
	}
	if off := unsafe.Offsetof(InstanceData{}.Layer); off != 56 {
		t.Fatalf("Layer offset = %d, want 56", off)
	}
	if InstanceBufferLayout.ArrayStride != instanceDataSize {
		t.Fatalf("layout stride = %d, want %d", InstanceBufferLayout.ArrayStride, instanceDataSize)
	}
	// Vertex offsets must match the Go struct.
	want := map[uint32]uint32{2: 0, 3: 8, 4: 16, 5: 32, 6: 48, 7: 56}
	for _, a := range InstanceBufferLayout.Attributes {
		if w, ok := want[a.ShaderLocation]; ok && uint32(a.Offset) != w {
			t.Fatalf("location %d offset = %d, want %d", a.ShaderLocation, a.Offset, w)
		}
	}
}

func TestMergeRanges(t *testing.T) {
	in := [][2]int{{5, 7}, {0, 2}, {1, 4}, {10, 12}, {11, 15}}
	got := mergeRanges(append([][2]int(nil), in...))
	want := [][2]int{{0, 4}, {5, 7}, {10, 15}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// Adjacent ranges merge.
	got = mergeRanges([][2]int{{0, 2}, {2, 5}})
	if len(got) != 1 || got[0] != [2]int{0, 5} {
		t.Fatalf("adjacent merge got %v", got)
	}

	// Empty.
	if mergeRanges(nil) != nil {
		t.Fatal("empty merge should be nil")
	}
}

func TestIndirectCmdSize(t *testing.T) {
	if got := unsafe.Sizeof(IndirectCmd{}); got != indirectCmdSize {
		t.Fatalf("sizeof(IndirectCmd) = %d, want %d", got, indirectCmdSize)
	}
	if got := unsafe.Sizeof(CullParams{}); got != cullParamsSize {
		t.Fatalf("sizeof(CullParams) = %d, want %d", got, cullParamsSize)
	}
}
