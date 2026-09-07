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
type CullPipeline struct {
	dev          *wgpu.Device
	pipe         *wgpu.ComputePipeline
	bgl          *wgpu.BindGroupLayout
	pl           *wgpu.PipelineLayout
	indirectBuf  *wgpu.Buffer
	outputBuf    *wgpu.Buffer
	paramsBuf    *wgpu.Buffer
	maxInstances int
}

// NewCullPipeline creates the compute cull pipeline and GPU buffers.
func NewCullPipeline(dev *wgpu.Device, maxInstances int) *CullPipeline {
	cp := &CullPipeline{dev: dev, maxInstances: maxInstances}

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
func (cp *CullPipeline) ResetIndirect(queue *wgpu.Queue) {
	cmd := IndirectCmd{
		IndexCount:    6,
		InstanceCount: 0,
		FirstIndex:    0,
		BaseVertex:    0,
		FirstInstance: 0,
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&cmd)), indirectCmdSize)
	queue.WriteBuffer(cp.indirectBuf, 0, src)
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
	cp.dev.Queue().WriteBuffer(cp.paramsBuf, 0, src)

	// Create a fresh bind group (cheap, no allocation in GPU terms)
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
		return
	}
	defer bg.Release()

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
