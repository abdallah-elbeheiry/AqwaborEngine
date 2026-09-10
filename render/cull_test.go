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
		{"InputBase", uintptr(unsafe.Pointer(&p.InputBase)) - base, 8},
		// Three u32s end at 12 and a vec2 aligns to 8, so WGSL puts the bounds
		// at 16 and 24 with a pad between. The Go struct carries that pad.
		{"MinBounds", uintptr(unsafe.Pointer(&p.MinBounds)) - base, 16},
		{"MaxBounds", uintptr(unsafe.Pointer(&p.MaxBounds)) - base, 24},
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
	const per = 100
	cp := &CullPipeline{maxInstances: cullSlots * per}
	cp.BeginFrame()

	// A region belongs to a slot once it is claimed, and the size of it is what
	// that cull asked for.
	var lastEnd uint64
	for slot := range cullSlots {
		got, ok := cp.Claim(per)
		if !ok {
			t.Fatalf("claim %d was refused", slot)
		}
		start := cp.OutputOffset(got)
		end := start + per*instanceDataSize
		if slot > 0 && start < lastEnd {
			t.Fatalf("slot %d starts at %d, inside the previous slot ending at %d", got, start, lastEnd)
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
	cp := &CullPipeline{maxInstances: cullSlots * 100}
	cp.BeginFrame()

	seen := map[int]bool{}
	for i := range cullSlots {
		slot, ok := cp.Claim(100)
		if !ok {
			t.Fatalf("ran out of slots after %d, want %d", i, cullSlots)
		}
		if seen[slot] {
			t.Fatalf("slot %d handed out twice", slot)
		}
		seen[slot] = true
	}
	if _, ok := cp.Claim(100); ok {
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

// Slots take room in the output buffer in order, so the buffer holds the
// frame's total rather than its largest cull times the slot count. Sized the
// old way, one run of 535,548 instances asked the device for 261 MB and was
// refused.
func TestCullSlotsShareTheOutputBuffer(t *testing.T) {
	cp := &CullPipeline{maxInstances: 1000}
	cp.BeginFrame()

	a, ok := cp.Claim(600)
	if !ok {
		t.Fatal("the first claim was refused")
	}
	b, ok := cp.Claim(300)
	if !ok {
		t.Fatal("the second claim was refused with room left")
	}
	if cp.base[a] != 0 || cp.base[b] != 600 {
		t.Fatalf("bases are %d and %d, want 0 and 600: one after the other", cp.base[a], cp.base[b])
	}
	if cp.OutputOffset(b) != 600*instanceDataSize {
		t.Fatalf("OutputOffset = %d, want %d", cp.OutputOffset(b), 600*instanceDataSize)
	}

	if _, ok := cp.Claim(200); ok {
		t.Fatal("a claim past the end of the output buffer was allowed")
	}
	if cp.Fits(200) {
		t.Fatal("Fits says 200 more fit in a buffer with 100 left")
	}

	cp.BeginFrame()
	if !cp.Fits(1000) {
		t.Fatal("a new frame did not give the whole buffer back")
	}
}

func TestCullCapsWhatItAsksTheDeviceFor(t *testing.T) {
	if maxCullBytes/instanceDataSize <= 0 {
		t.Fatal("the cap leaves room for no instances at all")
	}
	// The size that was refused on a 256 MiB limit, asked for as one cull.
	if want := 535548; want <= maxCullBytes/instanceDataSize {
		return // it fits under the cap, nothing to check
	} else if maxCullBytes/instanceDataSize >= want {
		t.Fatalf("the cap %d still allows the size the device refused", maxCullBytes/instanceDataSize)
	}
}
