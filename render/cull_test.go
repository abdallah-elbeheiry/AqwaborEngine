package render

import (
	"testing"
	"unsafe"
)

// The Go struct and the WGSL struct are written separately and nothing in the
// toolchain checks they agree, so this does.
func TestCullParamsLayout(t *testing.T) {
	if got := unsafe.Sizeof(CullParams{}); got != cullParamsSize {
		t.Fatalf("CullParams is %d bytes, the constant says %d", got, cullParamsSize)
	}
	var p CullParams
	base := uintptr(unsafe.Pointer(&p))
	for _, c := range []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"InstanceCount", uintptr(unsafe.Pointer(&p.InstanceCount)) - base, 0},
		{"OutputBase", uintptr(unsafe.Pointer(&p.OutputBase)) - base, 4},
		{"MinBounds", uintptr(unsafe.Pointer(&p.MinBounds)) - base, 8},
		{"MaxBounds", uintptr(unsafe.Pointer(&p.MaxBounds)) - base, 16},
	} {
		if c.off != c.want {
			t.Errorf("%s at %d, want %d; the shader reads it there", c.name, c.off, c.want)
		}
	}
}

func TestIndirectCmdLayout(t *testing.T) {
	if got := unsafe.Sizeof(IndirectCmd{}); got != indirectCmdSize {
		t.Fatalf("IndirectCmd is %d bytes, the constant says %d", got, indirectCmdSize)
	}
	var c IndirectCmd
	base := uintptr(unsafe.Pointer(&c))
	if off := uintptr(unsafe.Pointer(&c.InstanceCount)) - base; off != 4 {
		t.Fatalf("InstanceCount at %d, want 4; the shader adds to it atomically there", off)
	}
}

// Slot regions must not overlap, or one cull's survivors would land in
// another's draw.
func TestCullSlotRegionsAreDisjoint(t *testing.T) {
	cp := &CullPipeline{maxInstances: 1000}

	var lastEnd uint64
	for slot := range cullSlots {
		start := cp.OutputOffset(slot)
		end := start + uint64(cp.maxInstances)*instanceDataSize
		if slot > 0 && start < lastEnd {
			t.Fatalf("slot %d starts at %d, inside the previous slot ending at %d", slot, start, lastEnd)
		}
		lastEnd = end
	}

	seen := map[uint64]bool{}
	for slot := range cullSlots {
		off := cp.IndirectOffset(slot)
		if seen[off] {
			t.Fatalf("slot %d shares an indirect command offset", slot)
		}
		if off%4 != 0 {
			t.Fatalf("indirect offset %d is not a multiple of four, which a draw requires", off)
		}
		seen[off] = true
	}
}

// A uniform binding offset has to be a multiple of 256 on the hardware this
// targets, which is what sets the slot stride.
func TestSlotStrideSatisfiesUniformAlignment(t *testing.T) {
	if slotStride%256 != 0 {
		t.Fatalf("slot stride %d is not a multiple of 256", slotStride)
	}
	if slotStride < cullParamsSize || slotStride < indirectCmdSize {
		t.Fatalf("slot stride %d is smaller than what it has to hold", slotStride)
	}
}

// The frame's slots are handed out once each and then refuse.
func TestCullSlotsAreClaimedOnce(t *testing.T) {
	cp := &CullPipeline{}
	cp.BeginFrame()

	seen := map[int]bool{}
	for i := range cullSlots {
		slot, ok := cp.Claim()
		if !ok {
			t.Fatalf("ran out of slots after %d, want %d", i, cullSlots)
		}
		if seen[slot] {
			t.Fatalf("slot %d handed out twice", slot)
		}
		seen[slot] = true
	}
	if _, ok := cp.Claim(); ok {
		t.Fatal("a slot was handed out past the limit")
	}
	if cp.SlotsLeft() != 0 {
		t.Fatalf("SlotsLeft = %d, want 0", cp.SlotsLeft())
	}

	cp.BeginFrame()
	if cp.SlotsLeft() != cullSlots {
		t.Fatalf("a new frame has %d slots, want %d", cp.SlotsLeft(), cullSlots)
	}
}

func TestCullShaderReadsTheOutputBase(t *testing.T) {
	if !contains(cullWGSL, "outputBase") {
		t.Fatal("the cull shader no longer names outputBase, so slots would share a region")
	}
	if !contains(cullWGSL, "outputInstances[cullParams.outputBase + idx]") {
		t.Fatal("the cull shader does not compact from its slot's base")
	}
}
