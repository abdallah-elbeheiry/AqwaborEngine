package render

import (
	_ "embed"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed shaders/cull.wgsl
var cullWGSL string

// IndirectCmd matches WebGPU's DrawIndexedIndirectCommand layout (20 bytes).
type IndirectCmd struct {
	IndexCount    uint32
	InstanceCount uint32
	FirstIndex    uint32
	BaseVertex    int32
	FirstInstance uint32
}

const indirectCmdSize = 20

// cullSlots is how many culled draws one frame may issue. Each takes its own
// region of the output buffer, its own indirect command and its own parameters,
// because the compute pass writes them and the draw that follows reads them.
// Sharing one set made a second culled draw overwrite the first.
const cullSlots = 8

// slotStride pads each slot's command and parameters out to the alignment a
// uniform binding offset requires, which is the larger of the two constraints.
const slotStride = 256

// CullParams is the compute shader uniform for culling parameters.
type CullParams struct {
	InstanceCount uint32
	// OutputBase is the first index of this slot's region in the output buffer.
	// The shader compacts survivors from there rather than from zero, which is
	// what lets several culls share one buffer.
	OutputBase uint32
	// InputBase is the first instance the cull reads, so a cull can cover one
	// range of a layer's buffer rather than the whole of it.
	InputBase uint32
	_         uint32 // pad: the vec2 fields align to 8 in WGSL
	MinBounds [2]float32
	MaxBounds [2]float32
}

const cullParamsSize = 32

var _ [cullParamsSize - unsafe.Sizeof(CullParams{})]byte
var _ [unsafe.Sizeof(CullParams{}) - cullParamsSize]byte

// CullPipeline manages GPU compute culling and indirect draw support.
//
// Ordering contract: the compute pass that writes outputBuf/indirectBuf must
// be encoded (and submitted) before the render pass that reads them. WebGPU
// inserts the required memory barriers between the compute and render passes
// as long as EncodeDispatch happens-before the DrawInstancedIndirect call on
// the same queue submission order.
type CullPipeline struct {
	dev          *wgpu.Device
	queue        *wgpu.Queue
	pipe         *wgpu.ComputePipeline
	bgl          *wgpu.BindGroupLayout
	pl           *wgpu.PipelineLayout
	indirectBuf  *wgpu.Buffer
	outputBuf    *wgpu.Buffer
	paramsBuf    *wgpu.Buffer
	maxInstances int

	// Cached bind group: rebuilt only when the input or camera buffer
	// identity changes (avoids a CreateBindGroup per dispatch).
	// One cached bind group per slot, each binding that slot's parameters at
	// its own offset. Rebuilt only when the input or camera buffer identity
	// changes, which avoids a CreateBindGroup per dispatch.
	cached [cullSlots]slotBinding

	// next is the slot the following cull this frame will take.
	next int
}

type slotBinding struct {
	bg     *wgpu.BindGroup
	input  *wgpu.Buffer
	camera *wgpu.Buffer
}

// NewCullPipeline creates the compute cull pipeline and GPU buffers.
// queue is retained for params uploads; if nil, dev.Queue() is used.
func NewCullPipeline(dev *wgpu.Device, queue *wgpu.Queue, maxInstances int) *CullPipeline {
	if queue == nil {
		queue = dev.Queue()
	}
	cp := &CullPipeline{dev: dev, queue: queue, maxInstances: maxInstances}

	shader, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "cull compute",
		WGSL:  cullWGSL,
	})
	if err != nil {
		panic(err)
	}
	defer shader.Release()

	cp.indirectBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "indirect cmd",
		Size:  slotStride * cullSlots,
		Usage: gputypes.BufferUsageIndirect | gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	cp.outputBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "cull output",
		Size:  uint64(maxInstances * instanceDataSize * cullSlots),
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	cp.paramsBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "cull params",
		Size:  slotStride * cullSlots,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	cp.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "cull bgl",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gputypes.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
			{Binding: 1, Visibility: gputypes.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage}},
			{Binding: 2, Visibility: gputypes.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage}},
			{Binding: 3, Visibility: gputypes.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
			{Binding: 4, Visibility: gputypes.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
		},
	})
	if err != nil {
		panic(err)
	}

	cp.pl, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "cull pll",
		BindGroupLayouts: []*wgpu.BindGroupLayout{cp.bgl},
	})
	if err != nil {
		panic(err)
	}

	cp.pipe, err = dev.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:      "cull pipeline",
		Layout:     cp.pl,
		Module:     shader,
		EntryPoint: "main",
	})
	if err != nil {
		panic(err)
	}

	return cp
}

// BeginFrame returns the pipeline to its first slot. Call once a frame, before
// any cull.
func (cp *CullPipeline) BeginFrame() { cp.next = 0 }

// SlotsLeft is how many culled draws remain available this frame.
func (cp *CullPipeline) SlotsLeft() int { return cullSlots - cp.next }

