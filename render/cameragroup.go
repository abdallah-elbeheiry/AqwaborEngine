package render

import (
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// cameraGroup is the one camera the frame is drawn through: a single uniform
// buffer, its layout, and its bind group, bound at group 0 by every pipeline.
//
// Each pipeline used to own a copy of this. The four of them held the same
// eighty bytes, SetCamera wrote each in turn, and a pipeline built outside the
// engine had to build a fourth for itself and remember to update it. They also
// drifted apart in principle: the compute cull read the sprite pipeline's copy,
// so a draw under a different camera would have been culled against the wrong
// frustum.
//
// Group 0 belongs to the engine. A pipeline's own bindings start at group 1.
type cameraGroup struct {
	buf *wgpu.Buffer
	bgl *wgpu.BindGroupLayout
	bg  *wgpu.BindGroup
}

func newCameraGroup(dev *wgpu.Device) *cameraGroup {
	c := &cameraGroup{}

	var err error
	c.buf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "camera uniform",
		Size:  CameraUniformSize,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}

	// Visible to both stages, because it is the superset of what the pipelines
	// sharing it ask for: the sprite pipeline reads it in both, the stroke
	// pipeline only in the vertex stage.
	c.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "camera bgl",
		Entries: []gputypes.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
			Buffer: &gputypes.BufferBindingLayout{
				Type: gputypes.BufferBindingTypeUniform,
			},
		}},
	})
	if err != nil {
		panic(err)
	}

	c.bg, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "camera bg",
		Layout: c.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: c.buf, Size: CameraUniformSize},
		},
	})
	if err != nil {
		panic(err)
	}

	return c
}

// update writes the view-projection and viewport. One write a frame, whatever
// is drawn with it.
func (c *cameraGroup) update(queue *wgpu.Queue, viewProj [16]float32, viewportW, viewportH float32) {
	u := CameraUniform{
		ViewProj: viewProj,
		Viewport: [2]float32{viewportW, viewportH},
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u)), CameraUniformSize)
	queue.WriteBuffer(c.buf, 0, src)
}

func (c *cameraGroup) Release() {
	if c.bg != nil {
		c.bg.Release()
	}
	if c.bgl != nil {
		c.bgl.Release()
	}
	if c.buf != nil {
		c.buf.Release()
	}
}
