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

// CullParams is the compute shader uniform for culling parameters.
type CullParams struct {
	InstanceCount uint32
	_pad          uint32
	MinBounds     [2]float32
	MaxBounds     [2]float32
}

const cullParamsSize = 24

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
	cachedBG     *wgpu.BindGroup
	cachedInput  *wgpu.Buffer
	cachedCamera *wgpu.Buffer
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
		Size:  indirectCmdSize,
		Usage: gputypes.BufferUsageIndirect | gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	cp.outputBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "cull output",
		Size:  uint64(maxInstances * instanceDataSize),
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	cp.paramsBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "cull params",
		Size:  cullParamsSize,
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

// ResetIndirect clears the instance count in the indirect command buffer.
// Must be called before each cull dispatch (the compute shader atomically adds to it).
// indexCount is the mesh index count (e.g. 6 for a unit quad); FirstInstance
// is 0 because the shader compacts survivors into outputBuf from slot 0.
func (cp *CullPipeline) ResetIndirect(indexCount uint32) {
	cmd := IndirectCmd{
		IndexCount:    indexCount,
		InstanceCount: 0,
		FirstIndex:    0,
		BaseVertex:    0,
		FirstInstance: 0,
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&cmd)), indirectCmdSize)
	cp.queue.WriteBuffer(cp.indirectBuf, 0, src)
}

// EncodeDispatch records the compute cull pass into the command encoder.
// inputBuf: the InstanceBuffer's GPU buffer with all instances.
// cameraBuf: the camera uniform buffer (binding 3 in the shader).
func (cp *CullPipeline) EncodeDispatch(
	enc *wgpu.CommandEncoder,
	inputBuf *wgpu.Buffer,
	cameraBuf *wgpu.Buffer,
	instanceCount int,
	minBounds, maxBounds [2]float32,
) {
	// Write cull params
	params := CullParams{
		InstanceCount: uint32(instanceCount),
		MinBounds:     minBounds,
		MaxBounds:     maxBounds,
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&params)), cullParamsSize)
	cp.queue.WriteBuffer(cp.paramsBuf, 0, src)

	// Reuse the cached bind group unless a buffer identity changed.
	bg := cp.bindGroupFor(inputBuf, cameraBuf)

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

// bindGroupFor returns the cached bind group, rebuilding it only when the
// input or camera buffer identity changes.
func (cp *CullPipeline) bindGroupFor(inputBuf, cameraBuf *wgpu.Buffer) *wgpu.BindGroup {
	if cp.cachedBG != nil && cp.cachedInput == inputBuf && cp.cachedCamera == cameraBuf {
		return cp.cachedBG
	}
	if cp.cachedBG != nil {
		cp.cachedBG.Release()
		cp.cachedBG = nil
	}
	bg, err := cp.dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "cull bg",
		Layout: cp.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: inputBuf},
			{Binding: 1, Buffer: cp.outputBuf},
			{Binding: 2, Buffer: cp.indirectBuf},
			{Binding: 3, Buffer: cameraBuf},
			{Binding: 4, Buffer: cp.paramsBuf},
		},
	})
	if err != nil {
		return nil
	}
	cp.cachedBG = bg
	cp.cachedInput = inputBuf
	cp.cachedCamera = cameraBuf
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
	if cp.cachedBG != nil {
		cp.cachedBG.Release()
		cp.cachedBG = nil
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