// Claim reserves the next slot, reporting false when the frame is out of them.
func (cp *CullPipeline) Claim() (int, bool) {
	if cp.next >= cullSlots {
		return 0, false
	}
	s := cp.next
	cp.next++
	return s, true
}

// ResetIndirect clears one slot's instance count. The compute shader adds to it
// atomically, so it starts each cull at zero. FirstInstance stays zero because
// the draw binds this slot's region of the output buffer directly.
func (cp *CullPipeline) ResetIndirect(slot int, indexCount uint32) {
	cmd := IndirectCmd{
		IndexCount:    indexCount,
		InstanceCount: 0,
		FirstIndex:    0,
		BaseVertex:    0,
		FirstInstance: 0,
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&cmd)), indirectCmdSize)
	cp.queue.WriteBuffer(cp.indirectBuf, uint64(slot)*slotStride, src)
}

// OutputOffset is the byte offset of a slot's region in the output buffer,
// which is what the draw binds the instance stream at.
func (cp *CullPipeline) OutputOffset(slot int) uint64 {
	return uint64(slot) * uint64(cp.maxInstances) * instanceDataSize
}

// IndirectOffset is the byte offset of a slot's draw command.
func (cp *CullPipeline) IndirectOffset(slot int) uint64 { return uint64(slot) * slotStride }

// EncodeDispatch records the compute cull pass into the command encoder.
// inputBuf: the InstanceBuffer's GPU buffer with all instances.
// cameraBuf: the camera uniform buffer (binding 3 in the shader).
func (cp *CullPipeline) EncodeDispatch(
	enc *wgpu.CommandEncoder,
	slot int,
	inputBuf *wgpu.Buffer,
	cameraBuf *wgpu.Buffer,
	firstInstance, instanceCount int,
	minBounds, maxBounds [2]float32,
) {
	params := CullParams{
		InstanceCount: uint32(instanceCount),
		OutputBase:    uint32(slot * cp.maxInstances),
		InputBase:     uint32(firstInstance),
		MinBounds:     minBounds,
		MaxBounds:     maxBounds,
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&params)), cullParamsSize)
	cp.queue.WriteBuffer(cp.paramsBuf, uint64(slot)*slotStride, src)

	bg := cp.bindGroupFor(slot, inputBuf, cameraBuf)
	if bg == nil {
		return
	}

	pass, err := enc.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "cull"})
	if err != nil {
		return
	}
	pass.SetPipeline(cp.pipe)
	pass.SetBindGroup(0, bg, nil)
	workgroups := (instanceCount + 63) / 64
	pass.Dispatch(uint32(workgroups), 1, 1)
	pass.End()
}

// bindGroupFor returns the cached bind group for one slot, rebuilding it only
// when the input or camera buffer identity changes.
//
// Each slot binds its own parameters and its own indirect command at their
// offsets, which is why there is a bind group per slot rather than one shared.
func (cp *CullPipeline) bindGroupFor(slot int, inputBuf, cameraBuf *wgpu.Buffer) *wgpu.BindGroup {
	b := &cp.cached[slot]
	if b.bg != nil && b.input == inputBuf && b.camera == cameraBuf {
		return b.bg
	}
	if b.bg != nil {
		b.bg.Release()
		b.bg = nil
	}
	bg, err := cp.dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "cull bg",
		Layout: cp.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: inputBuf},
			{Binding: 1, Buffer: cp.outputBuf},
			{Binding: 2, Buffer: cp.indirectBuf, Offset: uint64(slot) * slotStride, Size: indirectCmdSize},
			{Binding: 3, Buffer: cameraBuf},
			{Binding: 4, Buffer: cp.paramsBuf, Offset: uint64(slot) * slotStride, Size: cullParamsSize},
		},
	})
	if err != nil {
		log.Error("failed to build the cull bind group", "slot", slot, "err", err)
		return nil
	}
	b.bg, b.input, b.camera = bg, inputBuf, cameraBuf
	return bg
}

// IndirectBuffer returns the indirect command buffer for DrawIndexedIndirect.
func (cp *CullPipeline) IndirectBuffer() *wgpu.Buffer {
	return cp.indirectBuf
}

// OutputBuffer returns the compacted instance output buffer.
func (cp *CullPipeline) OutputBuffer() *wgpu.Buffer {
	return cp.outputBuf
}

// Release releases GPU resources.
func (cp *CullPipeline) Release() {
	for i := range cp.cached {
		if cp.cached[i].bg != nil {
			cp.cached[i].bg.Release()
			cp.cached[i].bg = nil
		}
	}
	if cp.pipe != nil {
		cp.pipe.Release()
	}
	if cp.bgl != nil {
		cp.bgl.Release()
	}
	if cp.pl != nil {
		cp.pl.Release()
	}
	if cp.indirectBuf != nil {
		cp.indirectBuf.Release()
	}
	if cp.outputBuf != nil {
		cp.outputBuf.Release()
	}
	if cp.paramsBuf != nil {
		cp.paramsBuf.Release()
	}
}
