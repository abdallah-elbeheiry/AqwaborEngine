package render

import (
	"testing"
	"unsafe"
)

func TestSubcellInstanceLayout(t *testing.T) {
	if got := unsafe.Sizeof(SubcellInstance{}); got != subcellInstanceSize {
		t.Fatalf("SubcellInstance is %d bytes, the constant says %d", got, subcellInstanceSize)
	}
	var s SubcellInstance
	base := uintptr(unsafe.Pointer(&s))
	if off := uintptr(unsafe.Pointer(&s.X)) - base; off != 0 {
		t.Errorf("X at %d, want 0", off)
	}
	if off := uintptr(unsafe.Pointer(&s.Palette)) - base; off != 8 {
		t.Errorf("Palette at %d, want 8; the shader reads it there", off)
	}
	if SubcellBufferLayout.ArrayStride != subcellInstanceSize {
		t.Errorf("vertex stride %d, struct %d", SubcellBufferLayout.ArrayStride, subcellInstanceSize)
	}
}

// The point of the format: a quarter of the bytes an instance of the general
// sprite path costs.
func TestSubcellIsAQuarterOfASprite(t *testing.T) {
	sprite := unsafe.Sizeof(InstanceData{})
	cell := unsafe.Sizeof(SubcellInstance{})
	if cell*4 != sprite {
		t.Fatalf("a cell is %d bytes against a sprite's %d; the claim is a quarter", cell, sprite)
	}

	const instances = 250_000 // the count measured on screen in the GPU benchmark
	t.Logf("%d instances a frame: %d KB as sprites, %d KB as cells",
		instances, uintptr(instances)*sprite/1024, uintptr(instances)*cell/1024)
}

// The ramp is a uniform binding, which is guaranteed only to 64 KB.
func TestRampTableFitsAUniformBinding(t *testing.T) {
	if got := unsafe.Sizeof(RampTable{}); got != rampTableSize {
		t.Fatalf("RampTable is %d bytes, the constant says %d", got, rampTableSize)
	}
	if rampTableSize > 64*1024 {
		t.Fatalf("the ramp table is %d bytes, past the 64 KB a uniform binding guarantees", rampTableSize)
	}
}

// The WGSL array length is written as a literal, so it has to agree with the Go
// constant. Nothing in the toolchain checks that, so this does.
func TestRampSizeMatchesTheShader(t *testing.T) {
	if !contains(subcellVertWGSL, "array<vec4<f32>, 1024>") {
		t.Fatal("the shader's ramp array length no longer reads 1024")
	}
	if RampEntries != 1024 {
		t.Fatalf("RampEntries is %d but the shader says 1024", RampEntries)
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
